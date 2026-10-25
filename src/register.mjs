import fs from 'node:fs/promises';
import {join, resolve, parse} from 'node:path';
import {userInfo} from 'node:os';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {withRegistrationCore,withAutomaticCore} from './core.mjs';
import {verifyRunnerDistribution} from './runner-distribution.mjs';
import {requestAutomatic,automaticBinding} from './automatic.mjs';
import {processEnvironment, githubObservation, observeProcess} from './create.mjs';
class Diagnostic extends Error {}
const fail = message => { throw new Diagnostic(message); };
const id = value => typeof value === 'string' && /^[1-9][0-9]*$/.test(value);
export function registrationOptions(args) {
  const values = {};
  for (let i = 0; i < args.length; i += 2) {
    const key = args[i], value = args[i+1];
    if (!['--runner','--repo','--org','--group','--method'].includes(key) || key in values ||
        !value || value.startsWith('--') || /[\x00-\x1f\x7f]/.test(value)) fail('Use --runner NAME (--repo OWNER/REPO | --org ORG) [--group NAME] [--method auto|manual]; each option once.');
    values[key] = value;
  }
  if (!/^[a-z][a-z0-9-]{0,47}$/.test(values['--runner'] ?? '') ||
      Boolean(values['--repo']) === Boolean(values['--org']) || values['--repo'] && values['--group']) fail('Choose one existing --runner and exactly one --repo or --org; --group is Org-only.');
  if (values['--method'] && !['manual','auto'].includes(values['--method'])) fail('Choose auto or manual registration.');
  if (values['--method'] === 'auto' && values['--org']) fail('Automatic organization-level registration requires a protected repository context not installed by this client; use the authorized manual Org route.');
  if (values['--group']?.length > 128) fail('Runner group name too long.');
  return {name:values['--runner'], repo:values['--repo'], org:values['--org'], group:values['--group'],method:values['--method'] ?? 'manual'};
}
// Explicit argument arrays, no shell; discard all potentially credential-bearing output.
// Kill the process group so config descendants cannot survive cancellation/timeout.
export function registrationProcess(command, args, {cwd, env, signal, timeoutMs = 30000} = {}) {
  return new Promise(resolve => {
    if (signal?.aborted) return resolve({ok:false, reason:'cancelled'});
    let child, timer, hard, reason;
    const stop = why => {
      reason ??= why;
      try { process.kill(-child.pid, 'SIGTERM'); } catch {}
      hard ??= setTimeout(() => { try { process.kill(-child.pid,'SIGKILL'); } catch {} }, 200);
    };
    try { child = spawn(command,args,{cwd,env,shell:false,detached:true,stdio:['ignore','ignore','ignore']}); }
    catch { return resolve({ok:false,reason:'process'}); }
    timer = setTimeout(() => stop('timeout'), timeoutMs);
    const abort = () => stop('cancelled');
    signal?.addEventListener('abort',abort,{once:true});
    child.once('error',() => { reason ??= 'process'; });
    child.once('close',code => {
      clearTimeout(timer);
      // Send a final kill even if the parent exits before its descendants.
      if (reason) { try { process.kill(-child.pid,'SIGKILL'); } catch {} }
      clearTimeout(hard); signal?.removeEventListener('abort',abort);
      resolve({ok:code === 0 && !reason,reason:reason ?? (code === 0 ? undefined : 'exit')});
    });
  });
}
export async function safePath(path, {directory = true, uid = userInfo().uid} = {}) {
  const absolute = resolve(path); if (/[\x00-\x1f\x7f]/.test(absolute)) fail('Unsafe control character in resource path.');
  let current = parse(absolute).root;
  for (const part of absolute.slice(current.length).split('/')) {
    current = join(current,part);
    const stat = await fs.lstat(current);
    if (stat.isSymbolicLink() || (current !== absolute && !stat.isDirectory())) fail('Unsafe symlink or path component.');
    if (current === absolute && (directory ? !stat.isDirectory() : !stat.isFile())) fail('Unsafe resource type.');
  }
  const stat = await fs.stat(absolute);
  if (stat.uid !== uid || stat.mode & (directory ? 0o077 : 0o022)) fail('Instance resources must belong to the execution account and deny group/other writes.');
  return absolute;
}
async function absent(path) {
  try { await fs.lstat(path); return false; } catch (error) { if (error.code === 'ENOENT') return true; throw error; }
}
export async function loadEnvironment(root, name, account) {
  const base = join(resolve(root),name);
  await safePath(base); await safePath(join(base,'environment.json'),{directory:false});
  const r = JSON.parse(await fs.readFile(join(base,'environment.json'),'utf8'));
  if (r.schemaVersion !== 1 || r.name !== name || !['codex','claude','gemini'].includes(r.agent) || !id(r.githubId) ||
      r.account?.uid !== account.uid || r.account?.username !== account.username ||
      r.loginSpace !== join(base,'login') || !Array.isArray(r.instances) || !r.instances.length || r.instances.length > 128) fail('Incomplete environment or execution/service account mismatch.');
  await safePath(r.loginSpace);
  const ids = new Set([name]);
  for (const [i, instance] of r.instances.entries()) {
    if (typeof instance.id !== 'string' || !/^[a-zA-Z0-9-]{1,80}$/.test(instance.id) || ids.has(instance.id) ||
        instance.loginSpace !== r.loginSpace || !['unregistered','registered'].includes(instance.status)) fail('Malformed/duplicate instance identity or conflicting registration state.');
    ids.add(instance.id);
    for (const [key, leaf] of [['workspace','workspace'],['registration','registration'],['service','service']]) {
      if (instance[key] !== join(base,`instance-${i+1}`,leaf)) fail('Instance path escape or conflicting layout.');
      await safePath(instance[key]);
    }
  }
  return r;
}
function providerFailure(result, level) {
  const kind = result.diagnostic?.kind;
  const category = ['authentication','denied','not_found_or_inaccessible','rate_limited','timeout','cancelled','malformed','transport','server'].includes(kind) ? kind : 'unavailable';
  fail(`GitHub ${category}${Number.isInteger(result.diagnostic?.status) ? ' HTTP '+result.diagnostic.status : ''}; verify stored gh login, target access and ${level === 'repository' ? 'Repository Administration write/read' : 'Organization self-hosted runners management/read'} rights and policy. Inaccessible does not mean nonexistent; uncertain writes require reconciliation.`);
}
// Org-only registration has no target repository; do not fabricate N15 access context.
export async function inventory(provider, endpoint, envelope, target, signal) {
  const values = [], seen = new Set(); let total;
  for (let page = 1; page <= 100; page++) {
    const result = await provider.request({endpoint:endpoint+`?per_page=100&page=${page}`, target, operation:'registration-readback',correlation:'manual',signal});
    if (result.outcome !== 'read') providerFailure(result,'organization');
    const data = result.data, items = data?.[envelope];
    if (!Number.isSafeInteger(data?.total_count) || data.total_count < 0 || !Array.isArray(items) || items.length > 100 ||
        total !== undefined && total !== data.total_count) fail('Incomplete Organization inventory.');
    total = data.total_count;
    for (const item of items) {
      if (!id(item?.id) || seen.has(item.id)) fail('Ambiguous Organization inventory identity.');
      seen.add(item.id); values.push(item);
    }
    if (values.length > total) fail('Inconsistent Organization inventory count.');
    let next;
    for (const part of result.link ? result.link.split(',') : []) {
      const match = part.trim().match(/^<([^>]+)>;\s*rel="(next|prev|first|last)"$/);
      if (!match) fail('Incomplete or unsafe Organization pagination.');
      const url = new URL(match[1]);
      if (url.origin !== 'https://api.github.com' || url.pathname !== endpoint || url.username || url.password || url.hash ||
          [...url.searchParams.keys()].some(k => !['page','per_page'].includes(k) || url.searchParams.getAll(k).length !== 1) ||
          !/^[1-9][0-9]*$/.test(url.searchParams.get('page') ?? '') || url.searchParams.get('per_page') !== '100') fail('Unsafe Organization pagination.');
      if (match[2] === 'next') { if (next) fail('Ambiguous pagination.'); next = Number(url.searchParams.get('page')); }
    }
    if (values.length === total && !next) return values;
    if (!items.length || next !== page+1) fail('Incomplete Organization pagination.');
  }
  fail('Organization pagination limit exceeded.');
}
export async function orgReadback(provider, prefix, target, runnerId, label, group, signal) {
  const identity = await provider.request({endpoint:prefix,target,expectedId:target,operation:'registration-readback',correlation:'manual',signal});
  if (identity.outcome !== 'read') providerFailure(identity,'organization');
  if (identity.data.type !== 'Organization' || identity.data.login?.toLowerCase() !== prefix.slice(6)) fail('Fresh Organization identity mismatch.');
  const runners = await inventory(provider,prefix+'/actions/runners','runners',target,signal);
  const matches = runners.filter(r => r.id === runnerId);
  if (matches.length !== 1) fail('Runner ID missing/replaced in Organization readback.');
  const runner = matches[0], labels = runner.labels?.map(l => typeof l.name === 'string' ? l.name.toLowerCase() : '');
  if (runner.os !== 'linux' || !['online','offline'].includes(runner.status) || typeof runner.busy !== 'boolean' ||
      typeof runner.name !== 'string' || !labels || labels.length !== new Set(labels).size || !['self-hosted','linux',label].every(l => labels.includes(l)) ||
      labels.some(l => /^(windows|macos)$/.test(l) || l.startsWith('nibfin-user-') && l !== label)) fail('Organization Runner Label/platform/state mismatch.');
  if (group) {
    const groups = await inventory(provider,prefix+'/actions/runner-groups','runner_groups',target,signal);
    const matching = groups.filter(g => g.name === group);
    if (matching.length !== 1) fail('Organization runner group missing or ambiguous.');
    const members = await inventory(provider,prefix+`/actions/runner-groups/${matching[0].id}/runners`,'runners',target,signal);
    if (!members.some(r => r.id === runnerId)) fail('Organization runner group membership mismatch.');
    return {id:runnerId,platform:'linux',status:runner.status,busy:runner.busy,groupId:matching[0].id};
  }
  return {id:runnerId,platform:'linux',status:runner.status,busy:runner.busy};
}
async function registerUnlocked(args, dependencies = {}) {
  const options = registrationOptions(args);
  const execute = async core => {
    const account = dependencies.account ?? userInfo(), root = dependencies.root ?? join(userInfo().homedir,'.local/share/nibfin/environments');
    let coordinates;
    try {
      coordinates = options.repo ? core.repositoryCoordinates(...options.repo.split('/')) : core.repositoryCoordinates(options.org,'placeholder');
      if (options.repo && options.repo.split('/').length !== 2) throw Error();
    } catch { fail('Invalid GitHub target coordinate.'); }
    const level = options.repo ? 'repository' : 'organization';
    const record = await loadEnvironment(root,options.name,account);
    const environment = processEnvironment(record.loginSpace);
    if (Object.values(environment).some(value => /[\x00-\x1f\x7f]/.test(value))) fail('Unsafe execution account environment.');
    const member = await githubObservation({run:dependencies.run ?? observeProcess,environment,account});
    if (member !== record.githubId) fail('Current gh member identity differs from the environment member.');
    // A pending/unknown instance blocks the environment until explicit reconciliation.
    const localRecords = [record];
    for (const entry of await fs.readdir(root,{withFileTypes:true})) {
      if (entry.name === record.name || !entry.isDirectory() || !/^[a-z][a-z0-9-]{0,47}$/.test(entry.name) ||
          await absent(join(root,entry.name,'environment.json'))) continue;
      const other = await loadEnvironment(root,entry.name,account);
      if (other.githubId === member) localRecords.push(other);
    }
    const boundIds = new Set(), instanceIds = new Set();
    for (const localRecord of localRecords) for (const item of localRecord.instances) {
      if (instanceIds.has(item.id)) fail('Duplicate local instance identity across environments.');
      instanceIds.add(item.id);
      const path = join(item.registration,'binding.json');
      if (!await absent(path)) {
        await safePath(path,{directory:false});
        const binding = JSON.parse(await fs.readFile(path,'utf8'));
        if (localRecord !== record && binding.disposition !== 'registered') continue;
        if (binding.schemaVersion !== 1 || binding.disposition !== 'registered' || binding.instanceId !== item.id || binding.memberId !== member ||
            !id(binding.runnerId) || !id(binding.registration?.id) || !['repository','organization'].includes(binding.registration.level) ||
            binding.label !== core.memberRunnerLabel(member) || binding.platform !== 'linux' || binding.target?.id !== binding.registration.id ||
            binding.service?.account !== account.username || (!automaticBinding(binding) && (!['online','offline'].includes(binding.status) || typeof binding.busy !== 'boolean')) ||
            binding.registration.level === 'organization' && !/^[a-z0-9-]+$/.test(binding.registration.organization ?? '') ||
            boundIds.has(binding.registration.level+':'+binding.registration.id+':'+binding.runnerId)) fail('Pending/unknown or conflicting binding requires manual reconciliation before registration.');
        boundIds.add(binding.registration.level+':'+binding.registration.id+':'+binding.runnerId);
        item.status = 'registered'; item.binding = binding;
      } else if (item.status !== 'unregistered') fail('Registered descriptor lacks a verified binding; reconcile before registration.');
    }
    const instance = record.instances.find(i => i.status === 'unregistered');
    if (!instance) fail('No remaining unregistered instance; count is unchanged.');
    const lock = join(instance.service,'registration.lock');
    let locked = false, pending = false, state;
    const publish = dependencies.publish ?? (async value => {
      const path = join(instance.registration,'binding.json');
      await fs.writeFile(path+'.pending',JSON.stringify(value,null,2)+'\n',{flag:'wx',mode:0o600});
      await fs.rename(path+'.pending',path);
    });
    try {
      await fs.mkdir(lock,{mode:0o700}); locked = true;
      await fs.writeFile(join(lock,'lifecycle-owner.json'),JSON.stringify({pid:process.pid}),{flag:'wx',mode:0o600});
      // Recheck under the shared lock: selection above may predate another operation.
      if (!await absent(join(instance.registration,'binding.json'))) fail('Binding changed; reconcile before registration.');
      const residual = join(instance.registration,'unregister.json');
      if (!await absent(residual)) {
        await safePath(residual,{directory:false});
        const previous = JSON.parse(await fs.readFile(residual,'utf8'));
        if (previous.schemaVersion !== 1 || previous.instanceId !== instance.id || previous.identity?.instanceId !== instance.id ||
            !id(previous.identity.runnerId) || previous.complete !== true || previous.local !== 'detached' || previous.service !== 'terminated' || previous.remote !== 'absent') fail('Unregister residual requires reconciliation before registration.');
      }
      if ((await fs.readdir(instance.service)).some(name => name !== 'registration.lock')) fail('Conflicting local service resources require reconciliation before registration.');
      for (const name of ['.runner','.runner_migrated','.credentials','.credentials_migrated','.credentials_rsaparams','.credential_store','.service','runsvc.sh','binding.json.pending','unregister.json.pending']) {
        if (!await absent(join(instance.registration,name))) fail('Existing configuration/credentials or pending publication requires reconciliation.');
      }
      if (process.platform !== 'linux' || account.uid === 0 || account.uid !== userInfo().uid) fail('Supported prerequisite: Linux with non-root execution/service account and a user systemd manager.');
      for (const name of ['config.sh','bin/runsvc.sh']) {
        try { const path = join(instance.registration,name); await safePath(path,{directory:false}); await fs.access(path,fs.constants.X_OK); }
        catch { fail('Missing or unsafe official runner application config.sh/bin/runsvc.sh; prepare a pinned official distribution in this instance registration directory.'); }
      }
      if (options.method === 'auto') {
        try {await (dependencies.distributionVerifier ?? verifyRunnerDistribution)(instance.registration,{signal:dependencies.signal});}
        catch {fail('Automatic registration requires the pinned official Linux x64 v2.331.0 archive, matching extracted bytes, private directories and local tar; no request submitted.');}
      }
      const proc = dependencies.process ?? registrationProcess;
      const invoke = async (command, argv, cwd = instance.registration) => {
        const result = await proc(command,argv,{cwd,env:environment,signal:dependencies.signal,timeoutMs:dependencies.timeoutMs ?? 30000});
        if (!result?.ok) fail(`Official configuration/service process ${['timeout','cancelled'].includes(result?.reason) ? result.reason : 'failed'}; verify executable, local privilege and execution/service account. Inspect private instance state before retrying.`);
      };
      // Runtime directory is derived from the real account, never inherited from Actions.
      environment.XDG_RUNTIME_DIR = `/run/user/${account.uid}`;
      await invoke('systemctl',['--user','show-environment']);
      const prefix = options.repo ? `/repos/${coordinates.owner}/${coordinates.repository}` : `/orgs/${coordinates.owner}`;
      const signal = AbortSignal.any([AbortSignal.timeout(dependencies.waitMs ?? 120000), ...(dependencies.signal ? [dependencies.signal] : [])]);
      const transport = dependencies.transport ?? core.localGhTransport();
      // This capability authorizes only this one manual token endpoint, not Core lifecycle writes.
      let targetId;
      const provider = core.createGithubProvider({transport,attempts:1,authorize:request => request.method === 'POST' &&
        (options.method === 'manual' ? request.endpoint === prefix+'/actions/runners/registration-token' : /^\/repos\/[a-z0-9-]+\/[a-z0-9_.-]+\/actions\/workflows\/[1-9][0-9]*\/dispatches$/.test(request.endpoint) && request.endpoint.startsWith(prefix+'/actions/')) && request.target === targetId});
      const targetResult = await provider.request({endpoint:prefix,target:member,operation:'registration-target',correlation:'manual',signal});
      if (targetResult.outcome !== 'read') providerFailure(targetResult,level);
      const target = targetResult.data; targetId = target?.id;
      if (!id(targetId) || (options.repo ? target.private !== false || target.visibility !== 'public' ||
          target.full_name?.toLowerCase() !== `${coordinates.owner}/${coordinates.repository}` || !id(target.owner?.id) ||
          !['User','Organization'].includes(target.owner?.type) : target.type !== 'Organization' || target.login?.toLowerCase() !== coordinates.owner)) fail('Target identity/public visibility mismatch.');
      if (options.method === 'auto' && !(target.permissions?.push === true || target.permissions?.maintain === true || target.permissions?.admin === true)) fail('Automatic registration requires current repository Write/Maintain/Admin access.');
      const scope = {level,id:targetId,...(options.org ? {organization:coordinates.owner} : {})};
      state = {schemaVersion:1,instanceId:instance.id,memberId:member,registration:scope,
        target:{id:targetId,owner:coordinates.owner,...(options.repo ? {repository:coordinates.repository,ownerId:target.owner.id,ownerType:target.owner.type} : {})},label:core.memberRunnerLabel(member),
        disposition:'pending',service:{status:'not-installed',account:account.username},runnerName:`nibfin-${record.name.slice(0,12)}-${createHash('sha256').update(record.name+':'+instance.id).digest('hex').slice(0,24)}`};
      let issued;
      if (options.method === 'auto') {
        issued = await requestAutomatic(core,{provider,repository:`${coordinates.owner}/${coordinates.repository}`,repositoryId:targetId,
          member,instance,state,publish:async value=>{await publish(value);pending=true;},signal,
          ...Object.fromEntries(['configuration','confirm','archive','wait','now','waitMs'].filter(k=>dependencies[k]!==undefined).map(k=>[k,dependencies[k]]))});
        if (issued.cancelled) return issued;
      } else {
        // Durable reservation precedes token issuance and all potentially remote effects.
        await publish(state); pending = true;
        const result = await provider.request({method:'POST',endpoint:prefix+'/actions/runners/registration-token',target:targetId,
          operation:'manual-registration',correlation:'manual',signal});
        if (result.outcome !== 'submitted') providerFailure(result,level);
        issued = result.data;
      }
      let token = issued?.token;
      const expiry = Date.parse(issued?.expires_at);
      if (typeof token !== 'string' || !/^[A-Za-z0-9_-]{1,2048}$/.test(token) || !Number.isFinite(expiry) || expiry <= Date.now() || expiry > Date.now()+3660000) fail('Malformed or expired registration data; reconcile the pending instance.');
      const url = `https://github.com/${coordinates.owner}${options.repo ? '/'+coordinates.repository : ''}`;
      const configArgs = ['--unattended','--url',url,'--token',token,'--name',state.runnerName,'--labels',state.label,'--work',instance.workspace];
      if (options.group) configArgs.push('--runnergroup',options.group);
      try { await invoke(join(instance.registration,'config.sh'),configArgs); }
      finally { token = undefined; configArgs.fill(''); issued = undefined; }
      await safePath(join(instance.registration,'.runner'),{directory:false});
      await safePath(join(instance.registration,'.credentials'),{directory:false});
      if ((await fs.stat(join(instance.registration,'.credentials'))).mode & 0o077) fail('Official registration credentials must be private to the execution account.');
      // .runner uses agentId rather than REST id; preserve its numeric source spelling.
      const local = JSON.parse(await fs.readFile(join(instance.registration,'.runner'),'utf8'),(key,value,context) =>
        key === 'agentId' && typeof value === 'number' ? context.source : value);
      if (options.method === 'auto') {
        for(const name of ['.credentials','.credentials_rsaparams']) {
          const path=join(instance.registration,name);await safePath(path,{directory:false});
          const st=await fs.stat(path);if(st.mode & 0o077 || st.size<2 || st.size>65536)fail('Missing or unsafe official private credentials.');
          const value=JSON.parse(await fs.readFile(path,'utf8'));
          if(!value || typeof value !== 'object' || Array.isArray(value) || !Object.keys(value).length)fail('Malformed official private credentials.');
        }
        const credentials=JSON.parse(await fs.readFile(join(instance.registration,'.credentials'),'utf8'));
        if (credentials?.scheme !== 'OAuth' || !credentials.data || typeof credentials.data !== 'object' ||
            Array.isArray(credentials.data) || !Object.keys(credentials.data).length ||
            Object.values(credentials.data).some(v=>typeof v !== 'string' || !v)) fail('Malformed official credentials; reconcile configuration.');
      }
      const runnerId = local.agentId;
      if (!id(runnerId) || local.agentName !== state.runnerName || local.gitHubUrl?.replace(/\/$/,'').toLowerCase() !== url || local.workFolder !== instance.workspace) fail('Official local Runner identity/path mismatch or incomplete configuration.');
      state.runnerId = runnerId;
      state.configuration = {state:'completed',runnerId};state.platform='linux';
      let observation;
      if (options.method === 'auto') {
        state.method = 'auto';state.status=null;state.busy=null;state.remote = {state:'unknown',reason:'no_authorized_observation'};
        observation={platform:'linux',status:null,busy:null};
        if (dependencies.observeRemote) {
          try {const remote=await dependencies.observeRemote({state,signal});
            if (remote?.outcome === 'mismatch') {state.remote.reason='observed_mismatch';fail('Independent remote mismatch; reconcile configuration.');}
          } catch (error) {if (state.remote.reason === 'observed_mismatch') throw error;state.remote.reason='observation_unavailable';}
        }
      } else if (options.repo) {
        const result = await core.observeRunners(provider,{target:{...coordinates,id:targetId,ownerId:target.owner.id,ownerType:target.owner.type},
          registration:scope,records:[...localRecords.flatMap(r => r.instances).filter(i => i.binding?.registration.level === level && i.binding.registration.id === targetId).map(i =>
            ({instanceId:i.id,runnerId:i.binding.runnerId,memberId:member,repositoryId:targetId,registration:scope,platform:i.binding.platform})),
            {instanceId:instance.id,runnerId,memberId:member,repositoryId:targetId,registration:scope,platform:'linux'}],
          conditions:{platform:'linux',labels:['self-hosted','linux']},correlation:'manual',signal});
        state.readback = {outcome:result.outcome, codes:result.diagnostics.map(d => d.code).filter(c => typeof c === 'string' && /^[a-z_]+$/.test(c))};
        if (result.outcome !== 'matched' || result.instances?.find(i => i.instanceId === instance.id)?.observation?.id !== runnerId)
          fail(`Fresh N15 Runner readback ${['mismatch','unavailable','unsupported'].includes(result.outcome) ? result.outcome : 'incomplete'}; preserve pending identity for reconciliation.`);
        observation = result.instances.find(i => i.instanceId === instance.id).observation;
      } else observation = await orgReadback(provider,prefix,targetId,runnerId,state.label,options.group,signal);
      await publish(state);
      state.service.status='unknown';
      await publish(state);
      await fs.copyFile(join(instance.registration,'bin/runsvc.sh'),join(instance.registration,'runsvc.sh'),fs.constants.COPYFILE_EXCL);
      await fs.chmod(join(instance.registration,'runsvc.sh'),0o700);
      await safePath(join(instance.registration,'runsvc.sh'),{directory:false});
      await fs.access(join(instance.registration,'runsvc.sh'),fs.constants.X_OK);
      const unitName = `nibfin-${record.name}-${instance.id}.service`, unitPath = join(instance.service,unitName);
      // systemd command syntax is not shell syntax; quote and escape specifiers/variables.
      const quote = (value, expand = false) => '"'+value.replace(/\\/g,'\\\\').replace(/"/g,'\\"').replace(/%/g,'%%').replace(/\$/g, expand ? '$$$$' : '$')+'"';
      await fs.writeFile(unitPath,`[Unit]\nDescription=Nibfin instance runner\n[Service]\nWorkingDirectory=${quote(instance.registration)}\nExecStart=${quote(join(instance.registration,'runsvc.sh'),true)}\n${Object.entries(environment).filter(([key]) => key !== 'XDG_RUNTIME_DIR').map(([key,value]) => 'Environment='+quote(key+'='+value)).join('\n')}\nUnsetEnvironment=GH_TOKEN GITHUB_TOKEN GH_HOST NODE_OPTIONS NODE_PATH OPENAI_API_KEY ANTHROPIC_API_KEY GEMINI_API_KEY\nRestart=on-failure\n[Install]\nWantedBy=default.target\n`,{flag:'wx',mode:0o600});
      await invoke('systemctl',['--user','link',unitPath]);
      state.service = {status:'installed',account:account.username,unit:unitName};
      await publish(state);
      state.service.status='unknown';
      await publish(state);
      await invoke('systemctl',['--user','enable',unitName]);
      await invoke('systemctl',['--user','start',unitName]);
      await invoke('systemctl',['--user','is-active','--quiet',unitName]);
      state.service.status = 'active';
      state = {...state,disposition:'registered',...(options.method === 'auto' ? {configuredAt:new Date().toISOString()} : {observedAt:new Date().toISOString()}),platform:observation.platform,status:observation.status,busy:observation.busy,
        ...(observation.groupId ? {groupId:observation.groupId} : {})};
      await publish(state);
      // Keep the G03 descriptor immutable; binding.json is the per-instance lifecycle authority.
      return state;
    } catch (error) {
      if (pending) {
        try { await publish({...state,disposition:'unknown'}); } catch {}
      }
      if (error instanceof Diagnostic) throw error;
      fail('Registration prerequisite or state publication failed; inspect private per-instance binding/configuration and reconcile before retrying. No automatic re-registration.');
    } finally { if (locked) {await fs.rm(join(lock,'lifecycle-owner.json'),{force:true});await fs.rmdir(lock);} }
  };
  try { return await (dependencies.core ? execute(dependencies.core) : (options.method === 'auto' ? withAutomaticCore(execute) : withRegistrationCore(execute))); }
  catch (error) {
    if (error instanceof Diagnostic) throw error;
    fail('Registration unavailable; verify intact reviewed package, private environment records, application and account prerequisites. Reconcile any existing instance state before retrying.');
  }
}

// One local environment lifecycle owner. Stale locks recover only after the
// recorded process no longer exists; no signals are sent to another process.
export async function withEnvironmentLock(root, name, action) {
  if(!/^[a-z][a-z0-9-]{0,47}$/.test(name)) throw Error('Invalid environment selection.');
  await safePath(root);
  const path=join(root,`.${name}.lifecycle.lock`);
  let handle,recovery;
  try {
    try {handle=await fs.open(path,'wx',0o600);}
    catch(e) {
      if(e.code!=='EEXIST') throw e;
      const conflict=()=>{throw Error('Lifecycle conflict; reconcile before retrying.');};
      await safePath(path,{directory:false});
      const old=await fs.stat(path),pid=await fs.readFile(path,'utf8');
      if(!/^[1-9][0-9]{0,9}$/.test(pid)) conflict();
      try {process.kill(Number(pid),0);conflict();} catch(error) {if(error.code!=='ESRCH') throw error;}
      // Serialize stale-lock reclamation. Never unlink a replacement lock after
      // another contender has recovered the dead process's file.
      recovery=path+'.recovery';
      try {await fs.mkdir(recovery,{mode:0o700});} catch {recovery=undefined;conflict();}
      const current=await fs.stat(path);
      if(current.dev!==old.dev || current.ino!==old.ino || current.birthtimeMs!==old.birthtimeMs) conflict();
      await fs.unlink(path);handle=await fs.open(path,'wx',0o600);
    }
    await handle.writeFile(String(process.pid));await handle.sync();
    if(recovery) {await fs.rmdir(recovery);recovery=undefined;}
    return await action();
  } finally {
    if(handle) {await handle.close();await fs.unlink(path);}
    if(recovery) await fs.rmdir(recovery);
  }
}

export async function registerInstance(args, dependencies = {}) {
  const {name}=registrationOptions(args);
  const root=dependencies.root ?? join(userInfo().homedir,'.local/share/nibfin/environments');
  return withEnvironmentLock(root,name,async()=>{
    try {await fs.lstat(join(root,name,'.removing'));throw Error('Removal requires reconciliation before registration.');}
    catch(e) {if(e.code!=='ENOENT') throw e;}
    return registerUnlocked(args,dependencies);
  });
}

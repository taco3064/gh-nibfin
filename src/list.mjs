import fs from 'node:fs/promises';
import {join, resolve} from 'node:path';
import {userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {withCurrentCore as withRegistrationCore} from './current-core.mjs';
import {processEnvironment, githubObservation, observeProcess} from './create.mjs';
import {safePath, orgReadback, inventory} from './register.mjs';
const namePattern = /^[a-z][a-z0-9-]{0,47}$/;
const id = v => typeof v === 'string' && /^[1-9][0-9]{0,127}$/.test(v);
const identity = v => typeof v === 'string' && /^[a-zA-Z0-9-]{1,80}$/.test(v);
const invalid = () => {throw Error('invalid');};
export function listOptions(args) {
  const options = {json:false};
  for (let i=0;i<args.length;i++) {
    if (args[i] === '--json' && !options.json) options.json = true;
    else if (args[i] === '--runner' && !options.name && namePattern.test(args[i+1] ?? '')) options.name = args[++i];
    else invalid();
  }
  return options;
}
// Read only these bounded public metadata files, never credential/login content.
async function read(path) {
  await safePath(path,{directory:false});
  const handle = await fs.open(path,fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  try {
    const bytes = Buffer.alloc(262145), {bytesRead} = await handle.read(bytes,0,bytes.length,0);
    if (bytesRead > 262144) invalid();
    return JSON.parse(bytes.subarray(0,bytesRead));
  } finally {await handle.close();}
}
const unknown = reason => ({state:'unknown',status:null,busy:null,source:null,observedAt:null,reason});
export function observeService(unit, environment, timeoutMs = 3000) {
  const result = spawnSync('systemctl',['--user','show',unit,'--property=LoadState','--property=ActiveState'],
    {env:environment,encoding:'utf8',timeout:timeoutMs,maxBuffer:4096,cwd:'/tmp',shell:false});
  if (result.error || result.status !== 0) return {state:'unknown',reason:result.error?.code === 'ETIMEDOUT' ? 'timeout' : 'service_unavailable'};
  const lines = result.stdout.trim().split('\n');
  if (lines.length !== 2 || new Set(lines.map(l=>l.split('=')[0])).size !== 2) return {state:'unknown',reason:'service_response'};
  const fields = Object.fromEntries(lines.map(l=>l.split('=')));
  if (fields.LoadState === 'not-found') return {state:'not-installed',reason:null};
  if (fields.LoadState !== 'loaded') return {state:'unknown',reason:'service_response'};
  if (fields.ActiveState === 'active') return {state:'active',reason:null};
  if (['inactive','failed'].includes(fields.ActiveState)) return {state:'inactive',reason:null};
  return {state:'unknown',reason:'service_transition'};
}
function bindingValue(b, instance, record, core) {
  if (b.schemaVersion !== 1 || b.instanceId !== instance.id || b.memberId !== record.githubId ||
      !['pending','unknown','registered'].includes(b.disposition)) invalid();
  const r = b.registration, t = b.target;
  if (!r || !t || !id(r.id) || t.id !== r.id || !['repository','organization'].includes(r.level)) invalid();
  // Registration fields are level-specific; never copy inapplicable private metadata.
  const registrationKeys = r.level === 'repository' ? ['level','id'] : ['level','id','organization'];
  if (Array.isArray(r) || Object.keys(r).some(key => !registrationKeys.includes(key))) invalid();
  const coordinates = core.repositoryCoordinates(t.owner,r.level === 'repository' ? t.repository : 'placeholder');
  if (coordinates.owner !== t.owner || r.level === 'repository' && (coordinates.repository !== t.repository ||
      !id(t.ownerId) || !['User','Organization'].includes(t.ownerType)) ||
      r.level === 'organization' && r.organization !== coordinates.owner || b.label !== core.memberRunnerLabel(record.githubId)) invalid();
  if (b.disposition === 'registered' && (!id(b.runnerId) || !['linux','windows','macos'].includes(b.platform))) invalid();
  if (b.platform !== undefined && !['linux','windows','macos'].includes(b.platform)) invalid();
  if (b.groupId !== undefined && (!id(b.groupId) || r.level !== 'organization')) invalid();
  return {disposition:b.disposition,runnerId:b.runnerId && id(b.runnerId) ? b.runnerId : null,
    memberId:b.memberId,label:b.label,platform:b.platform ?? null,
    registration:{level:r.level,id:r.id,...(r.level === 'organization' ? {organization:coordinates.owner} : {})},
    target:{id:t.id,owner:t.owner,...(r.level === 'repository' ? {repository:t.repository,ownerId:t.ownerId,ownerType:t.ownerType} : {})},
    groupId:b.groupId ?? null,
    historical:{status:['online','offline'].includes(b.status) ? b.status : null,busy:typeof b.busy === 'boolean' ? b.busy : null,
      observedAt:typeof b.observedAt === 'string' && /^\d{4}-\d\d-\d\dT[\d:.]+Z$/.test(b.observedAt) ? b.observedAt : null}};
}
export async function listEnvironments(args, dependencies = {}) {
  const result = {schemaVersion:1,complete:true,environments:[],diagnostics:[]};
  let options;
  try {options = listOptions(args);} catch {result.complete=false;result.diagnostics.push({code:'invalid_arguments'});return result;}
  const account = dependencies.account ?? userInfo();
  const root = resolve(dependencies.root ?? join(userInfo().homedir,'.local/share/nibfin/environments'));
  const problem = (code, environment, instanceId) => {
    result.complete=false;result.diagnostics.push({code,...(environment ? {environment} : {}),...(instanceId ? {instanceId} : {})});
  };
  let entries;
  try {
    await safePath(root);entries=await fs.readdir(root,{withFileTypes:true});
    const metadata=e=>e.name==='.removals' || /^\.[a-z][a-z0-9-]{0,47}\.lifecycle\.lock(?:\.recovery)?$/.test(e.name);
    for(const e of entries.filter(metadata)) await safePath(join(root,e.name),{directory:e.name==='.removals' || e.name.endsWith('.recovery')});
    entries=entries.filter(e=>!metadata(e));
    if(entries.length>128) invalid();
  }
  catch (error) {
    if (error.code === 'ENOENT' && !options.name) return result;
    problem(error.code === 'ENOENT' ? 'environment_missing' : 'storage_unavailable');return result;
  }
  if (options.name && !entries.some(e=>e.name === options.name)) {problem('environment_missing',options.name);return result;}
  const execute = async core => {
    const instanceIds = new Set(), bindings = new Set(), remote = [];
    for (const entry of entries.sort((a,b)=>a.name.localeCompare(b.name,'en'))) {
      if (options.name && entry.name !== options.name) continue;
      if (!namePattern.test(entry.name)) {problem('invalid_environment_entry');continue;}
      const name = entry.name, base = join(root,name);
      try {
        await safePath(base);
        const record = await read(join(base,'environment.json'));
        if (record.schemaVersion !== 1 || record.name !== name || !['codex','claude','gemini'].includes(record.agent) ||
            !id(record.githubId) || !Number.isSafeInteger(record.account?.uid) || typeof record.account.username !== 'string' ||
            !/^[a-zA-Z0-9_.-]{1,64}$/.test(record.account.username) || record.loginSpace !== join(base,'login') ||
            !Array.isArray(record.instances) || !record.instances.length || record.instances.length > 128) invalid();
        const out = {name,agent:record.agent,githubId:record.githubId,account:{uid:record.account.uid,username:record.account.username},instances:[]};
        result.environments.push(out);
        for (const [index,item] of record.instances.entries()) {
          if (!identity(item.id)) {problem('invalid_instance',name);continue;}
          const local = {id:item.id,registration:{disposition:'unknown'},local:{state:'unknown',reason:'local_metadata'},remote:unknown('not_registered')};
          out.instances.push(local);
          try {
            if (instanceIds.has(item.id) || item.id === name || item.loginSpace !== record.loginSpace || !['unregistered','registered'].includes(item.status)) invalid();
            instanceIds.add(item.id);
            for (const leaf of ['workspace','registration','service']) {
              if (item[leaf] !== join(base,`instance-${index+1}`,leaf)) invalid();
              await safePath(item[leaf]);
            }
            let b;
            try {b = await read(join(item.registration,'binding.json'));}
            catch (error) {if(error.code !== 'ENOENT') throw error;}
            if (b === undefined) {
              if (item.status !== 'unregistered' || item.serviceStatus !== 'not-installed') invalid();
              local.registration={disposition:'unregistered'};local.local={state:'not-installed',reason:null};
              continue;
            }
            local.registration=bindingValue(b,item,record,core);
            if (b.runnerId) {
              const key=b.registration.level+':'+b.registration.id+':'+b.runnerId;
              if(bindings.has(key)) invalid();bindings.add(key);
            }
            const expectedUnit=`nibfin-${name}-${item.id}.service`;
            const env = processEnvironment(record.loginSpace);
            env.XDG_RUNTIME_DIR=`/run/user/${account.uid}`;
            if (b.service?.account !== account.username || record.account.uid !== account.uid || record.account.username !== account.username)
              local.local={state:'unknown',reason:'account_mismatch'};
            else if (!b.service.unit && b.service.status === 'not-installed') local.local={state:'not-installed',reason:null};
            else if (b.service.unit !== expectedUnit) invalid();
            else if(process.platform !== 'linux' || b.platform !== 'linux') local.local={state:'unknown',reason:'unsupported_service'};
            else local.local=await (dependencies.service ?? observeService)(expectedUnit,env,dependencies.timeoutMs);
            local.remote=unknown(b.disposition === 'registered' ? 'not_observed' : 'registration_'+b.disposition);
            if (b.disposition === 'registered') remote.push({local,record});
          } catch {local.registration={disposition:'unknown'};local.remote=unknown('local_metadata');problem('invalid_instance_state',name,item.id);}
        }
        out.instances.sort((a,b)=>a.id.localeCompare(b.id,'en'));
      } catch {problem('invalid_environment',name);}
    }
    // Batch all local records in each scope; N15 detects contradictory remote Labels/IDs.
    if (!result.complete) {for(const m of remote)m.local.remote=unknown('incomplete_local_inventory');return;}
    const groups = Map.groupBy(remote,({local})=>JSON.stringify(local.registration.target)+':'+local.registration.registration.level);
    for (const members of groups.values()) {
      try {
        const first=members[0], r=first.local.registration;
        if (members.some(m=>m.record.account.uid !== account.uid || m.record.account.username !== account.username)) throw Error();
        const member=await githubObservation({run:dependencies.run ?? observeProcess,environment:processEnvironment(first.record.loginSpace),account});
        if(members.some(m=>m.record.githubId !== member)) {for(const m of members)m.local.remote=unknown('member_mismatch');continue;}
        const provider=core.createGithubProvider({transport:dependencies.transport ?? core.localGhTransport(),attempts:1});
        const signal=AbortSignal.timeout(dependencies.remoteTimeoutMs ?? 15000);
        if(r.registration.level === 'organization') {
          for(const m of members) {
            const b=m.local.registration;
            try {
              const o=await orgReadback(provider,'/orgs/'+b.target.owner,b.target.id,b.runnerId,b.label,undefined,signal);
              // Without repository context, groups require explicit membership verification.
              if(o.platform !== b.platform) throw Error();
              if(b.groupId) {
                const groups=await inventory(provider,'/orgs/'+b.target.owner+'/actions/runner-groups','runner_groups',b.target.id,signal);
                if(!groups.some(g=>g.id === b.groupId)) throw Error();
                const runners=await inventory(provider,'/orgs/'+b.target.owner+'/actions/runner-groups/'+b.groupId+'/runners','runners',b.target.id,signal);
                if(!runners.some(r=>r.id === b.runnerId)) throw Error();
              }
              m.local.remote={state:'verified',status:o.status,busy:o.busy,source:'github',observedAt:new Date().toISOString(),reason:null};
            } catch (error) {
              const message=error.message ?? '';
              m.local.remote=unknown(message.includes('missing/replaced') ? 'missing_or_replaced_id' : message.includes('Label/platform') ? 'member_label_or_platform' : 'organization_readback_unavailable_or_mismatch');
            }
          }
        } else {
          const observation=await core.observeRunners(provider,{target:r.target,registration:r.registration,
            records:members.map(({local})=>{const b=local.registration;return {instanceId:local.id,runnerId:b.runnerId,memberId:b.memberId,
              repositoryId:b.target.id,registration:b.registration,platform:b.platform};}),
            conditions:{platform:r.platform,labels:['self-hosted',r.platform]},correlation:'runner-list',signal});
          for(const m of members) {
            const instance=observation.instances?.find(i=>i.instanceId === m.local.id);
            const o=instance?.observation;
            if(observation.inventory === 'complete' && instance?.outcome === 'matched' && o?.id === m.local.registration.runnerId)
              m.local.remote={state:'verified',status:o.status,busy:o.busy,source:'github',observedAt:new Date().toISOString(),reason:null};
            else {
              m.local.remote=unknown(observation.outcome === 'mismatch' ? 'stale_or_mismatched_registration' : 'remote_unavailable');
              m.local.remote.diagnostics=[...(observation.diagnostics ?? []),...(instance?.diagnostics ?? [])].map(d=>({
                code:/^[a-z_]{1,80}$/.test(d.code) ? d.code : 'remote_unavailable',
                ...(d.provider && /^[a-z_]{1,40}$/.test(d.provider.kind) ? {kind:d.provider.kind} : {}),
                ...(Number.isInteger(d.provider?.status) ? {httpStatus:d.provider.status} : {})}));
            }
            if(m.local.remote.state === 'verified' && observation.diagnostics?.length)
              m.local.remote.diagnostics=observation.diagnostics.map(d=>({code:/^[a-z_]{1,80}$/.test(d.code) ? d.code : 'remote_unavailable'}));
          }
        }
      } catch {for(const m of members)m.local.remote=unknown('authentication_or_remote_unavailable');}
    }
  };
  try {await (dependencies.core ? execute(dependencies.core) : withRegistrationCore(execute));}
  catch {problem('core_unavailable');}
  return result;
}
export function listText(result) {
  const lines=[`Local inventory: ${result.complete ? 'complete' : 'incomplete'}`];
  for(const e of result.environments) {
    lines.push(`${e.name}: ${e.agent}; GitHub member ${e.githubId}; account ${e.account.username} (${e.account.uid})`);
    for(const i of e.instances) lines.push(`  ${i.id}: registration ${JSON.stringify(i.registration)}; local ${JSON.stringify(i.local)}; remote ${JSON.stringify(i.remote)}`);
  }
  for(const d of result.diagnostics)lines.push(`Diagnostic: ${JSON.stringify(d)}`);
  return lines.join('\n')+'\n';
}

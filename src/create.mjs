import fs from 'node:fs/promises';
import { join, resolve, parse, dirname } from 'node:path';
import { userInfo } from 'node:os';
import { randomUUID } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import {withCurrentCore,currentPin} from './current-core.mjs';
import {createInterface} from 'node:readline/promises';
import {Writable} from 'node:stream';
import {withEnvironmentLock} from './register.mjs';
import {loginCodexMember} from './login.mjs';

class Diagnostic extends Error {}
const fail = message => { throw new Diagnostic(message); };
const safeName = /^[a-z][a-z0-9-]{0,47}$/;
export function options(args) {
  const values = {};
  for (let i = 0; i < args.length; i += 2) {
    const key = args[i];
    if (!['--name', '--agent', '--count', '--image', '--socket','--runtime-profile'].includes(key) || key in values ||
        !args[i + 1] || args[i + 1].startsWith('--')) fail('Use --name NAME --agent AGENT [--count N]; each option exactly once.');
    values[key] = args[i + 1];
  }
  if (!safeName.test(values['--name'] ?? '')) fail('Name must be 1–48 lowercase letters/digits/hyphens, starting with a letter.');
  if (!['codex', 'claude', 'gemini'].includes(values['--agent'])) fail('Agent must be codex, claude or gemini; changing Agent requires another environment.');
  const text = values['--count'] ?? '2';
  const count = Number(text);
  if (!/^[1-9][0-9]*$/.test(text) || !Number.isSafeInteger(count) || count > 128) fail('Count must be a positive integer from 1 to 128 (total instances in this environment).');
  if(values['--image']&&!/^sha256:[a-f0-9]{64}$/.test(values['--image']))fail('Use an immutable --image sha256 digest.');
  if(values['--runtime-profile']&&(values['--agent']!=='codex'||!/^linux-amd64-codex[a-z0-9-]*$/.test(values['--runtime-profile'])))fail('Select an explicitly reviewed Codex runtime profile.');
  return {name:values['--name'],agent:values['--agent'],count,
    ...(values['--image']?{image:values['--image']}:{}),...(values['--socket']?{socket:values['--socket']}:{}),
    ...(values['--runtime-profile']?{runtimeProfile:values['--runtime-profile']}:{})};
}

// No inherited Actions tokens, provider keys, alternate login homes or Node injection.
export function processEnvironment(loginSpace, env = process.env) {
  const home = userInfo().homedir;
  return { PATH: env.PATH ?? '/usr/bin:/bin', HOME: home, LANG: 'C',
    GH_CONFIG_DIR: join(home, '.config/gh'), CODEX_HOME: loginSpace,
    CLAUDE_CONFIG_DIR: loginSpace, GEMINI_CLI_HOME: loginSpace };
}
export function observeProcess(command, args, environment) {
  const result = spawnSync(command, args, { env: environment, encoding: 'utf8', timeout: 15000, maxBuffer: 65536 });
  // Never return raw provider output to diagnostics or records.
  return { ok: !result.error && result.status === 0,
    missing: result.error?.code === 'ENOENT',
    http: /\b403\b/.test(result.stderr ?? '') ? 403 : /\b401\b/.test(result.stderr ?? '') ? 401 : undefined,
    identity: command === 'gh' && args[0] === 'api' && result.status === 0 && /^[1-9][0-9]*\s*$/.test(result.stdout ?? '') ? result.stdout.trim() : undefined };
}
export async function githubObservation({ run, environment, account }) {
  for (const tool of ['git', 'gh']) {
    if (!(await run(tool, ['--version'], environment)).ok) fail(`Missing required tool ${tool}; install it for the execution account.`);
  }
  const auth = await run('gh', ['auth', 'status', '--hostname', 'github.com'], environment);
  if (!auth.ok) fail(`gh login unavailable${auth.http ? ` (HTTP ${auth.http})` : ''}; run gh auth login as the execution/service account.`);
  const identity = await run('gh', ['api', 'user', '--jq', '.id'], environment);
  if (!identity.ok || !identity.identity) fail(`gh identity unavailable${identity.http ? ` (HTTP ${identity.http})` : ''}; verify this execution account's gh login and permissions.`);
  if (account.uid !== userInfo().uid) fail('Execution/service account mismatch; run create as the intended service account.');
  return identity.identity;
}
const authFile=agent=>({codex:'auth.json',claude:'anthropic-api-key.json',gemini:'gemini-api-key.json'})[agent];
export function backendReady(r){return r?.login==='available'&&r.tool==='available'&&['isolation','read','write'].every(k=>r[k]==='verified')
  &&Array.isArray(r.evidence)&&r.evidence.includes('diagnostic:ready')&&Array.isArray(r.limitations);}
export async function memberLogin(agent,environment){
  if(!process.stdin.isTTY||!process.stdout.isTTY)fail('Member login needs a terminal. Resume the same create command as the intended member.');
  if(agent==='codex'){
    await loginCodexMember(environment);
  }else{
    process.stdout.write(`Enter this member's ${agent} API key (hidden; retained only in this environment): `);
    const output=new Writable({write(chunk,encoding,done){done();}}),input=createInterface({input:process.stdin,output,terminal:true});
    let secret;
    try{secret=(await input.question('')).trim();}finally{input.close();process.stdout.write('\n');}
    if(!secret||/[\x00-\x20]/.test(secret)||secret.length>8192)fail('Login key missing or malformed; resume create.');
    await fs.writeFile(join(environment.CODEX_HOME,authFile(agent)),JSON.stringify({auth_mode:agent==='claude'?'anthropic-api-key':'gemini-api-key',api_key:secret}),{flag:'wx',mode:0o600});
  }
}
async function privateDirectory(path,io){
  const absolute=resolve(path);if(absolute===parse(absolute).root)fail('Choose a non-root storage directory.');
  let current=parse(absolute).root;
  for(const part of absolute.slice(current.length).split('/')){
    current=join(current,part);try{await io.mkdir(current,{mode:0o700});}catch(e){if(e.code!=='EEXIST')throw e;}
    const s=await io.lstat(current);if(!s.isDirectory()||s.isSymbolicLink())fail('Storage contains a link or non-directory.');
  }
  const s=await io.lstat(absolute);if(s.uid!==userInfo().uid||s.mode&0o077)fail('Storage must be private to the execution account.');
  return io.realpath(absolute);
}
export async function createEnvironment(input,dependencies={}){
  const optionsValue=options(input),{name,agent,count}=optionsValue;
  if(!dependencies.core)return withCurrentCore((core,context)=>createEnvironment(input,{...dependencies,core:()=>core,packageContext:context}));
  if(process.platform!=='linux'||process.arch!=='x64')fail('Unsupported host adapter: use the configured Linux/WSL execution account; no fixed WSL distribution is assumed.');
  const io=dependencies.io??fs,account=dependencies.account??userInfo(),run=dependencies.run??observeProcess;
  const configuration=dependencies.configuration??{};
  const image=optionsValue.image??configuration.image;
  const socket=optionsValue.socket??configuration.socket??`/run/user/${account.uid}/docker.sock`;
  if(!/^sha256:[a-f0-9]{64}$/.test(image??''))fail('Select the preprovisioned reviewed Agent image with --image sha256:DIGEST.');
  const core=await dependencies.core(),factory={codex:core.createLinuxCodexBackend,claude:core.createLinuxClaudeBackend,gemini:core.createLinuxGeminiBackend}[agent];
  if(typeof factory!=='function'||typeof core.createAgentEnvironments!=='function')fail('Selected Agent backend capability unavailable in the reviewed public package.');
  const runtimeProfile=optionsValue.runtimeProfile??configuration.runtimeProfile;
  if(runtimeProfile!==undefined&&(agent!=='codex'||typeof runtimeProfile!=='string'||!Object.hasOwn(core.linuxCodexProfiles??{},runtimeProfile)))fail('Runtime profile is not in the verified producer catalog.');
  const root=await privateDirectory(dependencies.root??join(userInfo().homedir,'.local/share/nibfin/environments'),io),destination=join(root,name);
  if(!dependencies.lockHeld)return withEnvironmentLock(root,name,()=>createEnvironment(input,{...dependencies,lockHeld:true}));
  const loginSpace=configuration.loginSpace??join(destination,'login'),environment=processEnvironment(loginSpace);
  const runtimeRoot=configuration.runtimeRoot??destination;
  if(configuration.loginSpace){
    if(runtimeRoot!==dirname(root)||loginSpace!==join(runtimeRoot,'member-auth'))fail('Native shared login must use the maintained private member location.');
    await privateDirectory(runtimeRoot,io);await privateDirectory(loginSpace,io);
  }
  const githubId=await githubObservation({run,environment,account});
  let pending,owned=false,journal=false;
  try{
    try{
      const s=await io.lstat(destination);if(!s.isDirectory()||s.isSymbolicLink()||s.uid!==account.uid||s.mode&0o077)fail('Environment name already exists or is unsafe.');
      try{await io.lstat(join(destination,'environment.json'));fail('Environment name already exists; creation completed.');}catch(e){if(e.code!=='ENOENT')throw e;}
      const p=join(destination,'creation.json'),ps=await io.lstat(p);
      if(!ps.isFile()||ps.isSymbolicLink()||ps.uid!==account.uid||ps.mode&0o077||ps.size>131072)fail('Pending creation is unsafe; reconcile privately.');
      pending=JSON.parse(await io.readFile(p,'utf8'));journal=true;
      try{await io.lstat(join(destination,'environment.pending'));fail('Original publication is unresolved; reconcile environment.pending before retry.');}catch(e){if(e.code!=='ENOENT')throw e;}
      if(pending.runtimeResources?.some(r=>r.status!=='removed'))fail('Retained runtime resources require original-operation reconciliation before retry.');
      if(pending.version!==1||pending.name!==name||pending.agent!==agent||pending.count!==count||pending.githubId!==githubId||pending.uid!==account.uid
        ||(pending.loginSpace??join(destination,'login'))!==loginSpace||pending.image!==image||pending.socket!==socket||pending.core!==currentPin.coreSha256
        ||(pending.runtimeProfile??null)!==(runtimeProfile??null))fail('Environment name already exists with a different account/Agent/count/runtime; preserve its pending creation.');
    }catch(e){if(e.code!=='ENOENT')throw e;if(pending)throw e;
      await io.mkdir(destination,{mode:0o700});owned=true;if(!configuration.loginSpace)await io.mkdir(loginSpace,{mode:0o700});
      const instances=[];
      for(let n=0;n<count;n++){
        const base=join(destination,`instance-${n+1}`);await io.mkdir(base,{mode:0o700});
        const instance={id:(dependencies.id??randomUUID)(),workspace:join(base,'workspace'),registration:join(base,'registration'),service:join(base,'service')};
        for(const path of [instance.workspace,instance.registration,instance.service])await io.mkdir(path,{mode:0o700});instances.push(instance);
      }
      core.createAgentEnvironments().describe({id:name,agent,loginSpace,instances});
      pending={version:1,name,agent,count,githubId,uid:account.uid,image,socket,loginSpace,core:currentPin.coreSha256,instances,...(runtimeProfile?{runtimeProfile}:{})};
      await io.writeFile(join(destination,'creation.json'),JSON.stringify(pending),{flag:'wx',mode:0o600});journal=true;
    }
    const expected=pending.instances.map((i,n)=>({id:i.id,...Object.fromEntries(['workspace','registration','service'].map(k=>[k,join(destination,`instance-${n+1}`,k)]))}));
    if(expected.length!==count||JSON.stringify(expected)!==JSON.stringify(pending.instances))fail('Pending instance paths/count changed; reconcile.');
    for(const path of [loginSpace,...expected.flatMap(i=>[i.workspace,i.registration,i.service])]){
      const s=await io.lstat(path);if(!s.isDirectory()||s.isSymbolicLink()||s.uid!==account.uid||s.mode&0o077||await io.realpath(path)!==path)fail('Pending resource ownership/layout changed.');
    }
    const descriptor=core.createAgentEnvironments().describe({id:name,agent,loginSpace,instances:expected});
    try{const s=await io.lstat(join(loginSpace,authFile(agent)));if(!s.isFile()||s.isSymbolicLink()||s.uid!==account.uid||s.mode&0o077)fail('Member login file must be private.');}
    catch(e){if(e.code!=='ENOENT')throw e;await (dependencies.login??memberLogin)(agent,environment);}
    const reports=[];
    const retain=async()=>{await io.writeFile(join(destination,'creation.pending'),JSON.stringify(pending),{flag:'wx',mode:0o600});await io.rename(join(destination,'creation.pending'),join(destination,'creation.json'));};
    for(const instance of descriptor.instances){
      const dispatch=dependencies.backendProcess??core.codexProcess;
      const backend=factory({root:runtimeRoot,image,socket,environment:descriptor,instanceId:instance.id,
        ...(runtimeProfile?{runtimeProfile}:{}),
        task:()=>{throw Error('Creation has no delivery authority.');},process:async request=>{
          const args=request.args.slice(2);let resource;
          if(args[0]==='create')resource={kind:'container',name:args[args.indexOf('--name')+1],instance:instance.id,status:'unknown'};
          if(args[0]==='network'&&args[1]==='create')resource={kind:'network',name:args.at(-1),instance:instance.id,status:'unknown'};
          if(resource){(pending.runtimeResources??=[]).push(resource);await retain();}
          return dispatch(request);
        },egress:{image,node:true}});
      let report,cleaned;
      try{report=await backend.readiness();}
      finally{
        // Readiness owns disposable probes/gateways only. The Core cleanup owner
        // removes its resources; recreate the still-unregistered empty spaces.
        cleaned=await backend.cleanup({environment:descriptor.id,instance:instance.id,resources:[instance.workspace,instance.registration,instance.service]});
        if(cleaned.status==='completed'){
          for(const path of [instance.workspace,instance.registration,instance.service])await io.mkdir(path,{mode:0o700});
          for(const r of pending.runtimeResources??[])if(r.instance===instance.id)r.status='removed';await retain();
        }
      }
      if(cleaned?.status!=='completed')fail('Readiness cleanup unresolved; retain original resource identities for reconciliation.');
      if(!backendReady(report))fail('Agent/backend not ready: check member login, pinned tools/image and isolated model/dependency egress.');reports.push(report);
    }
    // The shared preflight is a separate contract from backend readiness. Each
    // verified network capability comes from the selected backend's complete probe.
    const preflightInput={version:1,platform:'linux',arch:'x64',path:core.supportedRunnerDefaults.path,interpreter:core.supportedRunnerDefaults.interpreter,
      account:{uid:account.uid,user:account.username,githubUserId:githubId},work:expected[0].workspace,scratch:expected[0].service,cache:expected[0].registration};
    const preflight=await core.preflightRunner({configuration:core.runnerConfiguration(preflightInput),
      observe:dependencies.observeLocal??core.observeRunnerLocal,
      probe:async({capability})=>({status:reports.length===count&&reports.every(backendReady)?'verified':'blocked',...(capability==='github-identity'?{userId:githubId}:{})})});
    if(preflight.status!=='ready')fail('Packaged preflight blocked this account/tools/network configuration.');
    const record={schemaVersion:1,name,agent,loginSpace,...(configuration.loginSpace?{memberLogin:'shared-native-v1'}:{}),githubId,account:{uid:account.uid,username:account.username},
      runtime:{image,socket,core:currentPin.coreSha256,...(runtimeProfile?{runtimeProfile}:{})},instances:descriptor.instances.map(i=>({...i,loginSpace,status:'unregistered',serviceStatus:'not-installed'}))};
    await io.writeFile(join(destination,'environment.pending'),JSON.stringify(record,null,2)+'\n',{flag:'wx',mode:0o600});
    await io.rename(join(destination,'environment.pending'),join(destination,'environment.json'));await io.rm(join(destination,'creation.json'));
    return record;
  }catch(error){
    if(!journal&&owned){try{await io.rm(destination,{recursive:true,force:true});}catch{fail(`Creation failed; cleanup incomplete for ${name}. Preserve member credentials and inspect its private directory.`);}}
    if(journal)fail(`Creation incomplete for ${name}; allocated instances and member login are retained. ${error instanceof Diagnostic?error.message:'Backend or local operation failed without confirmed completion.'} Resume the same create command/count/image after resolving login/tools/isolation. Reconcile any environment.pending file before retrying.`);
    if(error instanceof Diagnostic)throw error;fail('Creation failed before readiness; inspect private resources and retry safely.');
  }
}

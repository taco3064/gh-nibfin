import fs from 'node:fs/promises';import{join}from'node:path';import{userInfo}from'node:os';import{spawn}from'node:child_process';
import{readNativeMember}from'./native-member.mjs';import{privatePath}from'./local-state.mjs';import{backendReady}from'./create.mjs';
import{retainUnacceptedWorkspace,restoreUnacceptedWorkspace}from'./native-unaccepted-work.mjs';
import{resumeNativeSession}from'./native-session-resume.mjs';
import{prepareNativeCleanBase}from'./native-clean-base.mjs';
import{prepareDeferredNativeRole}from'./native-deferred-role.mjs';
import{nativeRoleSummaries}from'./native-role-summary.mjs';
const roles=['shape','shape-validator','shape-diagnosis','shaper-review','deliver','accept'];
const need=(ok,code)=>{if(!ok)throw Error('native-role-host:'+code);};

// Older, already-authorized Core pins carry the trusted role in metadata but
// omit its name from the prompt. Project that existing assignment, as the
// controller does upstream, without changing any receipt or authority binding.
export function nativeAssignedRolePrompt(core,value,{sourceOnly=false}={}){
 const input=value.input,role=input.execution.role;
 need(roles.includes(role)&&core.skillForRole(role)===input.execution.skill
  &&input.binding.instructions===input.instructions.identity
  &&core.bodyDigest(input.instructions.content)===input.instructions.identity,'protected-role-assignment');
 const header='Core execution role: '+role+'. Core Skill identity: '+input.instructions.identity+'.';
 const format=['shape','shape-validator'].includes(role)?'Empty Candidate arrays require separate explanation lines beginning exactly "Candidate artifacts applicability:" and "Candidate hooks applicability:" in the Shape plan, outside code fences. Preserve the exact Scope.\n':'';
 need(typeof value.prompt==='string','protected-role-prompt');
 const preparation=sourceOnly?'Verification preparation applicability (trusted source observation): this repository root has neither package.json nor package-lock.json. The generic locked npm preparation helper is not applicable to these dependency-free source checks. As in the upstream verification contract, npm preparation is mandatory for locked Node projects and required nested projects, not every command implemented in Node. Run the existing Node/Python source regression harness through node /verification-runner.mjs -- <absolute executable> [arguments]; use its bounded temporary fixtures or an exact writable source copy when required. Keep /work identities unchanged during verification, retain real command receipts, and reverify changed bytes. Do not invent package manifests, skip required tests, run dependency scripts, bypass the guarded lane or report preparation as a pass. If a command needs dependencies or mutates source, use its appropriate bounded preparation; report an actual unsupported requirement truthfully.\n':'';
 // The upstream Shape contract scales verification to the actual change and
 // requires the Validator to check feasibility before publishing a plan.
 // Supply the missing runtime boundary to both read-only plan owners; an
 // isolated Agent is not a registered user service or an installed host.
 const planning=['shape','shape-validator'].includes(role)?'Verification planning: scale criteria and investigation to the actual Scope. A small change requires precise, compact proof, not invented prerequisites or universal full-suite testing. Inspect the test harness and its real prerequisites before making a command mandatory. This Agent runs in an isolated container; it is not the host service account, user systemd manager, registered Runner or installed consumer. A rootless daemon does not imply a non-root UID inside its container. Do not require host registration or service-manager tests inside this Agent without observed support. Distinguish applicable focused local checks from mandatory exact-head CI and separately scoped host/provider verification. Local proof cannot replace independent Acceptance, hooks or required CI; unavailable essential proof must be reported truthfully. Validator: independently confirm feasibility and reject a plan that invents verification capabilities.\n':'';
 const prefix=header+'\n'+format+preparation+planning;
 if(value.prompt.startsWith(prefix))return value;
 const original=value.prompt.startsWith(header+'\n')?value.prompt.slice(header.length+1):value.prompt;
 return{...value,prompt:prefix+original};
}
export function nativeReadBrokerDirectory(lease){
 need(/^[a-f0-9]{64}$/.test(lease),'broker-lease');
 const directory=join('/run/user/'+userInfo().uid,'nibfin-gh-'+lease);
 need(Buffer.byteLength(directory+'/read.sock')<108,'broker-path');return directory;
}
export async function publicCheckout(repository,commit,path,signal){
 need(/^[a-z0-9-]+\/[a-z0-9_.-]+$/.test(repository)&&/^[a-f0-9]{40}$/.test(commit),'checkout-binding');
 await privatePath(path,{directory:true});need((await fs.readdir(path)).length===0,'existing-workspace');
 const git=async args=>new Promise((resolve,reject)=>{
  const child=spawn('/usr/bin/git',['-c','core.hooksPath=/dev/null','-c','protocol.file.allow=never',...args],{cwd:path,detached:true,
   env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:path,LANG:'C',GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null',GIT_TERMINAL_PROMPT:'0'},stdio:'ignore'});
  const stop=()=>{if(child.pid)try{process.kill(-child.pid,'SIGKILL');}catch(e){if(e.code!=='ESRCH')throw e;}};
  const timer=setTimeout(stop,120000);signal?.addEventListener('abort',stop,{once:true});if(signal?.aborted)stop();
  const cleanup=()=>{clearTimeout(timer);signal?.removeEventListener('abort',stop);};
  child.once('error',()=>{cleanup();reject(Error('native-role-host:source-acquisition'));});
  child.once('close',code=>{cleanup();code===0&&!signal?.aborted?resolve():reject(Error('native-role-host:source-acquisition'));});
 });
 await git(['init','--template=','--object-format=sha1','.']);await git(['fetch','--no-tags','--depth=1','https://github.com/'+repository+'.git',commit]);
 await git(['checkout','--detach',commit]);await git(['config','core.hooksPath','/dev/null']);
}

// One maintained composition of the public Core role host. Physical slots are
// separate from the six isolated role workspaces. Only the same member's model
// login is shared, and the Agent gets a finite public read broker, never gh auth.
export async function nativeRoleHost(core,{root,binding,base,repository,memberId,source,retain,checkpoint,signal,store,progress=async()=>{}}={}){
 need(core.distributionAPI?.nativeHost===1,'compatible-native-core-required');
 need(/^[a-f0-9]{64}$/.test(binding.jobRecord?.lease?.identity??'')&&typeof retain==='function'&&typeof checkpoint==='function','composition');
 const member=await readNativeMember(core,{root,memberId}),directory=join(root,'jobs',binding.jobRecord.lease.identity,'roles');
 await fs.mkdir(directory,{mode:0o700});
 const allocatedDirectory=await fs.lstat(directory);
 let broker,brokerPath;
 try{
 const instances=[];
 for(const role of roles){const parent=join(directory,role);await fs.mkdir(parent,{mode:0o700});
  const instance={id:binding.request.id+'-'+role,workspace:join(parent,'work'),registration:join(parent,'registration'),service:join(parent,'service')};
  for(const path of [instance.workspace,instance.registration,instance.service])await fs.mkdir(path,{mode:0o700});instances.push(instance);}
 const environment=core.createAgentEnvironments().describe({id:'native-'+binding.request.id,agent:'codex',loginSpace:member.loginSpace,instances});
 const sourceDirectory=join(directory,'source');await fs.mkdir(sourceDirectory,{mode:0o700});await publicCheckout(repository,base,sourceDirectory,signal);
 if(store)await prepareDeferredNativeRole(core,{store,binding,memberId});
 const candidateDirectory=join(directory,'candidate-source');await fs.mkdir(candidateDirectory,{mode:0o700});
 const runnerRoot=join(directory,'runner');await fs.mkdir(runnerRoot,{mode:0o700});
 for(const name of ['work','scratch','cache'])await fs.mkdir(join(runnerRoot,name),{mode:0o700});
 const runner={version:1,platform:'linux',arch:'x64',account:{user:userInfo().username,uid:userInfo().uid,githubUserId:memberId},
  path:core.supportedRunnerDefaults.path,interpreter:core.supportedRunnerDefaults.interpreter,
  work:join(runnerRoot,'work'),scratch:join(runnerRoot,'scratch'),cache:join(runnerRoot,'cache')};
 brokerPath=nativeReadBrokerDirectory(binding.jobRecord.lease.identity);broker=await core.startGithubReadBroker({directory:brokerPath,
  repositories:binding.configuration.targets.map(t=>t.repository),environment:{PATH:runner.path,HOME:userInfo().homedir}});
 const backend=instance=>({root:member.base,image:member.image,socket:'/run/user/'+userInfo().uid+'/docker.sock',runtimeProfile:member.runtimeProfile,
  environment,instanceId:instance.id,egress:{image:member.image,node:true},githubRead:{directory:brokerPath,repositories:binding.configuration.targets.map(t=>t.repository)}});
 const summaries=nativeRoleSummaries(core,{retain});
 const native=core.createNativeRoleBindings({ownership:binding.ownerType==='Organization'?'organization':'personal',source,retain,
  allocate:async role=>{const s=store?await store.read():null,resume=s?.sessionResumes?.[role];
   if(!resume?.next||resume.next.request!==binding.request.id)return null;
   need(resume.next.role===role&&resume.next.memberId===memberId,'recovery-execution');
   return{id:resume.next.execution,request:resume.next.request,result:resume.next.result};}});
 const rootPackages=await Promise.all(['package.json','package-lock.json'].map(async name=>{try{await fs.lstat(join(sourceDirectory,name));return true;}catch(e){if(e.code==='ENOENT')return false;throw e;}}));
 const sourceOnly=rootPackages.every(p=>!p);
 const profiles=roles.map((role,i)=>({id:role,agent:'codex',os:'linux',backend:backend(instances[i])}));
 let observed,probeCleanup=true;
 async function probe(){
  if(!observed)observed=(async()=>{
   const parent=join(directory,'readiness');await fs.mkdir(parent,{mode:0o700});const instance={id:'readiness-'+binding.request.id,
    workspace:join(parent,'work'),registration:join(parent,'registration'),service:join(parent,'service')};
   for(const path of [instance.workspace,instance.registration,instance.service])await fs.mkdir(path,{mode:0o700});
   const descriptor=core.createAgentEnvironments().describe({id:instance.id+'-environment',agent:'codex',loginSpace:member.loginSpace,instances:[instance]});
   const selected=core.createLinuxCodexBackend({...backend(instance),environment:descriptor,task:()=>{throw Error('Readiness is not a role task');}});
   let report,cleanup;probeCleanup=false;try{report=await selected.readiness();}
   finally{cleanup=await selected.cleanup({environment:descriptor.id,instance:instance.id,resources:[instance.workspace,instance.registration,instance.service]});}
   need(cleanup?.status==='completed','readiness-cleanup');probeCleanup=true;await retain({version:1,kind:'native-readiness',report,cleanup});
   need(backendReady(report),'preflight');return report;
  })();return observed;
 }
 const prepared=new Map();
 const host=core.createProtectedRoleHost({configuration:{runner,profiles,assignments:Object.fromEntries(roles.map(role=>[role,role]))},
  collectUntilTerminal:true,signal,checkpoint,retain:summaries.retain,
  sessions:{
   prepare:async({task,memberId:actualMember})=>{
    need(actualMember===memberId,'session-member');const instance=instances[roles.indexOf(task.role)],directory=join(instance.service,'session');
    await fs.mkdir(directory,{mode:0o700});return store?resumeNativeSession(core,{store,role:task.role,memberId,directory,request:binding.request.id}):{directory,id:null,memberId};
   },
   retain:async record=>{
    need(record.memberId===memberId,'session-member');
    const snapshot=core.captureRescueSession({directory:record.directory,memberId,role:record.role,session:record.id,execution:record.execution,adapter:record.adapter});
    await retain({version:1,kind:'native-role-session',metadata:record,snapshot});return {outcome:'retained'};
   }
  },
  probe:async({capability})=>{
   const report=await probe();if(capability==='github-identity'){
    const result=await core.createGithubProvider({attempts:1}).request({endpoint:'/user',target:memberId,operation:'native-role-preflight',correlation:binding.request.id,signal});
    need(result.outcome==='read'&&result.data.id===memberId,'execution-account');
   }
   return {status:backendReady(report)?'verified':'blocked',...(capability==='github-identity'?{userId:memberId}:{})};
  },
  bind:async task=>{const value=await native.bind(task);need(core.canonicalJSON(value.input.execution.source)===core.canonicalJSON(source),'source-attribution');return nativeAssignedRolePrompt(core,summaries.bind(value),{sourceOnly});}
 });
 return Object.freeze({
  sourceDirectory,candidateDirectory,
  workspace(role){need(roles.includes(role),'role');return instances[roles.indexOf(role)].workspace;},
  async prepareRole(task){
   const path=this.workspace(task.role);
   const s=store?await store.read():null,resume=s?.sessionResumes?.[task.role];
   if(task.role==='shape'&&s?.shapeRevision)task={...task,task:{...task.task,revision:{kind:s.shapeRevision.kind??'unpublished-proposal-format-feedback',
    previousBody:s.shapeRevision.previousBody,diagnostics:s.shapeRevision.diagnostics,
    instruction:s.shapeRevision.kind==='owner-published-plan-revision'?
     'Revise the original proposal using the explicit Owner verification-applicability concern. Preserve the exact Scope, substantive implementation and all existing independent Acceptance, hook, exact-head CI, protected merge and closure gates. Resolve the diagnosed verification mismatch using the supplied source/runtime evidence; obtain a fresh independent Validator before publication. Previous implementation is retained as UNACCEPTED input for the Writer.':
     'Revise the original proposal using this machine feedback. Preserve Scope and substantive plan; make the empty-inventory applicability explanations parseable. A fresh independent Validator and all original publication gates remain required.'}}};
   if(task.role==='deliver'&&resume?.next?.request===binding.request.id&&resume.workspaceReference){
    const original=await store.get(resume.workspaceReference),session=await store.get(resume.reference);
    await restoreUnacceptedWorkspace({directory:path,snapshot:original,execution:session.metadata.execution});
    await prepareNativeCleanBase(core,{directory:path,sourceDirectory,base,state:s,snapshot:original,reference:resume.workspaceReference,retain});
   }else if(['shape','shape-validator','shape-diagnosis','deliver'].includes(task.role))await publicCheckout(repository,base,path,signal);
   if(resume?.next?.request===binding.request.id)task={...task,task:{...task.task,recovery:{caseId:resume.caseId,plan:resume.plan,
    status:'UNACCEPTED',instruction:'Diagnosis is guidance only; all original role gates and verification still apply.'}}};
   await checkpoint();const value=await host.prepareRole(task);prepared.set(value.input.execution.id,value);
   await progress({status:'running',reason:'native-'+task.role+'-launch-prepared'});return value;
  },
  get:id=>{need(prepared.has(id),'execution');return host.get(id);},
  async close({preserveWriter=false}={}){
   let settled=probeCleanup;
   for(const [id,value]of prepared){try{
    let observed=await value.lifecycle.termination(id);
    if(observed.termination.status!=='terminated'){await value.lifecycle.stop(id);const until=Date.now()+30000;
     do{await new Promise(r=>setTimeout(r,200));observed=await value.lifecycle.termination(id);}while(observed.termination.status!=='terminated'&&Date.now()<until);}
    if(observed.termination.status!=='terminated'){settled=false;continue;}
    if(['pending','unknown','unresolved'].includes(observed.result.status))await value.lifecycle.collect(id);
    if(preserveWriter&&value.input.execution.role==='deliver')await retainUnacceptedWorkspace({directory:this.workspace('deliver'),execution:value.input.execution,retain});
    const cleanup=await value.lifecycle.cleanup();if(cleanup.status!=='completed')settled=false;
   }catch{settled=false;}}
   await new Promise(resolve=>broker.close(resolve));
   // Node owns/removes its listening socket; retain any unexpected replacement.
   try{if((await fs.readdir(brokerPath)).length===0)await fs.rmdir(brokerPath);else settled=false;}catch{settled=false;}
   need(settled,'resources-unresolved');
   // Original conversations, rejected Writer bytes and verification records
   // were retained before the maintained cleanup owners acknowledged release.
   // The remaining source/probe/unused-role trees are disposable job resources.
   await privatePath(directory,{directory:true});const current=await fs.lstat(directory);
   need(current.ino===allocatedDirectory.ino&&current.dev===allocatedDirectory.dev,'workspace-directory-changed');
   await fs.rm(directory,{recursive:true});return {status:'completed'};
  }
 });
 }catch(error){
  // Construction has not exposed any lifecycle to Core and cannot launch a
  // model. Close the read broker before retiring this exact disposable tree.
  if(broker){await new Promise(resolve=>broker.close(resolve));await fs.rmdir(brokerPath);}
  await privatePath(directory,{directory:true});const current=await fs.lstat(directory);
  need(current.ino===allocatedDirectory.ino&&current.dev===allocatedDirectory.dev,'workspace-directory-changed');
  await fs.rm(directory,{recursive:true});throw error;
 }
}

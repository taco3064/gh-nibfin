import fs from 'node:fs/promises';import{join}from'node:path';import{userInfo}from'node:os';import{spawn}from'node:child_process';
import{readNativeMember}from'./native-member.mjs';import{privatePath}from'./local-state.mjs';import{backendReady}from'./create.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-role-host:'+code);};
const roles=['shape','shape-validator','shape-diagnosis','shaper-review','deliver','accept'];
async function publicCheckout(repository,commit,path,signal){
 need(/^[a-z0-9-]+\/[a-z0-9_.-]+$/.test(repository)&&/^[a-f0-9]{40}$/.test(commit),'checkout-binding');
 await privatePath(path,{directory:true});need((await fs.readdir(path)).length===0,'existing-workspace');
 const git=async args=>new Promise((resolve,reject)=>{
  const child=spawn('/usr/bin/git',['-c','core.hooksPath=/dev/null','-c','protocol.file.allow=never',...args],{cwd:path,signal,
   env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:path,LANG:'C',GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null',GIT_TERMINAL_PROMPT:'0'},stdio:'ignore'});
  const timer=setTimeout(()=>child.kill('SIGKILL'),120000);child.once('error',()=>{clearTimeout(timer);reject(Error('native-role-host:source-acquisition'));});
  child.once('close',code=>{clearTimeout(timer);code===0?resolve():reject(Error('native-role-host:source-acquisition'));});
 });
 await git(['init','--template=','--object-format=sha1','.']);await git(['fetch','--no-tags','--depth=1','https://github.com/'+repository+'.git',commit]);
 await git(['checkout','--detach',commit]);await git(['config','core.hooksPath','/dev/null']);
}

// One maintained composition of the public Core role host. Physical slots are
// separate from the six isolated role workspaces. Only the same member's model
// login is shared, and the Agent gets a finite public read broker, never gh auth.
export async function nativeRoleHost(core,{root,binding,base,repository,memberId,source,retain,checkpoint,signal,progress=async()=>{}}={}){
 need(core.distributionAPI?.nativeHost===1,'compatible-native-core-required');
 need(/^[a-f0-9]{64}$/.test(binding.jobRecord?.lease?.identity??'')&&typeof retain==='function'&&typeof checkpoint==='function','composition');
 const member=await readNativeMember(core,{root,memberId}),directory=join(root,'jobs',binding.jobRecord.lease.identity,'roles');
 await fs.mkdir(directory,{mode:0o700});
 const instances=[];
 for(const role of roles){const parent=join(directory,role);await fs.mkdir(parent,{mode:0o700});
  const instance={id:binding.request.id+'-'+role,workspace:join(parent,'work'),registration:join(parent,'registration'),service:join(parent,'service')};
  for(const path of [instance.workspace,instance.registration,instance.service])await fs.mkdir(path,{mode:0o700});instances.push(instance);}
 const environment=core.createAgentEnvironments().describe({id:'native-'+binding.request.id,agent:'codex',loginSpace:member.loginSpace,instances});
 const runnerRoot=join(directory,'runner');await fs.mkdir(runnerRoot,{mode:0o700});
 for(const name of ['work','scratch','cache'])await fs.mkdir(join(runnerRoot,name),{mode:0o700});
 const runner={version:1,platform:'linux',arch:'x64',account:{user:userInfo().username,uid:userInfo().uid,githubUserId:memberId},
  path:core.supportedRunnerDefaults.path,interpreter:core.supportedRunnerDefaults.interpreter,
  work:join(runnerRoot,'work'),scratch:join(runnerRoot,'scratch'),cache:join(runnerRoot,'cache')};
 const brokerPath=join(directory,'github-read'),broker=await core.startGithubReadBroker({directory:brokerPath,
  repositories:binding.configuration.targets.map(t=>t.repository),environment:{PATH:runner.path,HOME:userInfo().homedir}});
 const backend=instance=>({root:member.base,image:member.image,socket:'/run/user/'+userInfo().uid+'/docker.sock',runtimeProfile:member.runtimeProfile,
  environment,instanceId:instance.id,egress:{image:member.image,node:true},githubRead:{directory:brokerPath,repositories:binding.configuration.targets.map(t=>t.repository)}});
 const native=core.createNativeRoleBindings({ownership:binding.ownerType==='Organization'?'organization':'personal',source,retain});
 const profiles=roles.map((role,i)=>({id:role,agent:'codex',os:'linux',backend:backend(instances[i])}));
 let observed;
 async function probe(){
  if(!observed)observed=(async()=>{
   const parent=join(directory,'readiness');await fs.mkdir(parent,{mode:0o700});const instance={id:'readiness-'+binding.request.id,
    workspace:join(parent,'work'),registration:join(parent,'registration'),service:join(parent,'service')};
   for(const path of [instance.workspace,instance.registration,instance.service])await fs.mkdir(path,{mode:0o700});
   const descriptor=core.createAgentEnvironments().describe({id:instance.id+'-environment',agent:'codex',loginSpace:member.loginSpace,instances:[instance]});
   const selected=core.createLinuxCodexBackend({...backend(instance),environment:descriptor,task:()=>{throw Error('Readiness is not a role task');}});
   let report,cleanup;try{report=await selected.readiness();}
   finally{cleanup=await selected.cleanup({environment:descriptor.id,instance:instance.id,resources:[instance.workspace,instance.registration,instance.service]});}
   need(cleanup?.status==='completed','readiness-cleanup');await retain({version:1,kind:'native-readiness',report,cleanup});
   need(backendReady(report),'preflight');return report;
  })();return observed;
 }
 const prepared=new Map();
 const host=core.createProtectedRoleHost({configuration:{runner,profiles,assignments:Object.fromEntries(roles.map(role=>[role,role]))},
  collectUntilTerminal:true,signal,checkpoint,retain,
  sessions:{
   prepare:async({task,memberId:actualMember})=>{
    need(actualMember===memberId,'session-member');const instance=instances[roles.indexOf(task.role)],directory=join(instance.service,'session');
    await fs.mkdir(directory,{mode:0o700});return {directory,id:null,memberId};
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
  bind:async task=>{const value=await native.bind(task);need(core.canonicalJSON(value.input.execution.source)===core.canonicalJSON(source),'source-attribution');return value;}
 });
 return Object.freeze({
  workspace(role){need(roles.includes(role),'role');return instances[roles.indexOf(role)].workspace;},
  async prepareRole(task){
   const path=this.workspace(task.role);
   if(['shape','shape-validator','shape-diagnosis','deliver'].includes(task.role))await publicCheckout(repository,base,path,signal);
   await checkpoint();const value=await host.prepareRole(task);prepared.set(value.input.execution.id,value);
   await progress({status:'running',reason:'native-'+task.role+'-launch-prepared'});return value;
  },
  get:id=>{need(prepared.has(id),'execution');return host.get(id);},
  async close(){
   let settled=true;
   for(const [id,value]of prepared){try{
    let observed=await value.lifecycle.termination(id);
    if(observed.termination.status!=='terminated'){await value.lifecycle.stop(id);const until=Date.now()+30000;
     do{await new Promise(r=>setTimeout(r,200));observed=await value.lifecycle.termination(id);}while(observed.termination.status!=='terminated'&&Date.now()<until);}
    if(observed.termination.status!=='terminated'){settled=false;continue;}
    if(['pending','unknown','unresolved'].includes(observed.result.status))await value.lifecycle.collect(id);
    const cleanup=await value.lifecycle.cleanup();if(cleanup.status!=='completed')settled=false;
   }catch{settled=false;}}
   await new Promise(resolve=>broker.close(resolve));
   // Node owns/removes its listening socket; retain any unexpected replacement.
   try{if((await fs.readdir(brokerPath)).length===0)await fs.rmdir(brokerPath);else settled=false;}catch{settled=false;}
   need(settled,'resources-unresolved');return {status:'completed'};
  }
 });
}

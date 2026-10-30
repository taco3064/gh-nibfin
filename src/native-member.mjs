import fs from 'node:fs/promises';
import{join,dirname}from'node:path';import{userInfo}from'node:os';
import{createEnvironment}from'./create.mjs';
import{ensurePrivateDirectory,readPrivate,writePrivate,privatePath}from'./local-state.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-member:'+code);};
const name='native-member';
export async function readNativeMember(core,{root,memberId}={}){
 const base=dirname(root),record=await readPrivate(join(root,'member-runtime.json'));
 const {identity,...body}=record;
 need(core.contentIdentity(body)===identity&&record.version===1&&record.name===name&&record.memberId===memberId
  &&record.agent==='codex'&&/^sha256:[a-f0-9]{64}$/.test(record.image)
  &&Object.hasOwn(core.linuxCodexProfiles,record.runtimeProfile),'configuration');
 const path=join(base,'environments',name,'environment.json'),environment=await readPrivate(path);
 need(environment.schemaVersion===1&&environment.name===name&&environment.agent===record.agent
  &&environment.githubId===memberId&&environment.account.uid===userInfo().uid&&environment.account.username===userInfo().username
  &&environment.loginSpace===join(base,'member-auth')&&environment.runtime.image===record.image
  &&environment.runtime.runtimeProfile===record.runtimeProfile&&environment.runtime.socket==='/run/user/'+userInfo().uid+'/docker.sock'
  &&Array.isArray(environment.instances)&&environment.instances.length===record.capacity,'environment');
 await privatePath(environment.loginSpace,{directory:true});await privatePath(join(environment.loginSpace,'auth.json'));
 core.createAgentEnvironments().describe({id:name,agent:record.agent,loginSpace:environment.loginSpace,
  instances:environment.instances.map(({id,workspace,registration,service})=>({id,workspace,registration,service}))});
 return {...record,base,loginSpace:environment.loginSpace};
}

// Ordinary init calls the maintained create/login owner; no credential copying,
// manual capability module, alternate installer or new command is introduced.
export async function prepareNativeMember(core,{root,context,memberId,capacity,image,runtimeProfile}={}){
 const previous=await readPrivate(join(root,'member-runtime.json'),{missing:true});
 if(previous){
  need((!image||previous.image===image)&&(!runtimeProfile||previous.runtimeProfile===runtimeProfile)
   &&previous.capacity===capacity,'preserve-existing-runtime');return readNativeMember(core,{root,memberId});
 }
 if(image===undefined)return {status:'unconfigured'};
 need(/^sha256:[a-f0-9]{64}$/.test(image)&&Object.hasOwn(core.linuxCodexProfiles,runtimeProfile),'supported-runtime-required');
 const base=dirname(root),loginSpace=join(base,'member-auth');await ensurePrivateDirectory(loginSpace);
 const environmentRoot=join(base,'environments'),existing=await readPrivate(join(environmentRoot,name,'environment.json'),{missing:true});
 if(!existing)await createEnvironment(['--name',name,'--agent','codex','--count',String(capacity),'--image',image,'--runtime-profile',runtimeProfile],
  {core:async()=>core,packageContext:context,root:environmentRoot,configuration:{image,runtimeProfile,loginSpace,runtimeRoot:base}});
 const body={version:1,name,agent:'codex',memberId,capacity,image,runtimeProfile};
 await writePrivate(join(root,'member-runtime.json'),{...body,identity:core.contentIdentity(body)},{exclusive:true});
 return readNativeMember(core,{root,memberId});
}

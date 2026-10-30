import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {pathToFileURL} from 'node:url';
import {spawn} from 'node:child_process';
import {privatePath,readPrivate,writePrivate,sha} from './local-state.mjs';
import {maintenanceLock} from './upgrade.mjs';
import {verifyNativeRuntime} from './native-control-service.mjs';
import {nativePoolStore} from './native-pool-store.mjs';
import {verifyRunnerDistribution} from './runner-distribution.mjs';
import {checkAdmission} from './admission.mjs';
import {nativeJitConfiguration,nativeRunnerEnvironment} from './native-jit.mjs';
import {nativeRunnerService} from './native-runner-service.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-runner:'+code);};
/** Fixed maintained service entry. A private launch intent is single-use even
 * after a crash. The exact JIT files are privately materialized before invoking
 * the official configured Listener. No credential goes into argv or environment.
 * Process termination is recorded; GitHub Job success and model-resource cleanup
 * must still be independently observed by the pool owner before slot release.
 */
export async function runNativeRunner(root,selector,{signal,invocation=process.env.INVOCATION_ID,
 verify=verifyRunnerDistribution,admissionCheck=checkAdmission,serviceState}={}){
 need(process.platform==='linux'&&process.arch==='x64'&&process.versions.node.split('.')[0]==='22'&&userInfo().uid!==0,'platform');
 need(typeof selector==='string'&&/^[A-Za-z0-9_-]{1,80}$/.test(selector)&&/^[a-f0-9]{32}$/.test(invocation??''),'service-invocation');
 await privatePath(root,{directory:true});const installation=await readPrivate(join(root,'native-installation.json'));
 need(installation.executionPool?.version===2,'repository-local-pool');await verifyNativeRuntime(root,installation);
 const core=await import(pathToFileURL(join(root,'runtime/node_modules/@nibfin/core/src/index.mjs')).href);
 const store=nativePoolStore(core,{root,configuration:installation.executionPool}),record=await store.jobs.read(selector);
 need(record&&['launch-intent','active'].includes(record.phase)&&record.request.id===selector
  &&/^[a-f0-9]{64}$/.test(record.lease?.identity??'')&&Number.isSafeInteger(record.lease.slot)
  &&record.lease.slot>=1&&record.lease.slot<=installation.executionPool.capacity,'launch-binding');
 need(core.canonicalJSON(record.lease.request)===core.canonicalJSON(record.request)
  &&record.lease.identity===core.contentIdentity({pool:core.contentIdentity(installation.executionPool),request:record.request,slot:record.lease.slot})
  &&installation.executionPool.targets.some(t=>t.repository===record.request.repository&&t.repositoryId===record.request.repositoryId),'lease-identity');
 const directory=join(root,'jobs',record.lease.identity),application=join(directory,'application');await privatePath(directory,{directory:true});
 return maintenanceLock(directory,async assertLease=>{
  const prepared=await readPrivate(join(directory,'prepared.json')),
   configuration=await readPrivate(join(directory,'configuration.json'),{limit:3145728}),admission=await readPrivate(join(directory,'admission.json'));
  need(prepared.lease===record.lease.identity&&prepared.identity===record.prepared
   &&configuration.lease===record.lease.identity&&configuration.identity===record.configuration
   &&admission.lease===record.lease.identity&&admission.identity===record.admission,'private-bindings');
  for(const value of [prepared,configuration,admission]){const {identity,...body}=value;
   need(identity===core.contentIdentity(body),'private-integrity');}
  const jit=nativeJitConfiguration(configuration.encoded,record);
  await privatePath(application,{directory:true});await verify(application,{signal});
  for(const [name,bytes]of Object.entries(jit.files)){
   const file=join(application,name);await privatePath(file);
   need((await fs.readFile(file)).equals(bytes),'configuration-bytes');
  }
  need(sha(await fs.readFile(join(application,'bin/Runner.Worker')))===prepared.workerSha256,'worker');
  await admissionCheck({id:admission.instance,registration:application},admission.environment);
  const environment=nativeRunnerEnvironment(admission.environment),path=join(directory,'process.json');
  const service=await (serviceState??(()=>nativeRunnerService({root,selector,slot:record.lease.slot,lease:record.lease.identity}).state()))();
  need(service.pid===process.pid&&service.invocation===invocation&&['active','activating'].includes(service.status)
   &&typeof service.controlGroup==='string'&&service.controlGroup.startsWith('/user.slice/user-'+userInfo().uid+'.slice/'),'actual-service');
  need(await readPrivate(path,{missing:true})===null,'original-launch-retained');
  need(!signal?.aborted,'cancelled');assertLease();
  const stat=await fs.readFile('/proc/self/stat','utf8'),entryStart=stat.slice(stat.lastIndexOf(')')+2).split(' ')[19];
  need(/^[0-9]+$/.test(entryStart),'process-start');
  const initial={version:1,lease:record.lease.identity,invocation,controlGroup:service.controlGroup,entryPid:process.pid,entryStart,phase:'launch-intent',configuration:record.configuration,admission:record.admission};
  await writePrivate(path,initial,{exclusive:true});
  const log=await fs.open(join(directory,'listener.log'),'wx',0o600);let child,timer,ended,aborted=false;
  const stop=()=>{aborted=true;if(child?.pid){try{process.kill(-child.pid,'SIGTERM');}catch{}
   timer??=setTimeout(()=>{try{process.kill(-child.pid,'SIGKILL');}catch{}},30000);}};
  try{
   signal?.addEventListener('abort',stop,{once:true});
   child=spawn(join(application,'bin/Runner.Listener'),['run'],{cwd:application,env:environment,detached:true,stdio:['ignore',log.fd,log.fd]});
   let spawned=false;
   ended=new Promise(resolve=>{
    child.once('spawn',()=>{spawned=true;if(signal?.aborted)stop();});
    child.once('error',()=>resolve({code:null,signal:null,spawned:false}));
    child.once('close',(code,termination)=>resolve({code,signal:termination,spawned}));
   });
   await new Promise(resolve=>{child.once('spawn',resolve);child.once('error',resolve);});
   if(child.pid)await writePrivate(path,{...initial,phase:'running',listenerPid:child.pid});
   const outcome=await ended;assertLease();
   await log.sync();await writePrivate(path,{...initial,phase:outcome.spawned?'terminated':'unconfirmed',
    ...(child.pid?{listenerPid:child.pid}:{}),exitCode:outcome.code,signal:outcome.signal,aborted});
   return {status:outcome.spawned?'terminated':'unconfirmed',lease:record.lease.identity};
  }catch(error){
   if(child){stop();await ended;}throw error;
  }finally{
   signal?.removeEventListener('abort',stop);clearTimeout(timer);await log.close();
  }
 });
}

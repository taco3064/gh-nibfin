import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {createServer,connect} from 'node:net';
import {pathToFileURL} from 'node:url';
import {privatePath,readPrivate,ensurePrivateDirectory,sha} from './local-state.mjs';
import {maintenanceLock} from './upgrade.mjs';
import {verifyNativeRuntime,nativeServiceUnit,nativeServiceName} from './native-control-service.mjs';
import {nativePoolStore} from './native-pool-store.mjs';
import {nativePoolCoordinator} from './native-pool-coordinator.mjs';
import {nativeStageService} from './native-stage-service.mjs';
import {executeNativeDelivery} from './native-delivery.mjs';
import {readNativeMember} from './native-member.mjs';
import {readNativeOwnerPolicy} from './native-owner-policy.mjs';
import {verifyCurrentCore} from './current-core.mjs';

const need=(ok,code)=>{if(!ok)throw Error('native-control:'+code);};
async function removeStaleSocket(path){
 let before;try{before=await fs.lstat(path);}catch(e){if(e.code==='ENOENT')return;throw e;}
 need(before.isSocket()&&before.uid===userInfo().uid&&(before.mode&0o777)===0o600,'socket-conflict');
 const refused=await new Promise(resolve=>{
  const s=connect(path);s.setTimeout(1000,()=>{s.destroy();resolve(false);});
  s.once('connect',()=>{s.destroy();resolve(false);});s.once('error',e=>{s.destroy();resolve(e.code==='ECONNREFUSED');});
 });
 need(refused,'socket-active');const current=await fs.lstat(path);
 need(current.ino===before.ino&&current.dev===before.dev,'socket-changed');await fs.unlink(path);
}
/** One kernel lease for the maintained host process. The version-2 pool consumes
 * authenticated queued Jobs and resumes its retained Runner lifecycle. No Issue
 * is synthesized and no old receipt becomes a native Acceptance. Full delivery
 * stays blocked until stage composition and original-work reconciliation are
 * installed; infrastructure liveness cannot clear that separate checkpoint.
 */
export async function runNativeControl(root,{signal,ready=async()=>{}}={}){
 need(process.platform==='linux'&&process.versions.node.split('.')[0]==='22'&&userInfo().uid!==0,'platform');
 await privatePath(root,{directory:true});
 const leaseRoot=join(root,'control-lease');await ensurePrivateDirectory(leaseRoot);
 return maintenanceLock(leaseRoot,async assertLease=>{
  const receipt=await readPrivate(join(root,'control-service.json')),record=await readPrivate(join(root,'native-installation.json'));
  need(receipt.version===1&&receipt.account===userInfo().uid&&/^[a-f0-9-]{36}$/.test(receipt.generation),'receipt');
  await privatePath(join(root,nativeServiceName));
  need(receipt.unit===sha(nativeServiceUnit(root))&&receipt.unit===sha(await fs.readFile(join(root,nativeServiceName))),'unit');
  await verifyNativeRuntime(root,record);assertLease();
  let poolStore,installedCore,coordinator,stages;
  if(record.executionPool?.version===2){
   installedCore=await import(pathToFileURL(join(root,'runtime/node_modules/@nibfin/core/src/index.mjs')).href);
   poolStore=nativePoolStore(installedCore,{root,configuration:record.executionPool});await poolStore.status();
  }
  const live={version:1,generation:receipt.generation,pid:process.pid,installation:sha(JSON.stringify(record)),
   service:'running',delivery:'blocked',reason:'native-delivery-composition-pending'};
  const path=join(root,'control.sock');await removeStaleSocket(path);
  const clients=new Set();let bound,serverError;
  const server=createServer({allowHalfOpen:true},socket=>{
   clients.add(socket);socket.once('close',()=>clients.delete(socket));socket.on('error',()=>socket.destroy());
   socket.setTimeout(60000,()=>socket.destroy());let bytes='';
   socket.on('data',b=>{bytes+=b;if(Buffer.byteLength(bytes)>1024)socket.destroy();});
   socket.once('end',async()=>{try{
    assertLease();const request=JSON.parse(bytes);
    need(request&&request.version===1,'request');
    if(['stage','stage-status'].includes(request.operation)){
     need(stages&&Object.keys(request).sort().join(' ')==='attempt operation repository request runId version','stage-request');
     const{operation,...selector}=request;
     const value=operation==='stage'?await stages.submit(selector):await stages.status(selector);
     assertLease();socket.end(JSON.stringify(value)+'\n');return;
    }
    if(request.operation==='enqueue'){
     need(Object.keys(request).sort().join(' ')==='jobId operation repository runId version'&&coordinator,'enqueue-request');
     const retained=await coordinator.enqueue({repository:request.repository,runId:request.runId,jobId:request.jobId});
     assertLease();socket.end(JSON.stringify(retained)+'\n');return;
    }
    need(Object.keys(request).sort().join(' ')==='operation version'&&request.operation==='status','request');
    const pool=coordinator?await coordinator.status():poolStore?await poolStore.status():undefined;assertLease();
    socket.end(JSON.stringify({...live,...(pool?{pool}:{})})+'\n');
   }catch{socket.destroy();}});
  });
  server.maxConnections=16;
  try{
   await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(path,resolve);});
   server.removeAllListeners('error');const failed=new Promise(resolve=>server.once('error',e=>{serverError=e;resolve();}));
   await fs.chmod(path,0o600);bound=await fs.lstat(path);assertLease();
   if(installedCore){
    coordinator=await nativePoolCoordinator(installedCore,{root,record,signal,onProgress:value=>
     process.stdout.write(installedCore.actionLogLine(value,'runner-pool'))});
    live.runnerPool='running';live.reason='native-stage-composition-pending';
    const configured=await readPrivate(join(root,'member-runtime.json'),{missing:true});
    const policy=await readPrivate(join(root,'owner-policy.json'),{missing:true});
    if(configured&&policy){
     await readNativeMember(installedCore,{root,memberId:coordinator.memberId});await readNativeOwnerPolicy(installedCore,{root});
     const verified=await verifyCurrentCore(pathToFileURL(join(root,'runtime/docs/handoffs/current/')));
     const context={...verified,root:join(root,'runtime'),identity:installedCore.distributionIdentity()};
     const provider=installedCore.createGithubProvider({attempts:1});
     stages=await nativeStageService(installedCore,{root,signal,source:(request,options)=>coordinator.stageSource(request,options),
      execute:(binding,options)=>executeNativeDelivery(installedCore,{root,binding,context,provider,...options})});
     live.stageEntry='configured';
    }
   }
   await ready(live);
   await Promise.race([failed,new Promise(resolve=>{if(signal?.aborted)resolve();else signal?.addEventListener('abort',resolve,{once:true});})]);
   if(serverError)throw serverError;
  }finally{
   await stages?.close();await coordinator?.close();
   for(const socket of clients)socket.destroy();
   await new Promise(resolve=>server.close(()=>resolve()));
   // Node removes its own listening socket. If one remains, never remove a
   // replacement created by a different process during shutdown.
   if(bound){let current;try{current=await fs.lstat(path);}catch(e){if(e.code!=='ENOENT')throw e;}
    if(current){need(current.ino===bound.ino&&current.dev===bound.dev,'shutdown-socket-changed');await fs.unlink(path);}}
  }
 });
}

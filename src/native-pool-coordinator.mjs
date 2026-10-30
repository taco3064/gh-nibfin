import fs from 'node:fs/promises';
import {watch} from 'node:fs';
import {join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {nativePoolStore} from './native-pool-store.mjs';
import {nativePoolIntake} from './native-pool-intake.mjs';
import {nativePoolRuntime} from './native-pool-runtime.mjs';
import {nativeWorkCustody} from './native-work-custody.mjs';
import {nativeGithubProvider} from './native-github-provider.mjs';
import {verifyCurrentCore,currentPin} from './current-core.mjs';
import {readPrivate,writePrivate,ensurePrivateDirectory} from './local-state.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-pool-coordinator:'+code);};
const id=v=>typeof v==='string'&&/^[1-9][0-9]*$/.test(v);

/** The maintained control service owns one Core pool lifecycle. Filesystem
 * notifications wake pending work; there is no scheduled GitHub workflow and
 * idle capacity is not polled. A short finite retry covers the real Job API's
 * completion lag after a locally observed process exit. It cannot clear a hold.
 */
export async function nativePoolCoordinator(core,{root,record,signal,onProgress=()=>{},
 provider,context,installationRoot,runtimeOptions={},observeResources}={}){
 const configuration=core.validateExecutionPool(record.executionPool);need(configuration.version===2,'configuration');
 need(record.core===currentPin.coreSha256&&typeof onProgress==='function','installation');
 const store=nativePoolStore(core,{root,configuration});await store.status();
 const targets=new Map(configuration.targets.map(t=>[t.repositoryId,t]));
 provider??=nativeGithubProvider(core,{attempts:1,authorize:r=>{
  const target=targets.get(r.target);return !!target&&r.operation==='runner-pool'
   &&(r.method==='POST'&&r.endpoint==='/repos/'+target.repository+'/actions/runners/generate-jitconfig'
    ||r.method==='DELETE'&&new RegExp('^/repos/'+target.repository.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')+'/actions/runners/[1-9][0-9]*$').test(r.endpoint));
 }});
 if(!context){
  const verified=await verifyCurrentCore(pathToFileURL(join(root,'runtime/docs/handoffs/current/')));
  context={...verified,root:join(root,'runtime'),identity:core.distributionIdentity()};
 }
 const account=await provider.request({endpoint:'/user',target:configuration.targets[0].repositoryId,
  operation:'native-pool-coordinator',correlation:'account',signal});
 need(account.outcome==='read'&&id(account.data?.id),'execution-account');const memberId=account.data.id;
 const blockedDirectory=join(root,'pool-blockages');await ensurePrivateDirectory(blockedDirectory);
 async function blocked(value){
  need(/^[A-Za-z0-9_-]{1,80}$/.test(value.request),'blockage-selector');
  const body={version:1,...value},retained={...body,identity:core.contentIdentity(body)};
  await writePrivate(join(blockedDirectory,value.request+'.json'),retained);
  onProgress({status:'unresolved',reason:value.diagnostic??'runner-pool-unavailable',
   ...(value.record?{issue:value.record.issue,stage:value.record.stage}:{}),ai_status:'not-yet-diagnosed'});
  return {status:'retained'};
 }
 const intake=await nativePoolIntake(core,{root,configuration,memberId,provider,onBlocked:blocked,installationRoot});
 const custody=await nativeWorkCustody(core,{root,configuration,store,...(observeResources?{resources:observeResources}:{})});
 const runtime=nativePoolRuntime(core,{root,configuration,context,provider,custody,...runtimeOptions});
 const capacity=core.createRunnerPoolCapacity({configuration,records:store.capacity,observeRelease:runtime.observeRelease});
 const host=core.createRunnerPoolHost({configuration,memberId,capacity,records:store.jobs,source:intake.source,provider,runtime});
 await ensurePrivateDirectory(join(root,'jobs'));
 let closed=false,pending=false,running=null,fatal=null;const watchers=new Map(),timers=new Map(),retries=new Map();
 function wake(){if(closed)return;pending=true;if(!running)running=drain().finally(()=>{running=null;if(pending&&!closed)wake();});}
 async function observeJobs(records){
  const live=new Set(records.map(r=>r.lease.identity));
  for(const [lease,w]of watchers)if(!live.has(lease)){w.close();watchers.delete(lease);}
  for(const record of records){const lease=record.lease.identity;if(!watchers.has(lease)){
    try{const w=watch(join(root,'jobs',lease),(_event,name)=>{if(name===null||name.toString()==='process.json')wake();});
     w.on('error',()=>{w.close();watchers.delete(lease);wake();});watchers.set(lease,w);
    }catch(e){if(e.code!=='ENOENT')throw e;}
   }
   // Only a terminated actual process can trigger a finite provider-lag retry.
   const process=await readPrivate(join(root,'jobs',lease,'process.json'),{missing:true});
   const count=retries.get(lease)??0;
   if(process?.phase==='terminated'&&count<60&&!timers.has(lease)){
    retries.set(lease,count+1);const timer=setTimeout(()=>{timers.delete(lease);wake();},5000);timer.unref();timers.set(lease,timer);
   }
  }
 }
 async function drain(){
  try{while(pending&&!closed){pending=false;const queued=await intake.list(),active=await store.jobs.pending();
    const keys=[...new Set([...active.map(r=>r.request.id),...queued.map(r=>r.request.id)])];
    for(const key of keys){if(closed||signal?.aborted)break;const record=await store.jobs.read(key);if(record?.phase==='released')continue;
     const result=await host.advance(key,{signal});
     onProgress({status:result.status,reason:result.reason??'runner-lifecycle',...(record?{issue:record.issue,stage:record.stage}:{})});
    }
    await observeJobs(await store.jobs.pending());
  }}catch{fatal='pool-observation-unavailable';onProgress({status:'unresolved',reason:fatal,ai_status:'not-yet-diagnosed'});}
 }
 const unsubscribe=intake.subscribe(value=>{if(value?.unavailable)fatal='intake-subscription-unavailable';wake();});
 const ledgerWatch=watch(join(root,'pool-state'),(_event,name)=>{if(name===null||name.toString()==='ledger.json')wake();});
 ledgerWatch.on('error',()=>{fatal='pool-subscription-unavailable';wake();});
 const jobsWatch=watch(join(root,'jobs'),()=>wake());jobsWatch.on('error',()=>{fatal='process-subscription-unavailable';wake();});
 const stop=()=>{closed=true;unsubscribe();ledgerWatch.close();jobsWatch.close();for(const w of watchers.values())w.close();for(const t of timers.values())clearTimeout(t);};
 signal?.addEventListener('abort',stop,{once:true});wake();
 return Object.freeze({memberId,
  async enqueue(hint){need(!closed&&!fatal,'unavailable');const retained=await intake.accept(hint,{signal});wake();return retained;},
  async stageSource(request,{signal:caller}={}){
   need(!closed&&!fatal,'unavailable');const retained=await intake.read(request.request);
   need(retained&&retained.request.repository===request.repository&&retained.request.runId===request.runId
    &&retained.request.attempt===request.attempt,'stage-request-binding');
   const observed=await intake.source.inspect(request.request,{signal:caller});
   need(observed.authorized===true&&observed.launchAuthorized===true,'stage-authority');
   const selected=await store.jobs.read(request.request);
   need(selected&&['launch-intent','active'].includes(selected.phase)&&selected.request.jobId===retained.request.jobId,'stage-lease');
   const actual=await provider.request({endpoint:'/repos/'+request.repository+'/actions/jobs/'+retained.request.jobId,
    target:retained.request.repositoryId,operation:'native-stage-source',correlation:request.request,signal:caller});
   need(actual.outcome==='read'&&actual.data.id===retained.request.jobId&&actual.data.run_id===request.runId
    &&(actual.data.run_attempt===undefined||String(actual.data.run_attempt)===request.attempt)&&actual.data.status==='in_progress'
    &&actual.data.runner_id===selected.runnerId,'actual-stage-job');
   return {...structuredClone(retained),jobRecord:structuredClone(selected),configuration:structuredClone(configuration),
    ownerType:configuration.targets.find(t=>t.repository===request.repository).ownerType};
  },
  async status(){return {...await store.status(),host:closed?'stopped':fatal?'unavailable':'running',...(fatal?{reason:fatal}:{})};},
  async settled(){await running;},
  async close(){stop();signal?.removeEventListener('abort',stop);await running;}
 });
}

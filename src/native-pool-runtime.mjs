import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {privatePath,readPrivate,writePrivate,ensurePrivateDirectory,sha} from './local-state.mjs';
import {prepareRunnerDistribution,runnerDistribution} from './runner-distribution.mjs';
import {prepareAdmission,removeAdmission} from './admission.mjs';
import {nativeJitConfiguration} from './native-jit.mjs';
import {nativeRunnerService} from './native-runner-service.mjs';
import {retireNativeApplication} from './native-resource-cleanup.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-pool-runtime:'+code);};
const hash=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const missing=async path=>{try{await fs.lstat(path);return false;}catch(e){if(e.code==='ENOENT')return true;throw e;}};
const factKeys=['id','run_id','run_attempt','runner_id','status','conclusion','started_at','completed_at'];

/** Maintained physical Runner capabilities, not an Issue planner or a model
 * authorization boundary. Native Core owns original-work custody; construction
 * requires that first-party owner and never accepts callbacks from public JSON.
 * GitHub credentials stay in the account's stored gh login. JIT credentials are
 * private files, never process arguments, environment variables or public logs.
 */
export function nativePoolRuntime(core,{root,configuration,context,provider,custody,
 acquire=prepareRunnerDistribution,admit=prepareAdmission,serviceFactory=nativeRunnerService}={}){
 const pool=core.validateExecutionPool(configuration);need(pool.version===2&&context?.files&&typeof provider?.request==='function'
  &&['read','retain','observe'].every(key=>typeof custody?.[key]==='function'),'composition');
 const seal=value=>({...value,identity:core.contentIdentity(value)});
 const validate=value=>{const {identity,...body}=value;need(hash(identity)&&core.contentIdentity(body)===identity,'private-integrity');return value;};
 const directory=record=>{
  need(hash(record?.lease?.identity)&&record.request?.id===record.lease.request?.id
   &&Number.isSafeInteger(record.lease.slot)&&record.lease.slot>=1&&record.lease.slot<=pool.capacity
   &&record.lease.identity===core.contentIdentity({pool:core.contentIdentity(pool),request:record.request,slot:record.lease.slot})
   &&pool.targets.some(t=>t.repository===record.request.repository&&t.repositoryId===record.request.repositoryId),'lease');
  return join(root,'jobs',record.lease.identity);
 };
 const service=record=>serviceFactory({root,selector:record.request.id,slot:record.lease.slot,lease:record.lease.identity});
 const retainedPath=record=>join(root,'pool-state','outcome-'+record.lease.identity+'.json');
 const retiredPath=record=>join(root,'pool-state','service-retired-'+record.lease.identity+'.json');
 async function prepared(record){
  const value=validate(await readPrivate(join(directory(record),'prepared.json')));
  need(value.lease===record.lease.identity&&value.identity===record.prepared,'preparation-binding');return value;
 }
 async function readJob(record,signal){
  const result=await provider.request({endpoint:'/repos/'+record.request.repository+'/actions/jobs/'+record.request.jobId,
   target:record.request.repositoryId,operation:'runner-pool-runtime',correlation:record.request.id,signal});
  need(result.outcome==='read','job-observation-unavailable');const job=result.data;
  need(job.id===record.request.jobId&&job.run_id===record.request.runId&&String(job.run_attempt)===record.request.attempt
   &&job.runner_id===record.runnerId&&job.status==='completed','job-unconfirmed');return job;
 }
 async function groupEmpty(group){
  need(typeof group==='string'&&group.startsWith('/user.slice/user-'+userInfo().uid+'.slice/')
   &&group.split('/').slice(1).every(part=>/^[A-Za-z0-9_.@:-]+$/.test(part)),'control-group');
  try{return /(?:^|\n)populated 0(?:\n|$)/.test(await fs.readFile('/sys/fs/cgroup'+group+'/cgroup.events','utf8'));}
  catch(error){if(error.code==='ENOENT')return true;throw error;}
 }
 async function entryEnded(processRecord){
  need(Number.isSafeInteger(processRecord.entryPid)&&processRecord.entryPid>0&&/^[0-9]+$/.test(processRecord.entryStart??''),'entry-process');
  try{
   const stat=await fs.readFile('/proc/'+processRecord.entryPid+'/stat','utf8');
   return stat.slice(stat.lastIndexOf(')')+2).split(' ')[19]!==processRecord.entryStart;
  }catch(error){if(error.code==='ENOENT')return true;throw error;}
 }
 async function observe(record,{signal}={}){
  await prepared(record);const current=await service(record).state();
  if(['active','activating','deactivating'].includes(current.status))return {status:'running',lease:record.lease.identity};
  const processRecord=await readPrivate(join(directory(record),'process.json'));
  need(['inactive','failed'].includes(current.status)&&current.pid===0&&processRecord.phase==='terminated'
   &&processRecord.lease===record.lease.identity&&processRecord.invocation===current.invocation
   &&processRecord.configuration===record.configuration&&processRecord.admission===record.admission
   &&await entryEnded(processRecord)&&await groupEmpty(processRecord.controlGroup),'process-unconfirmed');
  const job=await readJob(record,signal);
  return {status:'terminated',lease:record.lease.identity,job,processReceipt:core.contentIdentity(processRecord),process:processRecord};
 }
 async function outcome(record){
  const value=validate(await readPrivate(retainedPath(record)));
  need(value.phase==='retained'&&value.lease===record.lease.identity&&hash(value.custody)
   &&core.canonicalJSON(value.request)===core.canonicalJSON(record.request),'outcome-unconfirmed');return value;
 }
 async function settled(record,retained,{signal}={}){
  const observation=await custody.observe({lease:record.lease.identity,request:structuredClone(record.request),signal});
  if(observation?.lease!==record.lease.identity||observation.retention!==retained.custody||observation.workRetained!==true
   ||observation.processesTerminated!==true||observation.resourcesRemoved!==true)return false;
  if(!await service(record).absent())return false;
  const remote=await provider.request({endpoint:'/repos/'+record.request.repository+'/actions/runners/'+record.runnerId,
   target:record.request.repositoryId,operation:'runner-pool-runtime',correlation:record.request.id,signal});
  return remote.outcome==='failed'&&remote.diagnostic?.status===404;
 }
 const runtime={
  async prepare(record,{signal}={}){
   await privatePath(root,{directory:true});await ensurePrivateDirectory(join(root,'jobs'));
   const path=directory(record);await fs.mkdir(path,{mode:0o700});const application=join(path,'application');await fs.mkdir(application,{mode:0o700});
   await acquire(application,{archive:join(root,'runner-'+record.lease.slot,runnerDistribution.archive),signal});
   const value=seal({version:1,lease:record.lease.identity,request:record.request,distribution:runnerDistribution,
    workerSha256:sha(await fs.readFile(join(application,'bin/Runner.Worker')))});
   await writePrivate(join(path,'prepared.json'),value,{exclusive:true});return {identity:value.identity};
  },
  async storeConfiguration(record,data){
   await prepared(record);need(typeof data?.runner?.id==='string'&&data.runner.name===record.runnerName&&data.runner.ephemeral===true,'configuration-binding');
   const jit=nativeJitConfiguration(data.encoded_jit_config,{...record,runnerId:data.runner.id}),path=directory(record),application=join(path,'application');
   for(const [name,bytes]of Object.entries(jit.files)){
    const file=await fs.open(join(application,name),'wx',0o600);try{await file.writeFile(bytes);await file.sync();}finally{await file.close();}
   }
   const parent=await fs.open(application,'r');try{await parent.sync();}finally{await parent.close();}
   const value=seal({version:1,lease:record.lease.identity,runnerId:data.runner.id,encoded:jit.encoded});
   await writePrivate(join(path,'configuration.json'),value,{exclusive:true});return {identity:value.identity};
  },
  async installAdmission(record,{signal}={}){
   await prepared(record);const path=directory(record),target=pool.targets.find(t=>t.repository===record.request.repository),
    instance='pool-'+record.lease.identity.slice(0,40),[owner,repository]=target.repository.split('/');
   const installed=await admit(core,context,{provider,signal,instance:{id:instance,registration:join(path,'application')},
    state:{memberId:record.memberId,runnerId:record.runnerId,runnerName:record.runnerName,registration:{level:'repository',id:target.repositoryId},
     target:{id:target.repositoryId,owner,repository,ownerId:target.ownerId,ownerType:target.ownerType}}});
   const value=seal({version:1,lease:record.lease.identity,instance,environment:installed.environment,record:installed.record});
   await writePrivate(join(path,'admission.json'),value,{exclusive:true});return {status:'installed',identity:value.identity};
  },
  async start(record){await prepared(record);return service(record).start();},
  observe,
  async retainOutcome(record,observation,{signal}={}){
   need(observation.lease===record.lease.identity&&observation.status==='terminated'&&hash(observation.processReceipt),'outcome-binding');
   const job=Object.fromEntries(factKeys.filter(key=>Object.hasOwn(observation.job,key)).map(key=>[key,observation.job[key]]));
   const body={version:1,lease:record.lease.identity,request:record.request,job,process:observation.processReceipt};
   let previous=await readPrivate(retainedPath(record),{missing:true});
   if(previous){validate(previous);need(['intake','invoke-intent','retained'].includes(previous.phase)
    &&previous.lease===body.lease&&core.canonicalJSON(previous.request)===core.canonicalJSON(body.request)
    &&core.canonicalJSON(previous.job)===core.canonicalJSON(body.job)&&previous.process===body.process,'outcome-changed');
    if(previous.phase==='retained')return {status:'retained',identity:previous.identity};}
   else{previous=seal({...body,phase:'intake'});await writePrivate(retainedPath(record),previous,{exclusive:true});}
   let retained=await custody.read({lease:body.lease,request:structuredClone(record.request),signal});
   if(retained?.status==='absent'){
    need(previous.phase==='intake','custody-reconciliation-required');
    await writePrivate(retainedPath(record),seal({...body,phase:'invoke-intent'}));
    retained=await custody.retain({lease:body.lease,request:structuredClone(record.request),job:structuredClone(job),
     process:observation.processReceipt,listenerLog:join(directory(record),'listener.log'),signal});
   }
   need(retained?.status==='retained'&&retained.lease===body.lease&&hash(retained.identity),'custody-unconfirmed');
   const value=seal({...body,phase:'retained',custody:retained.identity});await writePrivate(retainedPath(record),value);
   return {status:'retained',identity:value.identity};
  },
  async cleanup(record,{signal}={}){
   const retained=await outcome(record);need(retained.identity===record.outcome,'outcome-binding');
   let retired=await readPrivate(retiredPath(record),{missing:true});
   if(retired){validate(retired);need(['intent','retired'].includes(retired.phase)&&retired.lease===record.lease.identity
    &&retired.outcome===record.outcome&&retired.process===retained.process,'retirement-binding');}
   else{
    const current=await observe(record,{signal});need(current.status==='terminated'&&current.processReceipt===retained.process,'termination-changed');
    retired=seal({version:1,lease:record.lease.identity,outcome:record.outcome,process:current.processReceipt,phase:'intent'});
    await writePrivate(retiredPath(record),retired,{exclusive:true});
   }
   if(retired.phase==='intent'){
    const state=await custody.observe({lease:record.lease.identity,request:structuredClone(record.request),signal});
    need(state?.lease===record.lease.identity&&state.retention===retained.custody&&state.workRetained===true
     &&state.processesTerminated===true&&state.resourcesRemoved===true,'custody-unconfirmed');
    const stopped=await service(record).retire();
    need(stopped.status==='retired'&&stopped.lease===record.lease.identity,'service-retirement-unconfirmed');
    await removeAdmission({id:'pool-'+record.lease.identity.slice(0,40)});
    const {identity,...body}=retired;await writePrivate(retiredPath(record),seal({...body,phase:'retired'}));
   }
   return retireNativeApplication(core,{root,lease:record.lease.identity,outcome:record.outcome,
    confirm:()=>settled(record,retained,{signal})});
  },
  async observeRelease(lease,{signal}={}){
   const path=join(root,'pool-state','cleanup-'+lease.identity+'.json'),completed=validate(await readPrivate(path,{limit:16777216}));
   need(completed.phase==='completed'&&completed.lease===lease.identity,'cleanup-unconfirmed');
   const retained=validate(await readPrivate(join(root,'pool-state','outcome-'+lease.identity+'.json')));
   need(retained.phase==='retained'&&retained.identity===completed.outcome&&core.canonicalJSON(retained.request)===core.canonicalJSON(lease.request),'outcome-binding');
   const record={request:lease.request,lease,runnerId:retained.job.runner_id};directory(record);
   const confirmed=await settled(record,retained,{signal});
   return {lease:lease.identity,processTerminated:confirmed,registrationRemoved:confirmed,
    cleanupConfirmed:confirmed&&await missing(directory(record))&&await missing(join(root,'jobs','retired-'+lease.identity))};
  }
 };
 // Preserve bounded, actionable infrastructure codes without raw provider,
 // credential, process or filesystem exception text in recovery notifications.
 return Object.freeze(Object.fromEntries(Object.entries(runtime).map(([name,method])=>[name,async(...args)=>{
  try{return await method(...args);}catch(error){
   const match=/^(native-cleanup|native-runner-service|native-jit):([a-z-]+)$/.exec(error?.message??'');
   if(match)throw Error('native-pool-runtime:'+match[1].replace(/^native-/,'')+'-'+match[2]);
   if(['ENOENT','EACCES','EPERM','EEXIST','EIO','ENOSPC'].includes(error?.code))
    throw Error('native-pool-runtime:'+({ENOENT:'private-state-missing',EACCES:'private-state-denied',EPERM:'private-state-denied',EEXIST:'private-state-conflict',EIO:'io-unconfirmed',ENOSPC:'storage-full'}[error.code]));
   throw error;
  }
 }])));
}

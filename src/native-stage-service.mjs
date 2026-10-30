import {join} from 'node:path';
import {readPrivate,writePrivate,ensurePrivateDirectory} from './local-state.mjs';
import {maintenanceLock} from './upgrade.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-stage:'+code);};
const id=v=>typeof v==='string'&&/^[1-9][0-9]*$/.test(v);
const selector=v=>typeof v==='string'&&/^[A-Za-z0-9_-]{1,80}$/.test(v);

// Protected first-party IPC owner. A caller supplies only a selector for an
// already authenticated project Job. Retain intent before executing; crashes
// retain uncertainty, never launch the role a second time on a status request.
export async function nativeStageService(core,{root,source,execute,signal}={}){
 need(typeof source==='function'&&typeof execute==='function','composition');
 const directory=join(root,'stage-records');await ensurePrivateDirectory(directory);
 const running=new Map();let closed=false;
 const validate=request=>{need(request&&Object.keys(request).sort().join(' ')==='attempt repository request runId version'
  &&request.version===1&&selector(request.request)&&id(request.runId)&&id(request.attempt)
  &&typeof request.repository==='string'&&/^[a-z0-9-]+\/[a-z0-9_.-]+$/.test(request.repository),'request');};
 const read=async request=>{const path=join(directory,request.request+'.json'),record=await readPrivate(path,{missing:true,limit:16777216});
  if(record){const {identity,...body}=record;need(core.contentIdentity(body)===identity
   &&core.canonicalJSON(record.request)===core.canonicalJSON(request),'record-binding');}return record;};
 const save=async record=>{const value={...record,identity:core.contentIdentity(record)};
  await writePrivate(join(directory,record.request.request+'.json'),value);return value;};
 const reconcile=async record=>{
  if(record.phase!=='running'||running.has(record.request.request))return record;
  const {identity,...retained}=record;
  return save({...retained,phase:'unresolved',conclusion:'failure',progressCount:record.progressCount+1,
   progress:[...record.progress,core.actionLogLine({issue:record.issue,stage:record.stage,status:'unresolved',
    reason:'interrupted-stage-reconcile-original-execution',verified:'false'},'native-stage')].slice(-50)});
 };
 const project=record=>({version:1,request:record.request.request,status:record.phase,
  issue:record.issue,stage:record.stage,progress:record.progress,offset:record.progressCount-record.progress.length,conclusion:record.conclusion});
 async function run(record){
  let value=record;
  const progress=async report=>{
   need(!closed&&!signal?.aborted,'cancelled');
   const line=core.actionLogLine({...report,issue:record.issue,stage:record.stage},'native-stage');
   value={...value,progress:[...value.progress,line].slice(-50),progressCount:value.progressCount+1};await save(value);
  };
  try{
   const binding=await source(record.request,{signal});
   need(binding.issue===record.issue&&binding.stage===record.stage,'source-drift');
   const result=await execute(binding,{signal,progress});
   const conclusion=result?.disposition==='completed'?'success':'failure';
   await progress({status:result?.disposition??'unresolved',reason:result?.reason??'native-stage-unavailable',verified:conclusion==='success'?'true':'false'});
   value={...value,phase:conclusion==='success'?'completed':'unresolved',conclusion,result};
  }catch(error){
   const reason=/^native-[a-z-]+:[a-z-]+$/.test(error?.message??'')?error.message:
    error?.code==='EADDRINUSE'?'native-stage:host-socket-in-use':'native-stage-reconciliation-required';
   value={...value,phase:'unresolved',conclusion:'failure',failure:{name:String(error?.name??'Error'),code:typeof error?.code==='string'?error.code:null,
    message:String(error?.message??'Unavailable').slice(0,4096),stack:String(error?.stack??'').slice(0,16384)},progressCount:value.progressCount+1,progress:[...value.progress,
    core.actionLogLine({issue:value.issue,stage:value.stage,status:'unresolved',reason,verified:'false'},'native-stage')].slice(-50)};
  }
  await save(value);
 }
 return Object.freeze({
  async submit(request){validate(request);need(!closed&&!signal?.aborted,'cancelled');
   return maintenanceLock(directory,async()=>{
    const existing=await read(request);if(existing)return project(await reconcile(existing));
    const binding=await source(request,{signal});need(id(binding.issue)&&core.coreStages.includes(binding.stage),'source');
    const record={version:1,request:structuredClone(request),issue:binding.issue,stage:binding.stage,phase:'running',conclusion:null,progress:[],progressCount:0};
    await save(record);const pending=run(record).catch(()=>{}).finally(()=>running.delete(request.request));running.set(request.request,pending);
    return project(record);
   });
  },
  async status(request){validate(request);const value=await read(request);need(value,'missing');return project(await reconcile(value));},
  async close(){closed=true;await Promise.allSettled([...running.values()]);}
 });
}

import fs from 'node:fs/promises';
import {join} from 'node:path';
import {privatePath,readPrivate,writePrivate} from './local-state.mjs';
import {maintenanceLock} from './upgrade.mjs';

const need=(ok,code)=>{if(!ok)throw Error('native-pool-store:'+code);};
const selector=id=>need(typeof id==='string'&&/^[A-Za-z0-9_-]{1,80}$/.test(id),'selector');
const hash=v=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
/** Private first-party storage, never a workflow-input database or an authority
 * adapter. A missing/corrupt activated ledger cannot become an empty pool.
 * The kernel lock spans read/validate/write/fsync. In-process operations queue;
 * another process must reconcile a busy lease rather than remove its lock.
 */
export function nativePoolStore(core,{root,configuration}={}){
 const pool=core.validateExecutionPool(configuration);need(pool.version===2,'repository-local-pool-required');
 const directory=join(root,'pool-state'),path=join(directory,'ledger.json'),poolIdentity=core.contentIdentity(pool);
 let tail=Promise.resolve();
 function validate(value){
  need(value&&Object.keys(value).sort().join(' ')==='capacity identity jobs pool version'&&value.version===1
   &&value.pool===poolIdentity&&value.capacity?.version===1&&value.capacity.pool===poolIdentity
   &&value.capacity.leases&&value.capacity.completed&&Object.keys(value.capacity.leases).length<=pool.capacity&&value.jobs
   &&Object.getPrototypeOf(value.jobs)===Object.prototype,'ledger');
  const {identity,...body}=value;need(hash(identity)&&identity===core.contentIdentity(body),'integrity');
  for(const [id,record]of Object.entries(value.jobs)){
   selector(id);const {identity:jobIdentity,...job}=record;
   need(record.request?.id===id&&hash(jobIdentity)&&jobIdentity===core.contentIdentity(job),'job-integrity');
  }
  return value;
 }
 const seal=body=>({...body,identity:core.contentIdentity(body)});
 async function read(){await privatePath(directory,{directory:true});return validate(await readPrivate(path,{limit:33554432}));}
 function transaction(work){
  const next=tail.then(()=>maintenanceLock(directory,async assertLease=>{
   const before=await read(),body=structuredClone(before);delete body.identity;
   const result=await work(body);assertLease();
   if(core.canonicalJSON(body)!==core.canonicalJSON((({identity,...value})=>value)(before))){
    const after=seal(body);validate(after);await writePrivate(path,after);assertLease();
   }
   return result;
  }));tail=next.catch(()=>{});return next;
 }
 return Object.freeze({
  async initialize(){
   await privatePath(root,{directory:true});let created=false;
   try{await fs.mkdir(directory,{mode:0o700});created=true;}catch(e){if(e.code!=='EEXIST')throw e;}
   await privatePath(directory,{directory:true});
   return maintenanceLock(directory,async()=>{
    const previous=await readPrivate(path,{missing:true,limit:33554432});
    if(previous){validate(previous);return {status:'retained'};}
    need(created,'missing-retained-ledger');
    await writePrivate(path,seal({version:1,pool:poolIdentity,capacity:core.initialRunnerPoolCapacity(pool),jobs:{}}),{exclusive:true});
    return {status:'initialized'};
   });
  },
  capacity:Object.freeze({update:fn=>{
   need(typeof fn==='function','transaction');return transaction(async state=>{
    const result=await fn(structuredClone(state.capacity));
    if(result!==null&&result!==undefined)state.capacity=structuredClone(result);
    return structuredClone(state.capacity);
   });
  }}),
  jobs:Object.freeze({
   async pending(){return Object.values((await read()).jobs).filter(job=>job.phase!=='released').map(job=>structuredClone(job));},
   async read(id){selector(id);return structuredClone((await read()).jobs[id]??null);},
   async write({id,expected,value}){
    selector(id);need(expected===null||hash(expected),'expected');
    return transaction(state=>{
     if((state.jobs[id]?.identity??null)!==expected)return {status:'conflict'};
     need(value?.request?.id===id,'job-selector');state.jobs[id]=structuredClone(value);
     return {status:'retained'};
    });
   }
  }),
  async status(){
   const state=await read();return {version:1,pool:poolIdentity,capacity:pool.capacity,
    occupied:Object.keys(state.capacity.leases).length,
    pending:Object.values(state.jobs).filter(job=>job.phase!=='released').length};
  }
 });
}

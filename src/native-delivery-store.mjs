import fs from 'node:fs/promises';import{join}from'node:path';
import{privatePath,ensurePrivateDirectory,readPrivate,writePrivate}from'./local-state.mjs';import{maintenanceLock}from'./upgrade.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-delivery-store:'+code);};
export const nativeCycleDirectory=(root,target)=>{
 need(['repositoryId','databaseId'].every(k=>typeof target[k]==='string'&&/^[1-9][0-9]*$/.test(target[k])),'target');
 return join(root,'delivery',target.repositoryId,target.databaseId);
};
export async function createNativeCycle(core,{root,target,value}={}){
 const directory=nativeCycleDirectory(root,target);await ensurePrivateDirectory(join(root,'delivery',target.repositoryId));
 await fs.mkdir(directory,{mode:0o700});
 const body={...structuredClone(value),version:1,target:structuredClone(target),revision:0,records:{},operations:{},operationBlobs:{},completions:{},completionBlobs:{},publications:{}};
 await writePrivate(join(directory,'state.json'),{...body,identity:core.contentIdentity(body)},{exclusive:true});
 return openNativeCycle(core,{root,target});
}
export async function openNativeCycle(core,{root,target}={}){
 const directory=nativeCycleDirectory(root,target);await privatePath(directory,{directory:true});
 const path=join(directory,'state.json');
 const read=async()=>{const value=await readPrivate(path,{limit:134217728}),{identity,...body}=value;
  need(value.version===1&&value.identity===core.contentIdentity(body)&&core.canonicalJSON(value.target)===core.canonicalJSON(target),'integrity');return value;};
 await read();const records=join(directory,'records');await ensurePrivateDirectory(records);let tail=Promise.resolve();
 const update=change=>{const next=tail.then(()=>maintenanceLock(records,async()=>{
  const before=await read(),value=await change(structuredClone(before));if(value===null)return before;
  const {identity,...body}=value,after={...body,revision:before.revision+1};await writePrivate(path,{...after,identity:core.contentIdentity(after)});return read();
 }));tail=next.catch(()=>{});return next;};
 async function retainRecord(kind,payload){
  const body={version:1,kind,payload:structuredClone(payload)},identity=core.contentIdentity(body),file=join(records,identity+'.json');
  const existing=await readPrivate(file,{missing:true,limit:134217728});
  if(existing)need(existing.identity===identity&&core.canonicalJSON(existing)===core.canonicalJSON({...body,identity}),'record-conflict');
  else await writePrivate(file,{...body,identity},{exclusive:true});return identity;
 }
 async function record(identity,kind){
  need(typeof identity==='string'&&/^[a-f0-9]{64}$/.test(identity),'record-reference');
  const value=await readPrivate(join(records,identity+'.json'),{limit:134217728}),{identity:actual,...body}=value;
  need(actual===identity&&core.contentIdentity(body)===identity&&body.kind===kind,'record-integrity');return body.payload;
 }
 const operationRecords={
  async retain({expected,value}){
   const body={version:1,kind:'native-operation-record',expected,value},identity=core.contentIdentity(body),ref={kind:body.kind,identity};
   // The intent/attempt locator is updated before any official write. A lost
   // result reopens this exact source, never guesses newness from an empty file.
   const retained=await retainRecord('operation-record',body);
   await update(state=>{const key=core.contentIdentity(value.intent),entry=state.operations[key]??{intent:structuredClone(value.intent),refs:{}};
    need(core.canonicalJSON(entry.intent)===core.canonicalJSON(value.intent),'operation-conflict');
    const phase={ref,source:{runId:expected.runId,attempt:expected.attempt}};
    need(!entry.refs[value.phase]||core.canonicalJSON(entry.refs[value.phase])===core.canonicalJSON(phase),'operation-phase-conflict');
    entry.refs[value.phase]=phase;state.operations[key]=entry;state.operationBlobs[identity]=retained;return state;});
   return {outcome:'retained',ref};
  },
  async retrieve({ref,expected}){
   need(ref.kind==='native-operation-record','operation-reference');
   const state=await read(),locator=state.operationBlobs[ref.identity];need(locator,'operation-locator');
   const body=await record(locator,'operation-record');
   need(core.contentIdentity(body)===ref.identity&&body.kind==='native-operation-record'
    &&core.canonicalJSON(body.expected)===core.canonicalJSON(expected),'operation-binding');
   return {outcome:'retrieved',expected:structuredClone(body.expected),value:structuredClone(body.value)};
  }
 };
 const completionRecords={
  async retain({expected,value}){
   need(typeof value.key==='string'&&/^[a-f0-9]{64}$/.test(value.key)&&typeof value.phase==='string','completion-key');
   const body={version:1,kind:'native-completion-record',expected,value},identity=core.contentIdentity(body);
   const ref={kind:body.kind,identity},retained=await retainRecord('completion-record',body);
   const entry={ref,source:{runId:expected.runId,attempt:expected.attempt}};
   await update(state=>{const phases=state.completions[value.key]??{};
    need(!phases[value.phase]||core.canonicalJSON(phases[value.phase])===core.canonicalJSON(entry),'completion-phase-conflict');
    phases[value.phase]=entry;state.completions[value.key]=phases;state.completionBlobs[identity]=retained;return state;});
   return {outcome:'retained',ref};
  },
  async retrieve({ref,expected}){
   need(ref.kind==='native-completion-record','completion-reference');const state=await read();
   const locator=state.completionBlobs[ref.identity];need(locator,'completion-locator');const body=await record(locator,'completion-record');
   need(core.contentIdentity(body)===ref.identity&&body.kind==='native-completion-record'
    &&core.canonicalJSON(body.expected)===core.canonicalJSON(expected),'completion-binding');
   return {outcome:'retrieved',expected:structuredClone(body.expected),value:structuredClone(body.value)};
  }
 };
 return Object.freeze({directory,read,update,record,
  async retain(value){const identity=await retainRecord('retained',value);await update(state=>{state.records[identity]=value.kind??'retained';return state;});return 'native-record:'+identity;},
  async get(reference){need(typeof reference==='string'&&/^native-record:[a-f0-9]{64}$/.test(reference),'reference');return record(reference.slice(14),'retained');},
  operationRecords,completionRecords,
  async locateCompletion({identity,phase}){const state=await read(),value=state.completions[identity]?.[phase];
   return value?{outcome:'found',...structuredClone(value)}:{outcome:'new',identity};},
  async locateOperation({identity}){const state=await read(),value=state.operations[identity];
   return value?{outcome:'found',refs:structuredClone(value.refs)}:{outcome:'new',identity};}
 });
}

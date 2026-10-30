import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs/promises';import{join}from'node:path';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeStageService}from'../../src/native-stage-service.mjs';
const request={version:1,repository:'example/project',request:'retained-request',runId:'100',attempt:'1'};
const source=async r=>{assert.deepEqual(r,request);return{issue:'55',stage:'shape'};};
async function settled(service){for(let n=0;n<100;n++){const value=await service.status(request);if(value.status!=='running')return value;await new Promise(r=>setTimeout(r,10));}throw Error('stage did not settle');}

test('the private stage entry retains intent once, publishes sanitized progress and reports success only for a completed Core stage',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'stage-service');await fs.mkdir(root,{mode:0o700});let launches=0;
 const service=await nativeStageService(core,{root,source,execute:async(binding,{progress})=>{launches++;assert.equal(binding.issue,'55');
  for(let n=0;n<55;n++)await progress({status:'running',reason:'checkpoint-'+n+' ::notice:: GH_TOKEN=private'});
  return{disposition:'completed',reason:'owner-result'};}});
 try{
  assert.equal((await service.submit(request)).status,'running');assert.equal((await service.submit(request)).status,'running');
  const value=await settled(service);assert.equal(value.status,'completed');assert.equal(value.conclusion,'success');assert.equal(launches,1);
  assert.equal(value.offset,6);assert.equal(value.progress.length,50);assert.equal(JSON.stringify(value).includes('private'),false);
  assert.equal(JSON.stringify(value).includes('::notice::'),false);
  assert.equal((await service.submit(request)).status,'completed');assert.equal(launches,1);
  await assert.rejects(service.submit({...request,runId:'101'}),/record-binding/);
 }finally{await service.close();}
}));

test('a restarted service preserves an interrupted original execution and never relaunches it',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'interrupted-stage');await fs.mkdir(root,{mode:0o700});let done;
 const first=await nativeStageService(core,{root,source,execute:()=>new Promise(resolve=>{done=resolve;})});
 await first.submit(request);for(let n=0;n<100&&!done;n++)await new Promise(r=>setTimeout(r,5));
 // Simulated daemon restart over the same private records. Execution is a double;
 // this test does not establish native role termination or cleanup.
 const restarted=await nativeStageService(core,{root,source,execute:()=>assert.fail('must reconcile original')});
 assert.equal((await restarted.status(request)).status,'unresolved');assert.equal((await restarted.submit(request)).conclusion,'failure');
 done({disposition:'blocked',reason:'original-result'});await first.close();await restarted.close();
}));

test('unknown, rejected and waiting Core outcomes cannot produce a green stage; invalid selectors never launch',async()=>withCurrentCore(async(core,context)=>{
 for(const disposition of ['unresolved','rejected','waiting']){
  const root=join(context.root,disposition);await fs.mkdir(root,{mode:0o700});let launched=0;
  const service=await nativeStageService(core,{root,source,execute:async()=>{launched++;return{disposition,reason:'held'};}});
  await assert.rejects(service.submit({...request,command:'arbitrary'}),/request/);assert.equal(launched,0);
  await service.submit(request);const result=await settled(service);assert.equal(result.status,'unresolved');assert.equal(result.conclusion,'failure');await service.close();
 }
}));

import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs/promises';import{join}from'node:path';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeStageService}from'../../src/native-stage-service.mjs';
import{resumeNativeSession}from'../../src/native-session-resume.mjs';import{createNativeCycle}from'../../src/native-delivery-store.mjs';
const request={version:1,repository:'example/project',request:'retained-request',runId:'100',attempt:'1'};
const source=async r=>{assert.deepEqual(r,request);return{issue:'55',stage:'shape'};};
async function settled(service){for(let n=0;n<100;n++){const value=await service.status(request);if(value.status!=='running')return value;await new Promise(r=>setTimeout(r,10));}throw Error('stage did not settle');}

test('Owner-reconciled native continuation restores the original role conversation and rejects cross-member or cross-role reuse',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'session-cycle');await fs.mkdir(root,{mode:0o700});
 const store=await createNativeCycle(core,{root,target:{repository:'example/project',repositoryId:'42',databaseId:'55',issueId:'I_55',number:'55'},value:{cycle:'original-cycle',scopeHash:core.bodyDigest('Original Scope')}});
 const original=join(context.root,'original-session'),directory=join(context.root,'restored-session'),id='11111111-1111-4111-8111-111111111111';
 await fs.mkdir(original,{mode:0o700});await fs.mkdir(directory,{mode:0o700});const bytes=JSON.stringify({type:'session_meta',payload:{id}})+'\n';await fs.writeFile(join(original,id+'.jsonl'),bytes,{mode:0o600});
 const metadata={id,memberId:'7',role:'shape',execution:'original-execution',adapter:'codex'},snapshot=core.captureRescueSession({directory:original,memberId:'7',role:'shape',session:id,execution:metadata.execution});
 const reference=await store.retain({version:1,kind:'native-role-session',metadata,snapshot});
 await store.update(s=>{s.sessionResumes={shape:{status:'UNACCEPTED',role:'shape',memberId:'7',scopeHash:s.scopeHash,cycle:s.cycle,caseId:'original-recovery',reference}};return s;});
 await assert.rejects(resumeNativeSession(core,{store,role:'shape',memberId:'8',directory,request:'next'}),/original-case/);
 await store.update(s=>{s.sessionResumes.accept={...s.sessionResumes.shape,role:'accept'};return s;});
 await assert.rejects(resumeNativeSession(core,{store,role:'accept',memberId:'7',directory,request:'next'}),/original-session/);
 assert.deepEqual(await fs.readdir(directory),[]);
 const resumed=await resumeNativeSession(core,{store,role:'shape',memberId:'7',directory,request:'next'});
 assert.equal(resumed.id,id);assert.equal(resumed.memberId,'7');assert.equal(await fs.readFile(join(directory,id+'.jsonl'),'utf8'),bytes);
 assert.ok(Object.values((await store.read()).records).includes('native-role-continuation'));
}));

test('construction errors retain private diagnostics while exposing only the classified stage reason',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'failed-stage');await fs.mkdir(root,{mode:0o700});
 const service=await nativeStageService(core,{root,source,execute:async()=>{throw Object.assign(Error('GH_TOKEN=private ::error:: private path'),{code:'EADDRINUSE'});}});
 try{await service.submit(request);const result=await settled(service);assert.equal(result.conclusion,'failure');assert.ok(result.progress[0].includes('host-socket-in-use'));assert.equal(JSON.stringify(result).includes('private'),false);
  const retained=JSON.parse(await fs.readFile(join(root,'stage-records',request.request+'.json')));assert.equal(retained.failure.code,'EADDRINUSE');assert.ok(retained.failure.message.includes('private'));
 }finally{await service.close();}
}));

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

test('only an authenticated Rescue job can report pending verification as neutral; it never fabricates successful delivery',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'pending-rescue');await fs.mkdir(root,{mode:0o700});
 const service=await nativeStageService(core,{root,source:async r=>({...await source(r),jobName:'rescue'}),execute:async()=>({disposition:'waiting',reason:'PENDING_VERIFICATION'})});
 try{await service.submit(request);const result=await settled(service);assert.equal(result.status,'waiting');assert.equal(result.conclusion,'neutral');
  assert.equal((await service.submit(request)).status,'waiting');assert.match(result.progress.at(-1),/"verified":"false"/);
 }finally{await service.close();}
}));

test('unknown, rejected and waiting Core outcomes cannot produce a green stage; invalid selectors never launch',async()=>withCurrentCore(async(core,context)=>{
 for(const disposition of ['unresolved','rejected','waiting']){
  const root=join(context.root,disposition);await fs.mkdir(root,{mode:0o700});let launched=0;
  const service=await nativeStageService(core,{root,source,execute:async()=>{launched++;return{disposition,reason:'held'};}});
  await assert.rejects(service.submit({...request,command:'arbitrary'}),/request/);assert.equal(launched,0);
  await service.submit(request);const result=await settled(service);assert.equal(result.status,'unresolved');assert.equal(result.conclusion,'failure');await service.close();
 }
}));

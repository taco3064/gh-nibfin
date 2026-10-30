import test from'node:test';import assert from'node:assert/strict';import{join,dirname}from'node:path';
import{withCurrentCore,currentPin}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';
import{initializeFileHostStore,openFileHostStore}from'../../src/host-store.mjs';import{nativeStageWork}from'../../src/native-stage-work.mjs';
import{nativeControlHost}from'../../src/native-host.mjs';
import{nativeActivationStatus,activateNativeHost}from'../../src/native-activation.mjs';import{nativePoolStore}from'../../src/native-pool-store.mjs';
import fs from'node:fs/promises';import{nativeRoleHost}from'../../src/native-role-host.mjs';
import{writePrivate,ensurePrivateDirectory,sha}from'../../src/local-state.mjs';import{managedWorkflowPlan}from'../../src/workflow-inputs.mjs';

async function host(core,f,context){return initializeFileHostStore(core,{directory:join(f.root,'state'),installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins},repositories:f.configuration.targets.map(({repository,repositoryId})=>({repository,repositoryId}))});}
test('two physical slots preserve concurrent work, Owner pause and unresolved effects',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),store=await host(core,f,context);try{
 let s=await store.read();await store.updateWork({expected:s.identity,work:{...s.work,paused:false}});
 const one=nativeStageWork(store,{request:'one',cycle:'first',stage:'deliver'}),two=nativeStageWork(store,{request:'two',cycle:'second',stage:'merge'});
 await Promise.all([one.enter(),two.enter()]);assert.equal((await store.read()).work.pending.length,2);
 s=await store.read();await store.updateWork({expected:s.identity,work:{...s.work,paused:true,hold:{reason:'owner'}}});
 await one.finish({completed:true,terminated:true,reference:'one-receipt'});s=await store.read();assert.equal(s.work.active,true);assert.equal(s.work.pending[0].request,'two');assert.equal(s.work.paused,true);
 await two.finish({completed:false,terminated:false,unknownWrite:true,reference:'two-unresolved'});s=await store.read();assert.equal(s.work.active,true);assert.equal(s.work.unknownWrite,true);assert.equal(s.work.reconciled,false);assert.equal(s.work.pending[0].status,'UNRESOLVED');assert.deepEqual(s.work.hold,{reason:'owner'});
 await assert.rejects(nativeStageWork(store,{request:'third',cycle:'third',stage:'shape'}).enter(),/restriction/);
 }finally{await store.close();}
}));
test('activation requires installed exact project source and preserves later Owner pause',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),store=await host(core,f,context);await store.close();
 await writePrivate(join(f.root,'native-installation.json'),{executionPool:f.configuration});await nativePoolStore(core,{root:f.root,configuration:f.configuration}).initialize();
 assert.equal((await nativeActivationStatus(core,{root:f.root})).status,'pending');
 assert.equal((await activateNativeHost(core,{root:f.root,configuration:f.configuration,provider:f.provider})).reason,'native-project-source-pending');
 const files=new Map(managedWorkflowPlan(core,{executionPool:f.configuration,repository:'example/project'}).files);
 files.set('.nibfin/runtime.json',JSON.stringify({allowedActorIds:['7'],package:currentPin,executionPool:f.configuration}));
 for(const role of['shape','deliver','accept','rescue'])files.set('.nibfin/skills/'+role+'/SKILL.md','# '+role+'\n');
 const directory=join(dirname(f.root),'installations/example--project');await ensurePrivateDirectory(directory);
 const local={version:1,repository:'example/project',repositoryId:'100',installation:{packageVersion:currentPin.packageVersion,pins:context.identity.pins},executionPool:f.configuration,files:Object.fromEntries([...files].map(([p,b])=>[p,sha(b)]))};await writePrivate(join(directory,'installation.json'),local);
 f.intercept(async r=>{const prefix='/repos/example/project/contents/';if(r.endpoint.startsWith(prefix)){const path=r.endpoint.slice(prefix.length).split('?')[0];if(files.has(path))return{status:200,body:JSON.stringify({type:'file',path,encoding:'base64',content:Buffer.from(files.get(path)).toString('base64')})};}});
 files.set('.nibfin/skills/shape/SKILL.md','changed');await assert.rejects(activateNativeHost(core,{root:f.root,configuration:f.configuration,provider:f.provider}),/source-bytes/);files.set('.nibfin/skills/shape/SKILL.md','# shape\n');
 assert.equal((await activateNativeHost(core,{root:f.root,configuration:f.configuration,provider:f.provider})).status,'ready');
 const current=await openFileHostStore(core,{directory:join(f.root,'state')});try{const s=await current.read();await current.updateWork({expected:s.identity,work:{...s.work,paused:true}});}finally{await current.close();}
 assert.equal((await activateNativeHost(core,{root:f.root,configuration:f.configuration,provider:f.provider})).status,'paused');
}));

test('aborted public source acquisition retires only its own unlaunched role allocation',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),identity='a'.repeat(64),parent=join(f.root,'jobs',identity);await ensurePrivateDirectory(parent);
 const controller=new AbortController();controller.abort();
 await assert.rejects(nativeRoleHost(core,{root:f.root,binding:{configuration:f.configuration,request:{id:'aborted-source'},jobRecord:{lease:{identity}}},base:'b'.repeat(40),repository:'example/project',memberId:'7',source:{},retain:async()=>{},checkpoint:async()=>{},signal:controller.signal}),/source-acquisition/);
 await assert.rejects(fs.lstat(join(parent,'roles')),{code:'ENOENT'});assert.ok((await fs.stat(join(f.root,'owner-signing-key.pem'))).isFile());
}));

test('Member preparation reloads an authenticated idle daemon that started without stage bindings',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context);let staged=0,installed=0;
 const service={check:async()=>({status:'running',delivery:'blocked',diagnostics:[]}),stageUnit:async()=>{staged++;return{status:'linked'};},install:async()=>{installed++;return{status:'running',stageEntry:'configured',diagnostics:[]};}};
 const host=nativeControlHost({root:f.root,service,verify:async()=>{},prepare:async path=>fs.writeFile(join(path,'fixture'),'application',{mode:0o600})});
 const result=await host.prepare({core:{...core,createGithubProvider:()=>f.provider},context,executionPool:f.configuration,memberId:'7'});
 assert.equal(result.status,'prepared');assert.equal(staged,1);assert.equal(installed,1);
}));

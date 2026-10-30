import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs/promises';import{join}from'node:path';
import{withCurrentCore}from'../../src/current-core.mjs';
import{createNativeCycle,openNativeCycle}from'../../src/native-delivery-store.mjs';
const target={repository:'example/project',repositoryId:'100',databaseId:'123',issueId:'I_123',number:'55'};

test('native records survive restart with independent content bindings, immutable phases and original source Runs',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'native-record-test');await fs.mkdir(root,{mode:0o700});
 let store=await createNativeCycle(core,{root,target,value:{cycle:'original-cycle'}});
 const reference=await store.retain({kind:'original-work',status:'UNACCEPTED',bytes:'original conversation'});
 const expected={runId:'1000',attempt:'1'},intent={kind:'recording-test',binding:{issueId:target.issueId}};
 const operation={intent,phase:'intent',value:'write not submitted'};
 const op=await store.operationRecords.retain({expected,value:operation});
 const key=core.contentIdentity({cycle:'original-cycle'}),completion={key,phase:'close-start',value:{state:'open'}};
 const ce=await store.completionRecords.retain({expected,value:completion});
 store=await openNativeCycle(core,{root,target});
 assert.equal((await store.get(reference)).status,'UNACCEPTED');
 assert.deepEqual((await store.locateOperation({identity:core.contentIdentity(intent)})).refs.intent,{ref:op.ref,source:expected});
 assert.deepEqual(await store.locateCompletion({identity:key,phase:'close-start'}),{outcome:'found',ref:ce.ref,source:expected});
 assert.deepEqual((await store.operationRecords.retrieve({ref:op.ref,expected})).value,operation);
 assert.deepEqual((await store.completionRecords.retrieve({ref:ce.ref,expected})).value,completion);
 await assert.rejects(store.operationRecords.retrieve({ref:op.ref,expected:{...expected,runId:'1001'}}),/operation-binding/);
 await assert.rejects(store.completionRecords.retrieve({ref:ce.ref,expected:{...expected,attempt:'2'}}),/completion-binding/);
 await assert.rejects(store.operationRecords.retain({expected,value:{...operation,value:'replacement'}}),/operation-phase-conflict/);
 await assert.rejects(store.completionRecords.retain({expected,value:{...completion,value:{state:'closed'}}}),/completion-phase-conflict/);
 assert.deepEqual((await store.completionRecords.retrieve({ref:ce.ref,expected})).value,completion);
 assert.deepEqual(await store.locateOperation({identity:core.contentIdentity('never-submitted')}),{outcome:'new',identity:core.contentIdentity('never-submitted')});
 assert.deepEqual(await store.locateCompletion({identity:key,phase:'closure'}),{outcome:'new',identity:key});
}));

test('missing or modified durable state is held instead of treated as a new cycle',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'corrupt-native-records');await fs.mkdir(root,{mode:0o700});
 const store=await createNativeCycle(core,{root,target,value:{cycle:'original'}}),path=join(store.directory,'state.json');
 const value=JSON.parse(await fs.readFile(path,'utf8'));value.cycle='replacement';await fs.writeFile(path,JSON.stringify(value));
 await assert.rejects(openNativeCycle(core,{root,target}),/integrity/);
 await fs.unlink(path);await assert.rejects(openNativeCycle(core,{root,target}),/ENOENT/);
 await assert.rejects(createNativeCycle(core,{root,target,value:{cycle:'replacement'}}),/EEXIST/);
}));

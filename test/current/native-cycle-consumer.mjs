import test from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{userInfo}from'node:os';
import{withCurrentCore}from'../../src/current-core.mjs';import{prepareNativeOwnerPolicy}from'../../src/native-owner-policy.mjs';
import{prepareNativeCycle}from'../../src/native-cycle-policy.mjs';import{nativeExecutionInputs}from'../../src/native-execution-inputs.mjs';
import{ensurePrivateDirectory,writePrivate}from'../../src/local-state.mjs';
const repository='example/project',target={version:1,repository,repositoryId:'100',ownerId:'7',ownerType:'User'};
const configuration={version:2,id:'member-default',capacity:2,targets:[target]},member={id:'7',login:'member'};

import{nativeCycleFixture as fixture}from'./native-cycle-fixture.mjs';

test('ordinary Owner policy signs a live Issue cycle, reopens original state and refuses changed Scope or legacy shaped work',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context),store=await prepareNativeCycle(core,f.options),state=await store.read();
 assert.equal(state.scopeBody,f.issue.body);assert.equal(state.grant.keyId,state.ownerKey.id);assert.equal(state.policy.maxParallel,2);
 const input=await nativeExecutionInputs(core,{root:f.root,store,configuration,binding:{request:{id:'request-1'},jobRecord:{runnerId:'999'}},
  provider:f.provider,transport:f.transport,checkpoint:async()=>{}});
 assert.equal(core.verifyGrant(state.grant,input.grantContext('shape')).ok,true);
 assert.equal((await input.authorization.eligibility({operation:'shape'})).ok,true);
 assert.equal((await input.dependencies.evaluate(input.dependencyInput)).outcome,'ready');
 assert.equal((await input.selector.select(input.selectionInput('shape'))).outcome,'selected');
 const original=(await prepareNativeCycle(core,f.options));assert.equal((await original.read()).identity,state.identity);
 f.issue.body+='\nChanged Scope';await assert.rejects(prepareNativeCycle(core,f.options),/original-cycle-reconciliation-required/);
 f.issue.body=state.scopeBody;f.issue.assignees=[];assert.equal((await input.authorization.eligibility({operation:'shape'})).ok,false);
 f.issue.assignees=[member];f.deny();assert.equal((await input.authorization.eligibility({operation:'shape'})).ok,false);
}));

test('pre-existing shaped work and manually closed Issues cannot become a new native execution',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);f.issue.labels.push({name:'shaped'});
 await assert.rejects(prepareNativeCycle(core,f.options),/original-work-handover-required/);
 f.issue.labels=[{name:'scope'}];f.issue.state='closed';await assert.rejects(prepareNativeCycle(core,f.options),/original-work-handover-required/);
}));

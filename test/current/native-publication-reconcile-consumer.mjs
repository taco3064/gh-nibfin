import test from'node:test';import assert from'node:assert/strict';import{join}from'node:path';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';import{prepareNativeCycle}from'../../src/native-cycle-policy.mjs';
import{initializeFileHostStore}from'../../src/host-store.mjs';import{ensurePrivateDirectory,writePrivate}from'../../src/local-state.mjs';import{reconcileNativePublication}from'../../src/native-publication-reconcile.mjs';
test('PR reconciliation clears only proven pre-submission uncertainty, never an attempted or unexpected write',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),store=await prepareNativeCycle(core,f.options),s=await store.read(),claim={id:'failed-pr',stage:'pr',runId:'4000'};
 const branch='nibfin/issue-55',sha='c'.repeat(40);await store.update(v=>{v.shapeBody=f.issue.body;v.dispatch={id:claim.id,stage:'pr'};v.attempts.pr={phase:'unresolved'};
  v.publications.fixture=[{event:{kind:'intent',pin:{source:{runId:claim.runId},target:{headRef:branch}}}},{event:{kind:'commit',value:{commit:sha}}}];return v;});
 const host=await initializeFileHostStore(core,{directory:join(f.root,'state'),installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins},repositories:[{repository:'example/project',repositoryId:'100'}]});
 try{
  const h=await host.read();await host.updateWork({expected:h.identity,work:{...h.work,paused:false,unknownWrite:true,pending:[{cycle:s.cycle,request:claim.id,status:'UNACCEPTED'}]}});
  await ensurePrivateDirectory(join(f.root,'pool-state'));await writePrivate(join(f.root,'pool-state/ledger.json'),{jobs:{[claim.id]:{phase:'released'}}});
  let unexpected=true;const reply=x=>({status:200,body:JSON.stringify(x)});f.intercept(r=>{
   if(r.endpoint.includes('/git/matching-refs/'))return reply([{ref:'refs/heads/'+branch,object:{sha}}]);
   if(r.endpoint.startsWith('/repos/example/project/pulls?'))return reply(unexpected?[{id:'500',head:{repo:{id:'100'},ref:branch}}]:[]);
  });
  await assert.rejects(reconcileNativePublication(core,{root:f.root,store,claim,provider:f.provider}),/unexpected-pr/);assert.equal((await host.read()).work.unknownWrite,true);
  unexpected=false;await store.update(v=>{v.operations.fixture={intent:{kind:'pr',source:{runId:claim.runId}}};return v;});
  assert.equal((await reconcileNativePublication(core,{root:f.root,store,claim,provider:f.provider})).outcome,'unknown');assert.equal((await host.read()).work.unknownWrite,true);
  await store.update(v=>{delete v.operations.fixture;return v;});
  assert.equal((await reconcileNativePublication(core,{root:f.root,store,claim,provider:f.provider})).outcome,'reconciled');
  const after=await host.read();assert.equal(after.work.unknownWrite,false);assert.equal(after.work.pending.length,1);assert.equal((await store.read()).attempts.pr.phase,'unresolved');
 }finally{await host.close();}
}));

import test from'node:test';import assert from'node:assert/strict';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';import{prepareNativeCycle}from'../../src/native-cycle-policy.mjs';import{refreshNativeDeliveryBase}from'../../src/native-delivery-base.mjs';
test('first Writer observes current main without changing Scope authority; original Writer continuations retain their base',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),store=await prepareNativeCycle(core,f.options),before=await store.read(),request='native-first-writer';
 await store.update(v=>{v.receipts.publication={fixture:'independently-validated-publication'};v.attempts.deliver={request,phase:'running'};return v;});
 f.setBase('c'.repeat(40));f.intercept(r=>{if(r.endpoint.includes('/contents/.github/workflows/ci.yml'))return{status:200,body:JSON.stringify({type:'file',encoding:'base64',content:Buffer.from(f.workflow).toString('base64')})};});
 const result=await refreshNativeDeliveryBase(core,{store,provider:f.provider,request});assert.equal(result.outcome,'refreshed');let s=await store.read();assert.equal(s.base,'c'.repeat(40));assert.equal(s.scopeHash,before.scopeHash);assert.deepEqual(s.grant,before.grant);assert.deepEqual(s.policy,before.policy);
 await store.update(v=>{v.sessionResumes={deliver:{status:'UNACCEPTED'}};return v;});f.setBase('d'.repeat(40));assert.equal((await refreshNativeDeliveryBase(core,{store,provider:f.provider,request})).outcome,'retained-original-base');assert.equal((await store.read()).base,'c'.repeat(40));
 await store.update(v=>{delete v.sessionResumes.deliver;return v;});f.intercept(r=>{if(r.endpoint.includes('/contents/.github/workflows/ci.yml'))return{status:200,body:JSON.stringify({type:'file',encoding:'base64',content:Buffer.from('Changed CI policy').toString('base64')})};});
 await assert.rejects(refreshNativeDeliveryBase(core,{store,provider:f.provider,request}),/ci-policy-changed/);assert.equal((await store.read()).base,'c'.repeat(40));
}));

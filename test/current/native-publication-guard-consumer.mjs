import test from'node:test';import assert from'node:assert/strict';import{withCurrentCore}from'../../src/current-core.mjs';
import{nativePublicationGuard}from'../../src/native-publication-guard.mjs';
test('publication keeps exact host/requirements and semantic dependency checks without recursive execution reads',async()=>withCurrentCore(async core=>{
 let state={cycle:'cycle',scopeHash:'scope',shapeBody:'reviewed plan',base:'base',receipts:{accept:'actual'}},restriction=false,graph=[];const calls=[];
 const guard=await nativePublicationGuard(core,{store:{read:async()=>structuredClone(state)},checkpoint:async options=>{calls.push(options);if(restriction)throw Error('owner pause');},dependencies:{evaluate:async()=>({outcome:'ready',target:{repositoryId:'100'},edges:graph,completions:[]})}});
 await guard.dependencies.evaluate({});await guard.checkpoint();assert.deepEqual(calls,[{forceRemote:true,exactBody:true}]);
 restriction=true;await assert.rejects(guard.checkpoint(),/owner pause/);restriction=false;
 state.shapeBody='unreviewed change';await assert.rejects(guard.checkpoint(),/protected-inputs-changed/);state.shapeBody='reviewed plan';
 graph=[{from:'100',to:'101'}];await assert.rejects(guard.dependencies.evaluate({}),/dependencies-changed/);
}));

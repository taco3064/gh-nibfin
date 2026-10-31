import test from'node:test';import assert from'node:assert/strict';
import{withCurrentCore}from'../../src/current-core.mjs';import{prepareDeferredNativeRole}from'../../src/native-deferred-role.mjs';

test('Owner plan revisions resume the exact original role through Core without inheriting acceptance or replaying a seed',async()=>withCurrentCore(async core=>{
 for(const kind of ['native-owner-shape-revision','native-owner-code-repair']){
 const hash='a'.repeat(64),scopeHash=core.bodyDigest('Unchanged Scope'),sessionId='00000000-0000-4000-8000-000000000001';
 const target={repository:'example/project',repositoryId:'100',issueId:'I_123',number:'55',cycle:'cycle',authority:hash,
  requirements:scopeHash,scopeHash,shapeHash:hash,candidate:hash,execution:'old-writer',role:'deliver',adapter:'codex',access:'read-only',
  status:'UNACCEPTED',memberId:'7',sessionMemberId:'7',runnerMemberId:'7',runnerId:'9',session:sessionId,source:{runId:'900',attempt:'1'}};
 const records=new Map([['proof',{kind,scopeHash,cycle:'cycle',memberId:'7',core:hash,shapeHash:core.bodyDigest('Fresh independently reviewed Shape')}],
  ['session',{kind:'native-role-session',metadata:{id:sessionId,execution:'old-writer',role:'deliver',memberId:'7'},snapshot:{terminated:true,digest:hash}}],
  ['work',{kind:'native-unaccepted-workspace',status:'UNACCEPTED',execution:{id:'old-writer'},entries:[],bytes:0}]]);
 let state={core:hash,target:{repository:'example/project',repositoryId:'100',issueId:'I_123',number:'55'},cycle:'cycle',scopeHash,shapeBody:'Fresh independently reviewed Shape',base:'b'.repeat(40),authority:{identity:hash},
  deferredRoles:{deliver:{phase:'prepared',memberId:'7',scopeHash,proof:'proof',sessionReference:'session',workspaceReference:'work',caseId:hash,target,plan:{instruction:'Verify the preserved work against the fresh plan'}}}};
 const retained=[],store={read:async()=>structuredClone(state),get:async ref=>structuredClone(records.get(ref)),
  retain:async v=>{retained.push(v);return'record:'+retained.length;},update:async change=>{state=await change(structuredClone(state));return structuredClone(state);}};
 const binding={stage:'deliver',issue:'55',memberId:'7',request:{id:'normal-next-dispatch',repository:'example/project',repositoryId:'100'}};
 const prepared=await prepareDeferredNativeRole(core,{store,binding,memberId:'7'});
 assert.equal(prepared.next.session,sessionId);assert.notEqual(prepared.next.execution,'old-writer');assert.equal(prepared.next.request,binding.request.id);
 assert.equal(prepared.status,'UNACCEPTED');assert.equal(prepared.next.freshBudget,true);assert.equal(state.sessionResumes.deliver.workspaceReference,'work');
 assert.deepEqual(prepared.requiredGates,['candidate','hooks','accept','exact-head-ci','shaper-review','protected-merge','closure']);
 assert.equal(state.receipts,undefined);assert.equal(retained.length,1);
 await prepareDeferredNativeRole(core,{store,binding,memberId:'7'});assert.equal(retained.length,1,'armed seed is never replayed');
 state.deferredRoles.deliver.phase='prepared';state.deferredRoles.deliver.scopeHash='c'.repeat(64);
 await assert.rejects(prepareDeferredNativeRole(core,{store,binding,memberId:'7'}),/scope-member/);assert.equal(retained.length,1);
 }
}));

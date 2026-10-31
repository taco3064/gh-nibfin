import test from'node:test';import assert from'node:assert/strict';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeRoleSummaries}from'../../src/native-role-summary.mjs';
test('pinned Core observes actual retained prose without adopting verdicts, authority or stage inputs',async()=>withCurrentCore(async core=>{
 for(const change of [null,e=>{e.binding.policy='f'.repeat(64);},e=>{e.inputs.publication='f'.repeat(64);},e=>{e.result.outcome='accepted';}]){
  const records=[],projection=nativeRoleSummaries(core,{retain:async v=>{records.push(structuredClone(v));return'private:terminal';}});
  const instructions=core.selectInstructions({role:'deliver',skill:'deliver'}),scopeBody='Scope',shapeBody='Shape',h='a'.repeat(64);
  const binding={authority:h,grant:h,policy:h,core:h,runtime:h,instructions:instructions.identity,repositoryId:'42',issueId:'I_actual',scopeHash:core.bodyDigest(scopeBody),shapeHash:core.bodyDigest(shapeBody)};
  const original=await core.createNativeRoleBindings({ownership:'personal',retain:async()=> 'private:request'}).bind({role:'deliver',skill:'deliver',instructions,task:{binding,scopeBody,shapeBody,publication:{verified:'publication'}}});
  const projected=projection.bind(original),e=structuredClone(original.current().expected);e.result.summary='Three documentation files verified with real guarded checks.';if(change)change(e);
  await projection.retain({kind:'role-collection',exitCode:0,execution:original.input.execution,binding:original.input.binding,evidence:e});
  assert.equal(projected.current().expected.result.summary,change?core.nativeRoleSuccessSummary:e.result.summary);
  assert.deepEqual(original.current().expected.result.summary,core.nativeRoleSuccessSummary);
  assert.deepEqual(projected.input,original.input);assert.deepEqual(projected.current().expected.inputs,original.current().expected.inputs);
  assert.equal(records[0].evidence.result.summary,e.result.summary,'actual prose remains in original custody');
 }
}));

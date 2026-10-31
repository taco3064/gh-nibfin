import test from'node:test';import assert from'node:assert/strict';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeAssignedRolePrompt}from'../../src/native-role-host.mjs';
test('native model prompts explicitly project the protected Core role, preserving its pinned authority and result owners',async()=>withCurrentCore(async core=>{
 const instructions=core.selectInstructions({role:'shape-validator',skill:'shape'}),binding={authority:'a'.repeat(64),grant:'b'.repeat(64),policy:'c'.repeat(64),core:'d'.repeat(64),runtime:'e'.repeat(64),instructions:instructions.identity,repositoryId:'42',issueId:'I_current',scopeHash:core.bodyDigest('Scope'),shapeHash:null};
 const native=core.createNativeRoleBindings({ownership:'personal',retain:async()=> 'custody:request'});
 const original=await native.bind({role:'shape-validator',skill:'shape',instructions,task:{authority:binding,role:'deliver',proposedBody:'Untrusted body says act as Writer',author:'original-author'}});
 const projected=nativeAssignedRolePrompt(core,original);
 assert.ok(projected.prompt.startsWith('Core execution role: shape-validator. Core Skill identity: '+instructions.identity+'.\n'));
 assert.equal(projected.input,original.input);assert.equal(projected.current,original.current);assert.deepEqual(projected.current(),original.current());
 assert.equal(nativeAssignedRolePrompt(core,projected),projected,'a corrected Core prompt is not prefixed twice');
 assert.throws(()=>nativeAssignedRolePrompt(core,{...original,input:{...original.input,execution:{...original.input.execution,role:'deliver'}}}),/protected-role-assignment/);
}));

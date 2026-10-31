import test from'node:test';import assert from'node:assert/strict';
import{awaitNativeRoleExit}from'../../src/native-role-wait.mjs';

test('native execution wait preserves unknown, expiration and cancellation instead of inventing termination',async()=>{
 let clock=1,reads=0,stops=0;
 const original={termination:async()=>{reads++;return{termination:{status:'running'}};},stop:async()=>{stops++;}};
 const value={input:{execution:{id:'same-execution'}},budget:{cutoff:3},lifecycle:original};
 const waiting=await awaitNativeRoleExit(value,{checkpoint:async()=>{},now:()=>clock,sleep:async()=>clock++});
 assert.equal(waiting.termination.status,'running');assert.equal(reads,3);assert.equal(stops,0);
 assert.equal(value.lifecycle,original,'cleanup keeps the original immediate observer');
 const signal=AbortSignal.abort();
 await assert.rejects(awaitNativeRoleExit({...value,budget:{cutoff:9}},{signal,checkpoint:async()=>{},now:()=>clock}),/cancelled/);
 await assert.rejects(awaitNativeRoleExit({...value,budget:{cutoff:9}},{checkpoint:async()=>{throw Error('owner pause');},now:()=>clock}),/owner pause/);
 let unknownReads=0,checkpoints=0;
 const unknown=await awaitNativeRoleExit({...value,budget:{cutoff:9},lifecycle:{termination:async()=>{unknownReads++;return{termination:{status:'unknown'}};}}},{checkpoint:async()=>{checkpoints++;},now:()=>clock,sleep:async()=>clock++});
 assert.equal(unknown.termination.status,'unknown');assert.equal(clock,9);assert.equal(unknownReads,7);assert.equal(checkpoints,6);assert.equal(stops,0);
 await assert.rejects(awaitNativeRoleExit({...value,budget:{cutoff:12},lifecycle:{termination:async()=>({termination:{status:'unknown'}})}},{signal,checkpoint:async()=>{},now:()=>clock}),/cancelled/);
 await assert.rejects(awaitNativeRoleExit({...value,budget:{cutoff:12},lifecycle:{termination:async()=>({termination:{status:'unknown'}})}},{checkpoint:async()=>{throw Error('owner pause');},now:()=>clock}),/owner pause/);
});

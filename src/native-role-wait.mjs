import{setTimeout as delay}from'node:timers/promises';

// Native Jobs await the original execution, just as the upstream runtime awaits
// agent.execute before Candidate capture. Keep the Core lifecycle as the sole
// observer; never translate running/unknown into successful termination. The
// original branded lifecycle is kept intact for start checks and cancellation.
export async function awaitNativeRoleExit(value,{checkpoint,signal,now=Date.now,sleep=ms=>delay(ms,undefined,{signal})}={}){
 const original=value.lifecycle,cutoff=value.budget?.cutoff;
 if(!Number.isSafeInteger(cutoff)||typeof checkpoint!=='function')throw Error('native-role-wait:configuration');
  const id=value.input.execution.id;let observed=await original.termination(id);
  // The Linux backend reports unknown while its attached process promise is
  // pending. Only verified termination permits collection; an unknown read is
  // still bounded by this original execution's deadline and Owner checkpoint.
  while(['running','unknown'].includes(observed.termination.status)&&now()<cutoff){
   if(signal?.aborted)throw Error('native-role-wait:cancelled');
   await checkpoint();await sleep(Math.min(1000,Math.max(1,cutoff-now())));
   observed=await original.termination(id);
  }
  return observed;
}

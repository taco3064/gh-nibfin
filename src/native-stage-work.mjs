const need=(ok,code)=>{if(!ok)throw Error('native-stage-work:'+code);};
// One protected CAS store is shared by all physical slots. Completing one Job
// cannot clear another Job's activity, Owner pause, old work or unknown writes.
export function nativeStageWork(store,{request,cycle,stage}={}){
 let entered=false;
 async function change(transform){for(let attempt=0;attempt<20;attempt++){
  const before=await store.read(),work=await transform(structuredClone(before.work));
  try{return await store.updateWork({expected:before.identity,work});}
  catch(e){if(!e.message?.endsWith('work-compare-and-swap'))throw e;}
 }throw Error('native-stage-work:concurrent-state-unavailable');}
 return Object.freeze({
  async enter(){await change(work=>{
   need(work.restrictionsChecked&&work.paused===false&&work.hold===null&&work.unknownWrite===false,'restriction');
   need(!work.pending.some(p=>p.kind==='native-stage'&&(p.request===request||p.cycle===cycle)),'original-stage-pending');
   work.pending.push({kind:'native-stage',request,cycle,stage,status:'ACTIVE'});work.active=true;work.terminated=false;work.reconciled=false;return work;
  });entered=true;},
  async session({reference,metadata}){need(entered,'not-entered');await change(work=>{
   if(!work.conversations.some(c=>c.kind==='native-role-session'&&c.reference===reference))
    work.conversations.push({kind:'native-role-session',reference,execution:metadata.execution,role:metadata.role,session:metadata.id});
   return work;
  });},
  async finish({completed,terminated,unknownWrite=false,reference=null}){
   if(!entered)return;
   await change(work=>{
    const index=work.pending.findIndex(p=>p.kind==='native-stage'&&p.request===request);need(index>=0,'original-stage-missing');
    if(completed&&terminated)work.pending.splice(index,1);
    else work.pending[index]={...work.pending[index],status:terminated?'UNACCEPTED':'UNRESOLVED'};
    work.active=work.pending.some(p=>p.kind==='native-stage'&&['ACTIVE','UNRESOLVED'].includes(p.status))||work.pending.some(p=>p.kind!=='native-stage');
    work.terminated=!work.active&&terminated;work.reconciled=work.pending.length===0&&work.terminated;
    work.unknownWrite=work.unknownWrite||unknownWrite;
    if(reference){const record={kind:'native-cycle',cycle,stage,reference},prior=work.receipts.findIndex(r=>r.kind==='native-cycle'&&r.cycle===cycle);
     if(prior<0)work.receipts.push(record);else work.receipts[prior]=record;}
    return work;
   });entered=false;
  }
 });
}

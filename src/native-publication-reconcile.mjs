import{join}from'node:path';import{openFileHostStore}from'./host-store.mjs';import{readPrivate}from'./local-state.mjs';
const need=(v,c)=>{if(!v)throw Error('native-publication-reconcile:'+c);};
// Reconcile a pushed Candidate which failed BEFORE the PR operation existed.
// Never infer no effect from absence of a local receipt after a submission.
export async function reconcileNativePublication(core,{root,store,claim,provider}={}){
 if(claim.stage!=='pr')return{outcome:'not-applicable'};
 const s=await store.read();if(s.attempts.pr?.phase!=='unresolved'||s.dispatch?.id!==claim.id)return{outcome:'not-applicable'};
 const host=await openFileHostStore(core,{directory:join(root,'state')});try{
  const h=await host.read();if(!h.work.unknownWrite)return{outcome:'already-reconciled'};
  need(!h.work.paused&&!h.work.hold&&!h.work.active&&h.work.terminated,'work-active-or-held');
  need(h.work.pending.length===1&&h.work.pending[0].request===claim.id&&h.work.pending[0].status==='UNACCEPTED','unrelated-write');
  const ledger=await readPrivate(join(root,'pool-state/ledger.json'));
  need(ledger.jobs[claim.id]?.phase==='released','runner-active');
  if(Object.values(s.operations).some(o=>o.intent.kind==='pr'&&o.intent.source.runId===claim.runId))return{outcome:'unknown',reason:'original-pr-operation-reconciliation-required'};
  const histories=Object.values(s.publications).filter(a=>a[0]?.event.pin?.source.runId===claim.runId);need(histories.length===1,'publication-history');
  const a=histories[0],pin=a[0].event.pin,commit=a.find(r=>r.event.kind==='commit')?.event.value;need(commit,'publication-commit');
  const get=async endpoint=>{const r=await provider.request({endpoint,target:s.target.repositoryId,operation:'native-publication-reconcile',correlation:claim.id});need(r.outcome==='read','readback');return r.data;};
  const base='/repos/'+s.target.repository,issue=await get(base+'/issues/'+s.target.number);need(issue.body===s.shapeBody&&issue.state==='open'&&issue.assignees.some(m=>m.id===s.member.id),'scope-or-member-changed');
  const refs=await get(base+'/git/matching-refs/heads/'+pin.target.headRef),ref=refs.find(r=>r.ref==='refs/heads/'+pin.target.headRef);
  need(!ref||ref.object.sha===commit.commit,'branch-changed');
  const pulls=await provider.collection({endpoint:base+'/pulls?state=all&per_page=100',target:s.target.repositoryId,operation:'native-publication-reconcile',correlation:claim.id,allowPage:p=>p.split('?')[0]===base+'/pulls'});
  need(pulls.outcome==='complete'&&!pulls.data.some(p=>p.head.repo?.id===s.target.repositoryId&&p.head.ref===pin.target.headRef),'unexpected-pr');
  const reference=await store.retain({kind:'native-publication-reconciliation',source:claim,branch:ref?.object.sha??null,prAbsent:true,operationAbsent:true,
   rootCause:'Publication stopped before creating its PR operation. The exact Candidate branch is independently read back; no PR submission occurred.'});
  const fresh=await host.read();need(core.canonicalJSON(fresh.work)===core.canonicalJSON(h.work),'work-changed');
  await host.updateWork({expected:fresh.identity,work:{...fresh.work,unknownWrite:false}});return{outcome:'reconciled',reference};
 }finally{await host.close();}
}

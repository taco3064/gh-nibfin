import{join}from'node:path';import{readPrivate}from'./local-state.mjs';import{openFileHostStore}from'./host-store.mjs';
import{readNativeOwnerPolicy}from'./native-owner-policy.mjs';
const need=(v,c)=>{if(!v)throw Error('native-shape-feedback:'+c);};
// Upstream returns rejected unpublished proposals to their original Author and
// independently validates the revision. This narrow compatibility path handles
// the old Core parser's applicability-label grammar, never a Scope decision.
export async function nativeShapeFeedback(core,{root,store,claim,provider,allowPaused=false}={}){
 const s=await store.read();
 if(s.shapeRevision?.phase==='prepared'){
  const marker=s.shapeRevision,event=await store.get(marker.reference);
  need(event.kind==='native-shape-format-feedback'&&event.scopeHash===s.scopeHash&&event.cycle===s.cycle,'feedback-journal');
  const issue=await provider.request({endpoint:'/repos/'+s.target.repository+'/issues/'+s.target.number,target:s.target.repositoryId,operation:'native-shape-feedback',correlation:event.source.request});
  need(issue.outcome==='read'&&issue.data.state==='open'&&issue.data.assignees.some(a=>a.id===s.member.id)&&core.bodyDigest(core.parseRequirementsDocument(issue.data.body).scope)===s.scopeHash,'current-scope');
  const host=await openFileHostStore(core,{directory:join(root,'state')});try{
   for(let attempt=0;attempt<20;attempt++){
    const fresh=await host.read(),w=fresh.work,own=w.pending.filter(p=>p.cycle===s.cycle);
    need(w.restrictionsChecked&&(!w.paused||allowPaused)&&!w.hold&&!w.unknownWrite,'owner-restriction');
    need(own.length===0||own.length===1&&own[0].request===event.source.request&&own[0].status==='UNACCEPTED','pending-changed');
    if(own.length){const next=w.pending.filter(p=>p.cycle!==s.cycle),active=next.some(p=>p.kind!=='native-stage'||['ACTIVE','UNRESOLVED'].includes(p.status));
     try{await host.updateWork({expected:fresh.identity,work:{...w,pending:next,active,terminated:!active,reconciled:next.length===0&&!active}});}catch(e){if(e.message?.endsWith('work-compare-and-swap'))continue;throw e;}
    }
    await store.update(v=>{need(v.shapeRevision.id===marker.id,'feedback-changed');v.shapeRevision.phase='applied';return v;});
    return{outcome:'revision-prepared',id:marker.id,reference:marker.reference};
   }need(false,'work-unavailable');
  }finally{await host.close();}
 }
 if(claim.stage!=='shape-validator'||s.attempts['shape-validator']?.phase!=='unresolved'||s.attempts['shape-validator'].request!==claim.id)return{outcome:'not-applicable'};
 let failure,observations;const sessions=[],collections=[];
 for(const[key,kind]of Object.entries(s.records)){
  if(!['native-core-result','native-source-obligations','native-role-session','role-collection'].includes(kind))continue;
  const v=await store.get('native-record:'+key);
  if(kind==='native-core-result'&&v.result.request===claim.id)failure={reference:'native-record:'+key,value:v.result};
  if(kind==='native-source-obligations'&&v.observations.source?.base===s.base)observations=v.observations;
  if(kind==='native-role-session')sessions.push({reference:'native-record:'+key,value:v});
  if(kind==='role-collection')collections.push(v);
 }
 if(failure?.value.owner?.output?.finding!=='shape:obligations')return{outcome:'not-applicable'};
 const parsed=core.parseCandidateObligations(s.shapeBody);
 if(parsed.ok||!parsed.diagnostics.length||parsed.diagnostics.some(d=>d.code!=='applicability'||!['artifacts','hooks'].includes(d.path))
  ||parsed.inventory.artifacts.length||parsed.inventory.hooks.length)return{outcome:'not-applicable'};
 need((s.shapeRevisions??[]).length<1,'format-revision-bound');
 need(Object.keys(s.receipts).join(' ')==='shape'&&!s.workspace&&!s.candidate&&!s.head&&!s.completion
  &&Object.values(s.operations).every(o=>o.intent.kind==='dispatch'),'unpublished-proposal');
 need(observations?.requiredArtifacts?.length===0&&observations.hookObservation?.hooks?.length===0
  &&observations.source.repository===s.target.repository&&observations.hookObservation.areas.every(a=>a.status==='resolved'),'independent-applicability');
 core.compareEvidence(s.receipts.shape,s.expectations.shape);
 const author=sessions.filter(v=>v.value.metadata.execution===s.receipts.shape.execution.id&&v.value.metadata.role==='shape');
 const validator=collections.filter(v=>v.execution.role==='shape-validator'&&v.execution.source?.runId===claim.runId&&v.evidence?.result?.outcome==='approved');
 need(author.length===1&&validator.length===1,'independent-approved-validator');
 const reviewer=sessions.filter(v=>v.value.metadata.execution===validator[0].execution.id&&v.value.metadata.role==='shape-validator');
 need(reviewer.length===1&&author[0].value.snapshot.terminated&&reviewer[0].value.snapshot.terminated,'retained-sessions');
 const ledger=await readPrivate(join(root,'pool-state/ledger.json'));
 if(Object.values(ledger.jobs).some(j=>j.phase!=='released'&&j.request.repository===s.target.repository&&j.issue===s.target.number))return{outcome:'waiting'};
 need(ledger.jobs[claim.id]?.phase==='released'&&ledger.jobs[claim.id].request.runId===claim.runId,'original-runner-release');
 const owner=await readNativeOwnerPolicy(core,{root});need(owner.memberId===s.member.id,'owner-member');
 const current=await provider.request({endpoint:'/repos/'+s.target.repository+'/issues/'+s.target.number,target:s.target.repositoryId,operation:'native-shape-feedback',correlation:claim.id});
 need(current.outcome==='read'&&current.data.state==='open'&&!current.data.labels.some(l=>l.name==='shaped')&&current.data.assignees.some(a=>a.id===s.member.id)
  &&core.bodyDigest(core.parseRequirementsDocument(current.data.body).scope)===s.scopeHash,'current-scope');
 const host=await openFileHostStore(core,{directory:join(root,'state')});
 try{
  const h=await host.read(),w=h.work;
  need(w.restrictionsChecked&&(!w.paused||allowPaused)&&!w.hold&&!w.unknownWrite,'owner-restriction');
  const original=w.pending.filter(p=>p.cycle===s.cycle);need(original.length===1&&original[0].request===claim.id&&original[0].status==='UNACCEPTED','original-unaccepted-work');
  const evidence={kind:'native-shape-format-feedback',cycle:s.cycle,scopeHash:s.scopeHash,proposalHash:core.bodyDigest(s.shapeBody),failure:failure.reference,
   diagnostics:parsed.diagnostics,author:author[0].reference,validator:reviewer[0].reference,source:{request:claim.id,runId:claim.runId}};
  const id=core.contentIdentity(evidence),reference=await store.retain({...evidence,proposal:s.shapeBody,receipts:s.receipts,expectations:s.expectations,attempts:s.attempts,dispatch:s.dispatch,pending:original});
  await host.rescue(s.target.repositoryId).update(v=>{for(const c of Object.values(v.cases))if(c.target.cycle===s.cycle){
   c.operatorReconciliations??=[];c.operatorReconciliations.push({kind:'unpublished-shape-feedback',reference,previousOutcome:c.outcome,previousCode:c.code});
   c.outcome='SUPERSEDED';c.code='SHAPE_PROPOSAL_REVISION';c.nextCheckAt=null;
  }return v;});
  await store.update(v=>{
   need(v.scopeHash===s.scopeHash&&v.core===s.core&&core.contentIdentity(v.grant)===core.contentIdentity(s.grant)
    &&core.contentIdentity(v.receipts)===core.contentIdentity(s.receipts)&&core.contentIdentity(v.attempts)===core.contentIdentity(s.attempts)
    &&v.shapeBody===s.shapeBody&&!v.workspace&&!v.candidate&&!v.completion,'proposal-changed');
   v.shapeRevisions??=[];v.shapeRevisions.push({id,reference});v.shapeRevision={id,reference,phase:'prepared',previousBody:s.shapeBody,diagnostics:parsed.diagnostics};
   v.sessionResumes??={};for(const[role,session]of[['shape',author[0]],['shape-validator',reviewer[0]]])v.sessionResumes[role]={status:'UNACCEPTED',role,memberId:s.member.id,scopeHash:s.scopeHash,cycle:s.cycle,caseId:id,reference:session.reference};
   v.receipts={};v.expectations={};v.attempts={};v.shapeBody=null;v.stageInput=null;delete v.dispatch;return v;
  });
  return await nativeShapeFeedback(core,{root,store,claim,provider,allowPaused});
 }finally{await host.close();}
}

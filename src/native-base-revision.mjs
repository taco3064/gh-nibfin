import fs from'node:fs/promises';import{join}from'node:path';import{execFileSync}from'node:child_process';
import{openFileHostStore}from'./host-store.mjs';import{readPrivate}from'./local-state.mjs';
import{retainUnacceptedWorkspace}from'./native-unaccepted-work.mjs';
import{readNativeOwnerPolicy}from'./native-owner-policy.mjs';
const need=(v,c)=>{if(!v)throw Error('native-base-revision:'+c);};
const later=['pre-commit-hooks','pr','ci','shaper-review','merge'];
export async function retainNativeCandidate(core,{root,state:s,store,execution}){
 const parent=await fs.mkdtemp(join(root,'base-revision-')),directory=join(parent,'original');
 try{
  const workspace=core.importWorkspace(Buffer.from(core.canonicalJSON(s.workspace)),{expected:s.workspaceExpected});
  core.materializeWorkspace(workspace,{expected:s.workspaceExpected,root:directory});
  execFileSync('/usr/bin/git',['-c','core.hooksPath=/dev/null','update-ref','--no-deref','HEAD',s.base],{cwd:directory,env:{PATH:'/usr/bin:/bin',HOME:'/nonexistent',GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null'},timeout:30000,stdio:'pipe'});
  return await retainUnacceptedWorkspace({directory,execution,retain:store.retain});
 }finally{await fs.rm(parent,{recursive:true,force:true});}
}

// Like the upstream repair path, advancing main preserves the whole Candidate
// and original Writer conversation, then repeats Candidate/review/CI gates.
// It is ordinary integration work, not an AI Rescue attempt or a Shape reset.
export async function prepareNativeBaseRevision(core,{root,store,claim,provider,signal,transport}={}){
 const same=(a,b)=>core.canonicalJSON(a)===core.canonicalJSON(b);
 let s=await store.read();
 if(s.baseRevision?.phase!=='prepared'&&(!later.includes(claim?.stage)||!s.workspace||s.receipts.merge))return{outcome:'not-applicable'};
 const host=await openFileHostStore(core,{directory:join(root,'state')});
 const get=async endpoint=>{const r=await provider.request({endpoint,target:s.target.repositoryId,operation:'native-base-revision',correlation:s.cycle,signal});need(r.outcome==='read','observation');return r.data;};
 const base='/repos/'+s.target.repository;
 const live=async()=>{
  const h=await host.read(),w=h.work;need(w.restrictionsChecked&&!w.paused&&!w.hold,'owner-restriction');
  const issue=await get(base+'/issues/'+s.target.number);
  need(issue.state==='open'&&issue.assignees.some(a=>a.id===s.member.id)&&issue.body===s.shapeBody,'scope-or-shape-changed');
  const repo=await get(base);need(repo.id===s.target.repositoryId&&!repo.private&&repo.permissions?.admin===true,'repository-permission');
  const owner=await readNativeOwnerPolicy(core,{root});need(owner.memberId===s.member.id&&(await get('/user')).id===s.member.id,'owner-member');
  const ledger=await readPrivate(join(root,'pool-state/ledger.json'));
  if(Object.values(ledger.jobs).some(j=>j.phase!=='released'&&j.request.repository===s.target.repository&&j.issue===s.target.number))return null;
  const own=w.pending.filter(p=>p.cycle===s.cycle);
  need(own.every(p=>p.status==='UNACCEPTED'&&p.request===(s.baseRevision?.phase==='prepared'?s.baseRevision.source.id:claim?.id)),'original-work-active');
  // An unrelated unknown write cannot be cleared by this cycle's reconciliation.
  need(!w.unknownWrite||own.length===1&&w.pending.length===1,'unrelated-write-unknown');
  return h;
 };
 async function finish(){
  const h=await live();if(!h)return{outcome:'waiting'};
  const marker=s.baseRevision,own=h.work.pending.filter(p=>p.cycle===s.cycle);
  if(own.length||h.work.unknownWrite){
   const pending=h.work.pending.filter(p=>p.cycle!==s.cycle),active=pending.some(p=>p.kind!=='native-stage'||['ACTIVE','UNRESOLVED'].includes(p.status));
   await host.updateWork({expected:h.identity,work:{...h.work,pending,active,terminated:!active,reconciled:pending.length===0&&!active,unknownWrite:false}});
  }
  await store.update(v=>{need(same(v.baseRevision,marker),'revision-changed');v.baseRevision.phase='applied';return v;});
  return{outcome:'revision-prepared',base:s.base,reference:marker.reference};
 }
 try{
  if(s.baseRevision?.phase==='prepared')return await finish();
  const head=(await get(base+'/git/ref/heads/'+s.policy.merge.baseBranch)).object;
  need(head.type==='commit'&&/^[a-f0-9]{40}$/.test(head.sha),'default-head');
  if(head.sha===s.base)return{outcome:'current'};
  if(!await live())return{outcome:'waiting'};
  need(s.dispatch?.id===claim.id&&s.attempts[claim.stage]?.request===claim.id,'original-dispatch');
  const ci=await get(base+'/contents/.github/workflows/ci.yml?ref='+head.sha);
  need(ci.encoding==='base64'&&core.bodyDigest(Buffer.from(ci.content,'base64').toString('utf8'))===s.policy.ci.workflow.hash,'ci-policy-changed');
  for(const op of Object.values(s.operations))if(['merge','close','label'].includes(op.intent.kind)&&op.intent.binding?.head){
   if(!op.refs.submission)continue;
   const locator=op.refs.result&&s.operationBlobs[op.refs.result.ref.identity];
   const result=locator?await store.record(locator,'operation-record'):null;
   need(result?.value.outcome==='confirmed-no-effect','completion-write-reconciliation-required');
  }
  for(const[name,receipt]of Object.entries(s.receipts))core.compareEvidence(receipt,s.expectations[name]);
  const execution=s.receipts.deliver.execution,sessions=[];
  for(const[id,kind]of Object.entries(s.records))if(kind==='native-role-session'){
   const record=await store.get('native-record:'+id);
   if(record.metadata.execution===execution.id)sessions.push({reference:'native-record:'+id,record});
  }
  need(sessions.length===1&&sessions[0].record.snapshot.terminated&&sessions[0].record.metadata.role==='deliver'
   &&sessions[0].record.metadata.memberId===s.member.id,'original-writer-session');
  const session=sessions[0],oldBranch=s.branchRef??'nibfin/issue-'+s.target.number+'-'+s.cycle.slice(-12);
  const ledger=await readPrivate(join(root,'pool-state/ledger.json'));
  const writerJobs=Object.values(ledger.jobs).filter(j=>j.request.runId===execution.source.runId&&j.phase==='released');
  need(writerJobs.length===1&&/^[1-9][0-9]*$/.test(writerJobs[0].runnerId),'original-writer-runner');
  const histories=Object.values(s.publications).filter(a=>a[0]?.event.pin?.target?.headRef===oldBranch),commits=histories.flatMap(a=>a.filter(r=>r.event.kind==='commit').map(r=>r.event.value.commit));
  const refs=await get(base+'/git/matching-refs/heads/'+oldBranch),ref=refs.find(r=>r.ref==='refs/heads/'+oldBranch);
  need(!ref||commits.includes(ref.object.sha),'branch-changed');
  const endpoint=base+'/pulls?state=all&per_page=100';
  const prs=await provider.collection({endpoint,target:s.target.repositoryId,operation:'native-base-revision',correlation:s.cycle,signal,allowPage:p=>p.split('?')[0]===base+'/pulls'});
  need(prs.outcome==='complete','pull-readback');
  prs.data=prs.data.filter(pr=>pr.head.ref===oldBranch&&pr.head.repo?.id===s.target.repositoryId);
  for(const pr of prs.data)need(pr.head.ref===oldBranch&&pr.head.repo.id===s.target.repositoryId&&commits.includes(pr.head.sha)&&!pr.merged_at,'published-pr-changed-or-merged');
  const workspaceReference=await retainNativeCandidate(core,{root,state:s,store,execution});
  const proof={kind:'native-base-revision',core:s.core,scopeHash:s.scopeHash,shapeHash:core.bodyDigest(s.shapeBody),cycle:s.cycle,memberId:s.member.id,
   previousBase:s.base,base:head.sha,source:claim,sessionReference:session.reference,workspaceReference,accepted:false,
   previous:{receipts:s.receipts,expectations:s.expectations,attempts:s.attempts,dispatch:s.dispatch,workspace:s.workspace,workspaceExpected:s.workspaceExpected,
    candidate:s.candidate,head:s.head,publication:s.publication??null,hooks:s.hooks??null,review:s.review??null},published:prs.data};
  const reference=await store.retain(proof);
  // Upstream retires the old unmerged PR before publishing a rebased Candidate.
  // A fresh per-base branch avoids overwriting another actor's branch or an
  // uncertain push. Every write is preceded by readback and retained intent.
  for(const pr of prs.data){
   const path=base+'/pulls/'+pr.number;
   const guard=async()=>{need(await live(),'resources-active');const p=await get(path);
    need(!p.merged&&p.head.sha===pr.head.sha&&p.head.ref===oldBranch,'pr-changed-or-merged');return p;};
   if((await guard()).state==='open'){
    await store.retain({kind:'native-base-revision-pr-retirement',reference,path,head:pr.head.sha});
    const writes=core.createGithubProvider({attempts:1,...(transport?{transport}:{}),authorize:async r=>{
     need(r.method==='PATCH'&&r.endpoint===path&&r.target===s.target.repositoryId&&same(r.body,{state:'closed'}),'retirement-write');
     await guard();return true;}});
    await writes.request({method:'PATCH',endpoint:path,target:s.target.repositoryId,operation:'installation',correlation:reference.slice(14),body:{state:'closed'},signal});
   }
   const p=await guard();need(p.state==='closed','pr-retirement-unresolved');
  }
  need((await get(base+'/git/ref/heads/'+s.policy.merge.baseBranch)).object.sha===head.sha,'base-advanced-during-preparation');
  need(await live(),'resources-active');
  const id=core.contentIdentity({reference});
  const target={repository:s.target.repository,repositoryId:s.target.repositoryId,issueId:s.target.issueId,number:s.target.number,cycle:s.cycle,
   authority:s.authority.identity,requirements:s.scopeHash,scopeHash:s.scopeHash,shapeHash:core.bodyDigest(s.shapeBody),candidate:s.workspaceExpected.identity,
   execution:execution.id,role:'deliver',adapter:'codex',access:'read-only',status:'UNACCEPTED',memberId:s.member.id,sessionMemberId:s.member.id,
   runnerMemberId:s.member.id,runnerId:writerJobs[0].runnerId,session:session.record.metadata.id,source:{runId:execution.source.runId,attempt:execution.source.attempt}};
  await store.update(v=>{
   need(v.base===s.base&&same(v.receipts,s.receipts)&&same(v.attempts,s.attempts)&&same(v.dispatch,s.dispatch)&&same(v.workspace,s.workspace)
    &&v.scopeHash===s.scopeHash&&v.shapeBody===s.shapeBody&&same(v.authority,s.authority)&&same(v.grant,s.grant)&&same(v.policy,s.policy),'candidate-changed');
   v.baseRevisions??=[];v.baseRevisions.push({id,reference,oldBase:s.base,base:head.sha});
   v.baseRevision={id,reference,phase:'prepared',source:claim};v.base=head.sha;
   v.branchRef='nibfin/issue-'+s.target.number+'-'+s.cycle.slice(-12)+'-'+head.sha;
   v.receipts=Object.fromEntries(['shape','shape-validator','publication'].map(k=>[k,v.receipts[k]]));
   v.expectations=Object.fromEntries(['shape','shape-validator','publication'].map(k=>[k,v.expectations[k]]));
   v.attempts=Object.fromEntries(['shape','shape-validator','publication'].map(k=>[k,v.attempts[k]]));
   for(const key of ['workspace','workspaceExpected','candidate','head','ci','completion','stageInput'])v[key]=null;
   for(const key of ['dispatch','hooks','publication','review','reviewPin','mergeIntent','mergeOperation','completionPlan'])delete v[key];
   v.deferredRoles??={};v.deferredRoles.deliver={phase:'prepared',scopeHash:s.scopeHash,memberId:s.member.id,proof:reference,
    sessionReference:session.reference,workspaceReference,caseId:id,target,plan:{kind:'default-branch-advanced',previousBase:s.base,base:head.sha,
     steps:['Continue the original Writer conversation and preserve all retained Candidate changes. The trusted host applies them with a three-way merge onto the current main.',
      'Resolve every reported conflict in the original changed paths, preserving both the authorized work and current main changes. Remove conflict markers and unmerged index entries; do not replace protected Git metadata.',
      'Refresh source and rerun applicable checks on the rebased bytes. Old Acceptance is historical. Repeat Candidate, both hooks, independent Accept, exact-head CI, final review and protected merge.']}};
   delete v.sessionResumes?.deliver;return v;
  });
  s=await store.read();return await finish();
 }finally{await host.close();}
}

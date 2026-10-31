import{randomUUID}from'node:crypto';
const need=(v,c)=>{if(!v)throw Error('native-deferred-role:'+c);};

// An explicit Owner plan revision retains original conversations and unaccepted
// Writer bytes. Rebind them only when the normal dispatcher reaches that role;
// the existing Core continuation and session owners still enforce every gate.
export async function prepareDeferredNativeRole(core,{store,binding,memberId}){
 const s=await store.read(),role=binding.stage,seed=s.deferredRoles?.[role];
 if(!seed||seed.phase!=='prepared')return;
 need(['shape','shape-validator','deliver'].includes(role)&&seed.scopeHash===s.scopeHash&&seed.memberId===memberId,'scope-member');
 need(binding.request.repository===s.target.repository&&binding.request.repositoryId===s.target.repositoryId
  &&binding.issue===s.target.number&&binding.memberId===memberId,'invocation');
 const proof=await store.get(seed.proof),session=await store.get(seed.sessionReference);
 need(proof.kind==='native-owner-shape-revision'&&proof.scopeHash===s.scopeHash&&proof.memberId===memberId
  &&proof.cycle===s.cycle&&session.kind==='native-role-session'&&session.metadata.role===role
  &&session.metadata.memberId===memberId&&session.snapshot.terminated===true,'owner-and-original-session');
 need(seed.target.execution===session.metadata.execution&&seed.target.role===role&&seed.target.session===session.metadata.id
  &&seed.target.repositoryId===s.target.repositoryId&&seed.target.issueId===s.target.issueId&&seed.target.cycle===s.cycle,'original-target');
 const workspace=seed.workspaceReference?await store.get(seed.workspaceReference):null;
 need(role!=='deliver'||workspace?.kind==='native-unaccepted-workspace'&&workspace.status==='UNACCEPTED'
  &&workspace.execution.id===session.metadata.execution,'writer-custody');
 const target={...seed.target,authority:s.authority.identity,scopeHash:s.scopeHash,requirements:s.scopeHash,
  shapeHash:s.shapeBody===null?null:core.bodyDigest(s.shapeBody),candidate:workspace?core.contentIdentity(workspace):null};
 const capture=core.contentIdentity({scope:s.scopeHash,shape:target.shapeHash,base:s.base,
  session:session.snapshot.digest,workspace:seed.workspaceReference??null});
 const continuation=core.createRescueContinuation({
  load:async()=>{const current=await store.read();need(current.scopeHash===s.scopeHash&&current.base===s.base
    &&current.shapeBody===s.shapeBody&&current.authority.identity===s.authority.identity
    &&core.contentIdentity(current.deferredRoles?.[role])===core.contentIdentity(seed),'current-drift');
   return{status:'UNACCEPTED',target,terminated:true,workspace:capture,session:session.metadata.id,memberId};},
  retain:async value=>{await store.retain({kind:'native-rescue-continuation',...value});return{outcome:'retained'};},
  prepare:async value=>{
   const next={execution:'role-'+randomUUID(),request:binding.request.id,result:'result-'+randomUUID(),session:value.session,
    memberId,role,freshBudget:true,status:'UNACCEPTED',workspace:value.workspace,access:role==='deliver'?'writer':'read-only'};
   await store.update(v=>{
    need(v.scopeHash===s.scopeHash&&v.base===s.base&&v.shapeBody===s.shapeBody&&v.authority.identity===s.authority.identity
     &&core.contentIdentity(v.deferredRoles?.[role])===core.contentIdentity(seed),'seed-changed');
    v.sessionResumes??={};v.sessionResumes[role]={status:'UNACCEPTED',role,memberId,scopeHash:s.scopeHash,cycle:s.cycle,
     caseId:seed.caseId,reference:seed.sessionReference,workspaceReference:seed.workspaceReference??null,next,plan:seed.plan};
    v.deferredRoles[role]={...seed,phase:'armed',request:binding.request.id};return v;
   });return next;
  }
 });
 return continuation({target,caseId:seed.caseId,proof:{kind:'owner-plan-revision',reference:seed.proof},plan:seed.plan});
}

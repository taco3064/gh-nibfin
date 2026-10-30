const need=(ok,code)=>{if(!ok)throw Error('native-session:'+code);};

// Only a private Owner-reconciled case can restore one original role session.
// Scope, member, role and cycle remain bound; this is not an acceptance receipt
// or permission to resume a different reviewer from a Writer conversation.
export async function resumeNativeSession(core,{store,role,memberId,directory,request}={}){
 const state=await store.read(),resume=state.sessionResumes?.[role];if(!resume)return {directory,id:null,memberId};
 need(resume.status==='UNACCEPTED'&&resume.role===role&&resume.memberId===memberId
  &&resume.scopeHash===state.scopeHash&&resume.cycle===state.cycle,'original-case');
 const original=await store.get(resume.reference);
 need(original.kind==='native-role-session'&&original.metadata.id===original.snapshot.session
  &&original.metadata.role===role&&original.metadata.memberId===memberId,'original-session');
 const target={repositoryId:state.target.repositoryId,issueId:state.target.issueId,role,memberId,adapter:'codex'},identity=core.contentIdentity(resume);
 const recover=core.createRoleSessionRecovery({
  current:async()=>{const current=await store.read();need(current.scopeHash===state.scopeHash&&current.cycle===state.cycle
    &&core.contentIdentity(current.sessionResumes?.[role])===identity,'case-drift');return {status:'UNACCEPTED',target};},
  load:async()=>original.snapshot,
  restore:async({original:snapshot})=>core.restoreRescueSession({snapshot,directory,
   expected:{memberId,role,session:original.metadata.id,execution:original.metadata.execution,adapter:'codex'}}),
  retain:async proof=>{await store.retain({version:1,kind:'native-role-continuation',proof,original:resume.reference,request});return {outcome:'retained'};}
 });
 const restored=await recover({target,caseId:resume.caseId,session:original.metadata.id,originalExecution:original.metadata.execution});
 return {...restored,memberId};
}

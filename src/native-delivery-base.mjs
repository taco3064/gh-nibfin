const need=(v,c)=>{if(!v)throw Error('native-delivery-base:'+c);};
// Upstream starts a new Writer from the current default branch. A stopped
// Writer's original bytes/base remain immutable across session continuation.
export async function refreshNativeDeliveryBase(core,{store,provider,request,signal}={}){
 const s=await store.read();if(s.workspace||s.candidate||s.sessionResumes?.deliver)return{outcome:'retained-original-base'};
 need(s.receipts.publication&&s.attempts.deliver?.request===request&&s.attempts.deliver.phase==='running','first-writer');
 const get=async endpoint=>{const r=await provider.request({endpoint,target:s.target.repositoryId,operation:'native-delivery-base',correlation:request,signal});need(r.outcome==='read','fresh-read');return r.data;};
 const root='/repos/'+s.target.repository,head=(await get(root+'/git/ref/heads/'+s.policy.merge.baseBranch)).object;
 need(head.type==='commit'&&/^[a-f0-9]{40}$/.test(head.sha),'default-head');if(head.sha===s.base)return{outcome:'current',base:s.base};
 const ci=await get(root+'/contents/.github/workflows/ci.yml?ref='+head.sha);
 need(ci.type==='file'&&ci.encoding==='base64'&&core.bodyDigest(Buffer.from(ci.content,'base64').toString('utf8'))===s.policy.ci.workflow.hash,'ci-policy-changed');
 await store.update(v=>{need(v.scopeHash===s.scopeHash&&v.base===s.base&&v.attempts.deliver?.request===request&&!v.workspace&&!v.candidate&&!v.sessionResumes?.deliver
  &&core.contentIdentity(v.grant)===core.contentIdentity(s.grant)&&core.contentIdentity(v.receipts)===core.contentIdentity(s.receipts),'writer-changed');
  v.baseObservations??=[];v.baseObservations.push({kind:'fresh-first-writer-base',previous:v.base,current:head.sha,request});v.base=head.sha;return v;});
 return{outcome:'refreshed',previous:s.base,base:head.sha};
}

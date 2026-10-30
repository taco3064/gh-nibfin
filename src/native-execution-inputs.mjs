import{readNativeOwnerPolicy}from'./native-owner-policy.mjs';
import{openNativeCycle}from'./native-delivery-store.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-execution:'+code);};

// Actual local Owner policy and Issue observations form the authority boundary.
// No event, role output or imported historical label can supply eligibility.
export async function nativeExecutionInputs(core,{root,store,configuration,binding,provider,signal,checkpoint,transport}={}){
 const state=await store.read(),owner=await readNativeOwnerPolicy(core,{root}),target=state.target;
 need(owner.memberId===state.member.id&&configuration.targets.some(t=>t.repositoryId===target.repositoryId&&t.repository===target.repository),'owner');
 const repository={id:target.repositoryId,name:target.repository,visibility:'public'};
 const grantContext=operation=>({now:Date.now(),trustedKeys:new Map([[owner.keyId,owner.publicKey]]),
  revocation:{id:owner.keyId,available:true,revokedRecords:owner.revokedRecords??[]},
  expectedAuthority:core.authoritySnapshot(state.authority),expectedPolicy:state.policy,expectedWork:state.grant.payload.work,
  request:{repository,issueId:target.issueId,number:target.number,path:state.paths[0],operation}});
 const authorization=core.createAuthorizationBoundary({attempts:1,maxCheckpointMs:120000,...(transport?{transport}:{}),
  getCheckpoint:async intent=>{
   await checkpoint();const current=await readNativeOwnerPolicy(core,{root});need(current.identity===owner.identity,'owner-policy-changed');
   return {request:grantContext(intent.operation).request,member:state.member,operator:state.member,grant:state.grant,
    grantContext:grantContext(intent.operation),...(intent.method?{write:{method:intent.method,endpoint:intent.endpoint,target:intent.target}}:{})};
  },beforeMutation:async()=>{await checkpoint();return !signal?.aborted;}});
 const selected=configuration.targets.find(t=>t.repositoryId===target.repositoryId);
 const issues=[];
 for(const project of configuration.targets){
  const path='/repos/'+project.repository+'/issues',list=await provider.collection({endpoint:path+'?state=all&per_page=100',target:project.repositoryId,
   operation:'native-dependency-catalog',correlation:binding.request.id,signal,allowPage:p=>p.split('?')[0]===path});
  need(list.outcome==='complete','dependency-catalog');
  for(const issue of list.data)if(!issue.pull_request)issues.push({repository:project.repository,repositoryId:project.repositoryId,
   issueId:issue.node_id,databaseId:issue.id,number:String(issue.number)});
 }
 const dependencyInput={target,issues};
 const dependencies=core.createDependencyEvaluator({provider,maxNodes:1000,maxReads:3000,maxEdges:10000,timeoutMs:30000,
  authorize:async issue=>{
   const project=configuration.targets.find(t=>t.repositoryId===issue.repositoryId&&t.repository===issue.repository);need(project,'dependency-target');
   const startedAt=Date.now(),r=await provider.request({endpoint:'/repos/'+issue.repository+'/collaborators/'+state.member.login+'/permission',
    target:issue.repositoryId,operation:'native-dependency-permission',correlation:binding.request.id,signal});
   need(r.outcome==='read'&&r.data.user?.id===state.member.id&&['write','maintain','admin'].includes(r.data.permission),'dependency-permission');
   return {ok:true,observation:{repositoryId:issue.repositoryId,issueId:issue.issueId,operation:'readiness',startedAt,observedAt:Date.now()}};
  },current:async issue=>{try{
    const prior=await openNativeCycle(core,{root,target:issue}),value=await prior.read();
    return value.completion?{identity:value.completion.identity,expected:value.completionCurrent}:null;
   }catch(e){if(e.code==='ENOENT')return null;throw e;}},
  retrieve:async issue=>{const prior=await openNativeCycle(core,{root,target:issue});return (await prior.read()).completion;}});
 const selector=core.createAssigneeSelector({provider,authorizationFor:async member=>{need(member.id===state.member.id,'selected-member');return authorization;},
  maxCheckpointMs:120000,executionPool:configuration,poolReadiness:async request=>{
   await checkpoint();need(binding.jobRecord.runnerId&&request.memberId===state.member.id,'actual-runner-member');
   return {memberId:state.member.id,pool:core.contentIdentity(configuration),status:'ready',observedAt:Date.now()};
  }});
 const operation=stage=>({publication:'publish-shape',pr:'commit-pr',ci:'observe-ci',final:'verify-deployment',closure:'close',delivered:'mark-delivered'})[stage]??stage;
 const observedAt=Date.now();
 return {state,authorization,dependencies,dependencyInput,selector,grantContext,
  selectionInput:stage=>({target:{id:selected.repositoryId,owner:selected.repository.split('/')[0],repository:selected.repository.split('/')[1],ownerId:selected.ownerId,ownerType:selected.ownerType},
   registration:{level:'repository',id:selected.repositoryId},executionPool:configuration,issueId:target.issueId,number:target.number,
   operation:operation(stage),conditions:{platform:'linux',labels:['x64','nibfin-request-'+binding.request.id]},records:[],
   execution:{repositoryId:selected.repositoryId,issueId:target.issueId,operation:operation(stage),observedAt,state:'new'}})};
}

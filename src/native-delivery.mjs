import{nativeRoleHost}from'./native-role-host.mjs';import{nativeExecutionInputs}from'./native-execution-inputs.mjs';
import{prepareNativeCycle}from'./native-cycle-policy.mjs';import{observeNativeObligations}from'./native-obligations.mjs';
import{openFileHostStore}from'./host-store.mjs';import{join}from'node:path';
import{nativeDeliveryOwners}from'./native-delivery-owners.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-delivery:'+code);};
const copy=structuredClone;

/** Maintained public-Core composition. A real authenticated project Job supplies
 * attribution; private source authority and original receipts supply the plan.
 * Neither the model nor an IPC caller can choose callbacks, owners or commands.
 */
export async function executeNativeDelivery(core,{root,binding,context,provider,signal,progress,transport,roleFactory=nativeRoleHost}={}){
 need(core.distributionAPI?.nativeHost===1,'compatible-native-core-required');
 let roles,firstCheckpoint=true,lastRemote=0,completed=false;
 const hostStore=await openFileHostStore(core,{directory:join(root,'state')});
 const configuration=binding.configuration,repository=binding.request.repository;
 const checkpoint=async()=>{
  need(!signal?.aborted,'cancelled');const state=await hostStore.read();
  // A retained pause/hold cannot be cleared by a request or by model output.
  need(state.work.restrictionsChecked&&state.work.paused===false&&!state.work.hold&&!state.work.unknownWrite,'host-restriction');
  if(firstCheckpoint||Date.now()-lastRemote>=30000){firstCheckpoint=false;
   const r=await provider.request({endpoint:'/repos/'+repository+'/issues/'+binding.issue,target:binding.request.repositoryId,
    operation:'native-live-scope',correlation:binding.request.id,signal});
   need(r.outcome==='read'&&r.data.assignees?.some(a=>a.id===binding.memberId),'live-issue');
   if(store){const retained=await store.read();need(core.parseRequirementsDocument(r.data.body).scope===retained.scopeBody,'scope-drift');}
   lastRemote=Date.now();
  }
 };
 let store;
 store=await prepareNativeCycle(core,{root,configuration,repository,number:binding.issue,provider,context,signal});
 const state=await store.read(),stage=core.nextDeliveryStage(state.receipts,null);
 need(stage===binding.stage,'fresh-stage-required');
 const source={repositoryId:state.target.repositoryId,workflow:core.bodyDigest(binding.sourceCommit+':.github/workflows/nibfin-stage.yml'),
  runId:binding.request.runId,attempt:binding.request.attempt};
 // Claim the original execution before any role preparation. Interruption is
 // retained as uncertainty and cannot become permission to launch it again.
 await store.update(s=>{need(!s.attempts[stage],'original-stage-reconciliation-required');
  s.attempts[stage]={request:binding.request.id,source,phase:'running'};return s;});
 try{
  await checkpoint();
  const input=await nativeExecutionInputs(core,{root,store,configuration,binding,provider,signal,checkpoint,transport});
  const role=['shape','shape-validator','deliver','accept','shaper-review'].includes(stage)?stage:stage==='publication'?'shape':'deliver';
  const instructions=core.selectInstructions({skill:core.skillForRole(role),role});
  const authority={authority:state.authority.identity,grant:core.contentIdentity(state.grant),policy:core.contentIdentity(state.policy),core:state.core,
   runtime:state.policy.runtime,instructions:instructions.identity,repositoryId:state.target.repositoryId,issueId:state.target.issueId,
   scopeHash:state.scopeHash,shapeHash:state.shapeBody===null?null:core.bodyDigest(state.shapeBody)};
  const pin={restrictions:{restrictionsChecked:true,hold:null,paused:false,unknownWrite:false},target:state.target,cycle:state.cycle,
   visibility:'public',ownership:binding.ownerType==='Organization'?'organization':'personal',binding:authority,scopeBody:state.scopeBody,shapeBody:state.shapeBody,
   authority:{export:state.authority,context:{grant:state.grant,grantContext:input.grantContext(stage==='publication'?'publish-shape':stage),
    expected:{source:state.authority.source,revision:state.authority.revision,approvalId:state.authority.approvalId,cycle:state.cycle}}},
   receipts:state.receipts,evidenceCurrent:state.expectations,candidate:state.candidate,base:state.base,head:state.head,ciIdentity:null,
   completion:state.completion,completionCurrent:state.completionCurrent??null,transition:null,operationIdentity:null,stageInput:state.stageInput,
   dependencyInput:input.dependencyInput,selectionInput:input.selectionInput(stage),invocation:{request:binding.request.id,run:{id:source.runId,attempt:source.attempt}},prior:{state:'new'}};
  roles=await roleFactory(core,{root,binding,base:state.base,repository,memberId:state.member.id,source,retain:store.retain,checkpoint,signal,progress});
  if(state.workspace){const candidate=core.importWorkspace(Buffer.from(core.canonicalJSON(state.workspace)),{expected:state.workspaceExpected});
   core.materializeWorkspace(candidate,{expected:state.workspaceExpected,root:roles.candidateDirectory});}
  const observations=await observeNativeObligations(core,{directory:state.workspace?roles.candidateDirectory:roles.sourceDirectory,scopeBody:state.scopeBody,base:state.base,repository});
  await store.retain({kind:'native-source-obligations',observations});
  const operations=options=>core.createControlledOperations({...options,authorization:input.authorization,provider,
   operationRecords:store.operationRecords,locate:store.locateOperation,execution:{runId:source.runId,attempt:source.attempt}});
  const evidenceCurrent=async id=>{
   try{return roles.get(id).current();}catch{
    const expected=Object.values(state.expectations).find(e=>e.expected.execution.id===id);need(expected,'retained-role-expectation');return copy(expected);
   }
  };
  const composed=await nativeDeliveryOwners(core,{state,stage,pin,roles,store,provider,authorization:input.authorization,dependencies:input.dependencies,
   dependencyInput:input.dependencyInput,operations,evidenceCurrent,source,observations,checkpoint,signal,progress});
  if(stage==='pr'){
   const session=core.createPublicationGit({repository,repositoryId:state.target.repositoryId}).open(composed.workspace,composed.message,composed.author,{signal});
   try{state.head=session.commit;pin.head=session.commit;}finally{session.close();}
  }
  const owners={...composed.owners,shape:{provider,authorization:input.authorization,dependencies:input.dependencies,dependencyInput:input.dependencyInput,
   current:()=>copy({target:state.target,binding:authority,original:{kind:'raw-scope',body:state.scopeBody,hash:state.scopeHash},
    instructions:{},requirementsDocument:'scope-shape-v1',decision:core.contentIdentity({cycle:state.cycle,stage,request:binding.request.id}),
    correlation:binding.correlation,source:{request:binding.request.id,event:'workflow_dispatch',repositoryId:state.target.repositoryId,
     runId:source.runId,attempt:source.attempt,actorId:state.member.id},
    operationBinding:{cycle:state.cycle,runId:source.runId,attempt:source.attempt,candidate:null,base:state.base,head:null}}),
   prepareRole:task=>roles.prepareRole(task),readBody:async id=>roles.get(id).readBody(),operations,evidenceCurrent,
   obligations:async()=>copy(observations)}};
  const runtime=core.createCore({current:()=>copy(pin),provider,authorization:input.authorization,dependencies:input.dependencies,
   selector:input.selector,owners,checkpoint});
  if(stage==='closure')await store.update(s=>{need(!s.completionPlan,'original-completion-plan-required');s.completionPlan=composed.completionState;return s;});
  const request={version:1,target:state.target,cycle:state.cycle,request:binding.request.id,run:pin.invocation.run,
   expected:{stage,decision:core.contentIdentity({stage,request:binding.request.id})}};
  const inspection=await runtime.inspect(request,{signal});
  if(!inspection.current||inspection.disposition==='blocked'){
   await store.retain({kind:'native-inspection',inspection});await progress({status:'blocked',reason:inspection.reason});return inspection;
  }
  request.expected=inspection.current;await store.retain({kind:'native-execution-intent',request,pin});
  await progress({status:'running',reason:'core-'+stage+'-owner-started'});
  const result=await runtime.run(request,{signal});await store.retain({kind:'native-core-result',result});
  if(result.disposition==='completed'){
   const output=result.owner.output;let receipt;
   if(stage==='shape')receipt=output.author.receipt;
   else if(stage==='shape-validator')receipt=output.validator.receipt;
   else if(stage==='publication'){
    receipt=core.createEvidence({expected:{version:1,stage,binding:authority,
     execution:{role:'deliver',skill:'deliver',id:'native-'+stage+'-'+binding.request.id,request:binding.request.id,result:'native-result-'+binding.request.id,source},
     inputs:{body:authority.shapeHash,author:state.receipts.shape.execution.id,validator:state.receipts['shape-validator'].execution.id,
      validation:core.contentIdentity(state.receipts['shape-validator']),readback:core.contentIdentity(output.publication)},result:null},consumed:[]});
   }else if(stage==='pre-accept-hooks'||stage==='pre-commit-hooks')receipt=composed.evidence(stage,{candidate:state.candidate,
    phase:stage==='pre-accept-hooks'?'pre-accept':'pre-commit',...(stage==='pre-commit-hooks'?{accept:core.contentIdentity(state.receipts.accept)}:{}),receipt:output.receipt.identity});
   else if(stage==='pr')receipt=composed.evidence(stage,{candidate:state.candidate,accept:core.contentIdentity(state.receipts.accept),
    hooks:core.contentIdentity(state.receipts['pre-commit-hooks']),head:output.value.commit,base:state.base,readback:core.contentIdentity(output.value.pr.observation)});
   else if(stage==='ci'){
    need(output.status==='satisfied'&&output.qualifies===true,'ci-not-qualified');const {provenance,...boundCI}=output;
    receipt=composed.evidence(stage,{pr:core.contentIdentity(state.receipts.pr),head:state.head,base:state.base,policy:authority.policy,checks:core.contentIdentity(boundCI)});
   }else if(stage==='final')receipt=composed.evidence(stage,{merge:core.contentIdentity(state.receipts.merge),
    commit:state.receipts.merge.inputs.commit,postMerge:composed.postMergeProof,deployment:output.proof});
   else if(stage==='delivered')receipt=output.receipt.payload.receipts.delivered;
   else receipt=output.evidence;
   need(receipt,'owner-receipt');const expected=['shape','shape-validator','deliver','accept','shaper-review'].includes(stage)
    ?await evidenceCurrent(receipt.execution.id):{expected:copy(receipt),consumed:[]};
   core.compareEvidence(receipt,expected);
   let captured;
   if(stage==='deliver'){
    captured=core.captureWorkspace(await composed.owners.deliver.captureInput());
    core.importWorkspace(Buffer.from(core.canonicalJSON(output.workspace)),{expected:captured.expected});
    need(core.canonicalJSON(captured.candidate)===core.canonicalJSON(receipt.inputs.candidate),'writer-candidate');
   }
   await roles.close();roles=null;
   await store.update(s=>{s.receipts[stage]=receipt;s.expectations[stage]=expected;s.attempts[stage].phase='completed';
    if(stage==='shape'){s.shapeBody=output.author.body;s.stageInput=output;}
    else if(stage==='shape-validator')s.stageInput=output;
    else s.stageInput=null;
    if(stage==='deliver'){s.workspace=output.workspace;s.workspaceExpected=captured.expected;s.candidate=captured.candidate;}
    if(stage==='pre-accept-hooks'||stage==='pre-commit-hooks'){
     const hook=composed.hookInput(stage==='pre-accept-hooks'?'pre-accept':'pre-commit');s.hooks??={};
     s.hooks[stage==='pre-accept-hooks'?'first':'second']={receipt:output.receipt,executionId:hook.executionId};
    }
    if(stage==='pr'){s.head=output.value.commit;s.publication=output.value;}
    if(stage==='ci'){const{provenance,...boundCI}=output;s.ci=boundCI;}
    if(stage==='shaper-review'){s.review=output;s.reviewPin=composed.reviewPin;}
    if(stage==='merge'){s.mergeOperation=output.operation;s.mergeIntent=composed.mergeIntent;}
    if(stage==='closure')s.closed=true;
    if(stage==='delivered'){s.completion=output.receipt;s.completionCurrent=Object.fromEntries(core.coreStages.slice(2,14)
     .map(name=>[name,name==='delivered'?expected:s.expectations[name]]));}
    return s;});
  }else await store.update(s=>{s.attempts[stage].phase='unresolved';s.attempts[stage].reason=result.reason;return s;});
  completed=result.disposition==='completed';return result;
 }finally{try{if(roles)await roles.close({preserveWriter:!completed});}finally{await hostStore.close();}}
}

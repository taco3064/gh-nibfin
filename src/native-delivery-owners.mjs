import{nativeWakeupInbox}from'./native-wakeups.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-owners:'+code);};
const copy=structuredClone;

// All mutation and review owners come from the installed Core. This module
// adapts private native records and real official observations to their inputs.
export async function nativeDeliveryOwners(core,{root,state,stage,pin,roles,store,provider,authorization,dependencies,dependencyInput,
 operations,evidenceCurrent,source,observations,checkpoint,signal,progress}={}){
 const c=core.contentIdentity,t=state.target,b=pin.binding;
 const sourceEvent={request:pin.invocation.request,event:'workflow_dispatch',repositoryId:t.repositoryId,
  runId:source.runId,attempt:source.attempt,actorId:state.member.id};
 const bound={repository:t.repository,repositoryId:t.repositoryId,issueId:t.issueId,number:t.number,cycle:state.cycle,
  scopeHash:b.scopeHash,shapeHash:b.shapeHash,authorization:b.grant,runId:source.runId,attempt:source.attempt,
  candidate:state.workspaceExpected?.identity??null,base:state.base,head:state.head};
 const execution={role:'deliver',skill:'deliver',id:'native-'+stage+'-'+pin.invocation.request,request:pin.invocation.request,
  result:'native-result-'+pin.invocation.request,source};
 const evidence=(name,inputs)=>core.createEvidence({expected:{version:1,stage:name,binding:b,execution,inputs,result:null},consumed:[]});
 const current=name=>copy(state.expectations[name]);
 const item=name=>({value:copy(state.receipts[name]),current:current(name)});
 const workspace=state.workspace?core.importWorkspace(Buffer.from(core.canonicalJSON(state.workspace)),{expected:state.workspaceExpected}):null;
 const common={repository:t.repository,binding:b,scopeBody:state.scopeBody,shapeBody:state.shapeBody,decisions:[],instructions:{},
  visibility:pin.visibility,ownership:pin.ownership};
 const author={name:state.member.login,email:state.member.id+'+'+state.member.login+'@users.noreply.github.com',timestamp:Math.floor(state.createdAt/1000)};
 const message=Buffer.from(state.title.replace(/[\r\n\x00-\x1f]/g,' ').slice(0,160)+'\n\nRefs #'+t.number+'\n');
 const hookProfile={id:'linux-amd64-rootless-hooks-v1',image:state.memberRuntime.image,docker:core.linuxCodexProfiles[state.memberRuntime.runtimeProfile].docker};
 const phaseName=stage==='pre-commit-hooks'?'pre-commit':'pre-accept';
 function hookInput(phase){
  const first=state.hooks?.first;
  return {workspace,expected:workspace.expected,phase,executionId:phase==='pre-accept'?'first-'+state.cycle:'second-'+state.cycle,
   message,observation:observations.hookObservation,profile:hookProfile,
   ...(phase==='pre-commit'?{first:first.receipt,firstExecutionId:first.executionId,firstReceiptCurrent:{identity:first.receipt.identity,consumed:[]},
    firstStage:state.receipts['pre-accept-hooks'],firstStageCurrent:current('pre-accept-hooks'),firstStageIdentity:c(state.receipts['pre-accept-hooks']),
    accept:state.receipts.accept,acceptCurrent:current('accept')}:{})};
 }
 const hookState=()=>{const second=state.hooks.second,first=state.hooks.first;return{
  profile:hookProfile,message:message.toString('base64'),executionId:second.executionId,receipt:second.receipt,
  receiptCurrent:{identity:second.receipt.identity,consumed:[]},first:first.receipt,firstExecutionId:first.executionId,
  firstReceiptCurrent:{identity:first.receipt.identity,consumed:[]},firstStage:state.receipts['pre-accept-hooks'],firstStageCurrent:current('pre-accept-hooks')};};
 const reviewEvidence=()=>Object.fromEntries(Object.entries({author:'shape',validator:'shape-validator',publication:'publication',deliver:'deliver',
  accept:'accept',secondStage:'pre-commit-hooks',pr:'pr',ci:'ci'}).map(([key,value])=>[key,item(value)]));
 const target={...t,cycle:state.cycle,headRef:'nibfin/issue-'+t.number+'-'+state.cycle.slice(-12),baseRef:state.policy.merge.baseBranch,
  branchPrefix:'nibfin',paths:state.paths,...(state.publication?{prId:state.publication.pr.observation.id,prNumber:state.publication.pr.observation.number}:{})};
 const owners={deliver:{current:()=>copy({...common,capture:{root:roles.workspace('deliver'),base:state.base,permittedPaths:state.paths,
    binding:{repository:t.repository,cycle:state.cycle,policy:b.policy,scopeHash:b.scopeHash,shapeHash:b.shapeHash}},
   publication:state.receipts.publication,publicationCurrent:current('publication')}),
  captureInput:()=>({root:roles.workspace('deliver'),base:state.base,permittedPaths:state.paths,scopeBody:state.scopeBody,shapeBody:state.shapeBody,
   binding:{repository:t.repository,cycle:state.cycle,policy:b.policy,scopeHash:b.scopeHash,shapeHash:b.shapeHash}}),
  prepareRole:task=>roles.prepareRole(task),evidenceCurrent},
  hooks:{image:state.memberRuntime.image,socket:state.memberRuntime.socket,runtimeProfile:state.memberRuntime.runtimeProfile},hookInput:()=>hookInput(phaseName),
  accept:{current:()=>copy({...common,workspace:workspace.expected,obligations:observations,deliver:state.receipts.deliver,deliverCurrent:current('deliver'),
    hooks:{stage:state.receipts['pre-accept-hooks'],stageCurrent:current('pre-accept-hooks'),receipt:state.hooks.first.receipt,
     receiptCurrent:{identity:state.hooks.first.receipt.identity,consumed:[]},profile:hookProfile,message:message.toString('base64'),executionId:state.hooks.first.executionId}}),
   workspace:()=>workspace,prepareRole:task=>roles.prepareRole(task),evidenceCurrent},
  publication:{current:()=>copy({...common,target,workspace:workspace.expected,obligations:observations,
    evidence:Object.fromEntries(Object.entries({author:'shape',validator:'shape-validator',publication:'publication',deliver:'deliver',accept:'accept',secondStage:'pre-commit-hooks'}).map(([key,value])=>[key,item(value)])),
    hooks:hookState(),author,pr:{title:state.title,body:'Implements the authorized Scope in #'+t.number+'.\n\nRefs #'+t.number},
    correlation:pin.invocation.request,source:sourceEvent}),workspace:()=>workspace,authorization,dependencies,dependencyInput,
   history:store.publicationHistory,git:core.createPublicationGit({repository:t.repository,repositoryId:t.repositoryId}),operations},
  ci:{provider,mode:'signed'}};
 let reviewPin,completionPin,mergeIntent,postMergeProof,completionState;
 const observe=()=>core.observeFinalReview({provider,target,head:state.head,base:state.base,ciMode:'signed'});
 if(['ci','shaper-review','merge'].includes(stage)){
  need(state.publication,'publication-required');
  const expected={repository:t.repository,repositoryId:t.repositoryId,prId:target.prId,pr:Number(target.prNumber),head:state.head,
   baseSha:state.base,baseRepositoryId:t.repositoryId,baseBranch:target.baseRef};
  // A pending server check is awaited in this same Job. It never becomes a
  // successful CI receipt, nor causes repeated Writer/Action submissions.
  const until=Date.now()+1200000;let observation;
  do{await checkpoint();observation=await core.observeGithubCI({provider,expected});
   if(observation.status==='satisfied'&&observation.qualifies===true)break;
   need(observation.status==='pending','ci-unavailable-or-failed');await progress({status:'waiting',reason:'exact-head-ci-pending'});
   await new Promise(r=>setTimeout(r,10000));
  }while(Date.now()<until&&!signal?.aborted);
  need(observation.status==='satisfied'&&observation.qualifies===true,'ci-wait-budget');
  const sourceFile=await provider.request({endpoint:'/repos/'+t.repository+'/contents/.github/workflows/ci.yml?ref='+state.head,
   target:t.repositoryId,operation:'native-ci-source',correlation:pin.invocation.request,signal});
  need(sourceFile.outcome==='read'&&sourceFile.data.encoding==='base64'
   &&Buffer.from(sourceFile.data.content,'base64').toString('utf8')===state.policy.ci.workflow.body,'ci-source-drift');
  if(stage==='ci')pin.stageInput={expected,actions:[]};
  if(stage==='shaper-review')reviewPin={...common,target,workspace:workspace.expected,obligations:observations,evidence:reviewEvidence(),
   hooks:hookState(),publication:state.publication,observation:await observe(),method:state.policy.merge.method,ciMode:'signed',
   mergeCorrelation:'merge-'+state.cycle,source:sourceEvent};
  if(stage==='merge'){
   need(state.reviewPin&&state.review,'review-required');reviewPin={...state.reviewPin,review:{value:state.review.evidence,
    current:state.expectations['shaper-review'],context:state.review.context}};
  }
 }
 if(stage==='shaper-review')owners.review={current:()=>copy(reviewPin),workspace:()=>workspace,observe,prepareRole:task=>roles.prepareRole(task),evidenceCurrent};
 if(stage==='merge')owners.merge={current:()=>copy(reviewPin),workspace:()=>workspace,observe,authorization,dependencies,dependencyInput,operations,
  mergeCurrent:async result=>({expected:evidence('merge',{review:c(state.receipts['shaper-review']),head:state.head,base:state.base,
   commit:result.observation.mergeCommit,readback:c(result)}),consumed:[]})};
 if(stage==='merge')mergeIntent=core.controlledIntent({version:1,kind:'merge',correlation:reviewPin.mergeCorrelation,source:reviewPin.source,
  binding:{...bound,runId:reviewPin.source.runId,attempt:reviewPin.source.attempt,baseRef:target.baseRef},
  target:{id:target.prId,number:target.prNumber,workflow:null},payload:{sha:state.head,merge_method:reviewPin.method}});
 const sourceOnly=()=>{
  need(state.scopeBody.includes('Deployment is not required for this source-only change.'),'deployment-applicability-required');
  return {mode:'not-applicable',reason:'The authorized Issue Scope explicitly requires a source-only change without deployment.'};
 };
 async function postMerge(){
  const commit=state.receipts.merge.inputs.commit,path='/repos/'+t.repository+'/commits/'+commit+'/check-runs';
  const result=await provider.collection({endpoint:path+'?filter=latest&per_page=100',envelope:'check_runs',target:t.repositoryId,
   operation:'native-post-merge-ci',correlation:pin.invocation.request,signal,allowPage:p=>p.split('?')[0]===path});
  need(result.outcome==='complete','post-merge-observation');const identities=[];
  for(const name of state.policy.ci.requiredChecks){const found=result.data.filter(x=>x.name===name&&x.head_sha===commit&&state.policy.ci.trustedAppIds.includes(x.app?.id));
   need(found.length===1,'post-merge-check-association');const check=found[0];
   if(check.status!=='completed')return null;
   need(['success','neutral','skipped'].includes(check.conclusion),'post-merge-ci-failed');
   identities.push(c({id:check.id,name,head:commit,app:check.app.id,status:check.status,conclusion:check.conclusion}));
  }
  return {mode:'required',identities:identities.sort(),reason:null};
 }
 if(['final','closure','delivered'].includes(stage)){
  need(state.receipts.merge&&state.mergeIntent,'merge-required');
  const until=Date.now()+1200000;
  do{await checkpoint();postMergeProof=await postMerge();if(postMergeProof)break;
   await progress({status:'waiting',reason:'merged-commit-ci-pending'});await new Promise(r=>setTimeout(r,10000));
  }while(Date.now()<until&&!signal?.aborted);
  need(postMergeProof,'post-merge-ci-wait-budget');const deployment=sourceOnly();
  if(stage==='final')pin.stageInput={policy:deployment};
  else{
   const completed=state.completionPlan;
   if(stage==='delivered')need(completed,'original-completion-plan-required');
   const make=kind=>core.controlledIntent({version:1,kind,correlation:kind+'-'+state.cycle,source:sourceEvent,binding:bound,
    target:{id:t.databaseId,number:t.number,workflow:null},payload:kind==='close'?{state:'closed',state_reason:'completed'}:{labels:['delivered']}});
   completionState=completed??{decision:c({cycle:state.cycle,final:state.receipts.final,writerId:state.member.id}),intents:{close:make('close'),label:make('label')}};
   completionPin={...common,target:{...t,cycle:state.cycle},decision:completionState.decision,writerId:state.member.id,
    candidate:workspace.expected.identity,receipts:Object.fromEntries(core.coreStages.slice(2,12).map(name=>[name,state.receipts[name]])),
    contexts:Object.fromEntries(core.coreStages.slice(2,12).map(name=>[name,current(name)])),intents:completionState.intents,
    source:{runId:source.runId,attempt:source.attempt},mergeIntent:state.mergeIntent,mergeOperation:state.mergeOperation,
    postMerge:{mode:'required'},deployment,deploymentObservation:null,health:null};
   owners.completion={current:()=>copy(completionPin),authorization,dependencies,dependencyInput,provider,operations,
    observe:({signal:caller}={})=>core.observeClosure({provider,target:t,mergeIntent:state.mergeIntent,signal:caller}),
    completionRecords:store.completionRecords,locate:store.locateCompletion,
    stageCurrent:async(name,record)=>{
     if(name==='closure'&&state.receipts.closure)return current('closure');
     const inputs=name==='closure'?{final:c(state.receipts.final),commit:state.receipts.merge.inputs.commit,state:'completed',readback:c(record)}
      :{closure:c(state.receipts.closure),commit:state.receipts.merge.inputs.commit,readback:c(record)};
     return {expected:evidence(name,inputs),consumed:[]};
    },postMerge:async()=>{const proof=await postMerge();need(proof,'post-merge-ci-pending');return proof;}};
  }
 }
 if(stage==='fanout'){
  need(core.distributionAPI?.nativeNotifications===1,'compatible-native-notification-core-required');
  need(state.completion&&state.completionSource,'completion-required');core.verifyCompletion(state.completion,state.completionCurrent);
  const inbox=await nativeWakeupInbox(core,{root});pin.stageInput={source:t,issues:dependencyInput.issues.filter(i=>i.issueId!==t.issueId)};
  owners.fanout={provider,currentSource:async()=>({cycle:state.cycle,identity:state.completion.identity,expected:state.completionCurrent,provenance:state.completionSource}),
   retrieveReceipt:async()=>({cycle:state.cycle,identity:state.completion.identity,bytes:Buffer.from(core.canonicalJSON(state.completion))}),
   currentTarget:async target=>{
    need(dependencyInput.issues.some(i=>core.canonicalJSON(i)===core.canonicalJSON(target)),'known-dependent');
    const r=await provider.request({endpoint:'/repos/'+target.repository,target:target.repositoryId,operation:'native-dependent-observation',correlation:pin.invocation.request,signal});
    need(r.outcome==='read'&&r.data.id===target.repositoryId&&r.data.private===false&&r.data.permissions?.admin===true,'dependent-permission');
    const binding={repository:target.repository,repositoryId:target.repositoryId,issueId:target.issueId,number:target.number};
    return {ok:true,binding,wakeup:{kind:'native-core-wakeup',identity:c({target,member:state.member.id})}};
   },operations:async()=>{throw Error('native-owners:hosted-resolve-not-supported');},notifyTarget:inbox.retain};
 }
 return {owners,workspace,evidence,reviewPin,completionPin,completionState,mergeIntent,postMergeProof,hookInput,author,message,sourceEvent,target};
}

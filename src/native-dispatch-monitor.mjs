import fs from'node:fs/promises';import{watch}from'node:fs';import{join}from'node:path';import{randomUUID}from'node:crypto';
import{readPrivate,writePrivate,ensurePrivateDirectory,sha}from'./local-state.mjs';
import{readNativeOwnerPolicy}from'./native-owner-policy.mjs';import{readNativeMember}from'./native-member.mjs';
import{prepareNativeCycle}from'./native-cycle-policy.mjs';import{openNativeCycle}from'./native-delivery-store.mjs';
import{nativeExecutionInputs}from'./native-execution-inputs.mjs';import{nativeActivationStatus}from'./native-activation.mjs';
import{managedWorkflowPlan}from'./workflow-inputs.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-dispatch:'+code);};
const code=e=>typeof e?.message==='string'&&/^native-[a-z-]+:[a-z-]+$/.test(e.message)?e.message:'native-dispatch:observation-unavailable';

/** One local control-service observer, not a scheduled GitHub Action. GitHub
 * events are read-only hints; only fresh Core authority/dependencies/member
 * selection can dispatch. A native completion/file event wakes it immediately;
 * bounded remote observation also notices Issue and CI changes behind NAT.
 */
export async function nativeDispatchMonitor(core,{root,record,coordinator,context,provider,signal,onProgress=()=>{},intervalMs=30000}={}){
 need(Number.isInteger(intervalMs)&&intervalMs>=1000&&intervalMs<=300000,'interval');
 const configuration=core.validateExecutionPool(record.executionPool),owner=await readNativeOwnerPolicy(core,{root});
 const directory=join(root,'dispatches');await ensurePrivateDirectory(directory);let closed=false,pending=false,running=null,timer,activeWrite=null;
 const findings=new Map();
 function report(key,value){const identity=core.contentIdentity(value);if(findings.get(key)===identity)return;findings.set(key,identity);onProgress(value);}
 const save=async body=>{await writePrivate(join(directory,body.id+'.json'),{...body,identity:core.contentIdentity(body)});return body;};
 async function claims(){const values=[];for(const name of await fs.readdir(directory)){
  need(/^native-[a-f0-9-]{36}\.json$/.test(name),'claim-inventory');const value=await readPrivate(join(directory,name));
  const{identity,...body}=value;need(identity===core.contentIdentity(body)&&name===body.id+'.json','claim-integrity');values.push(body);
 }return values;}
 const get=async(target,endpoint)=>{const r=await provider.request({endpoint,target:target.repositoryId,operation:'native-dispatch-observation',correlation:'native-monitor',signal});
  need(r.outcome==='read','read');return r.data;};
 const list=async(target,endpoint,envelope)=>{
  const r=await provider.collection({endpoint,target:target.repositoryId,operation:'native-dispatch-observation',correlation:'native-monitor',signal,
   ...(envelope?{envelope}:{}),allowPage:p=>p.split('?')[0]===endpoint.split('?')[0]});need(r.outcome==='complete','collection');return r.data;
 };
 // Initial dispatch has explicit local Owner provenance, not a fabricated
 // Actions source Run. Actual Run/Job attribution starts with official readback.
 const writes=core.createGithubProvider({attempts:1,authorize:async request=>{
  const active=activeWrite;need(active&&request.method==='POST'&&request.endpoint===active.endpoint
   &&request.target===active.target.repositoryId&&core.canonicalJSON(request.body)===core.canonicalJSON(active.body),'dispatch-write');
  await active.guard();return !signal?.aborted;
 }});
 async function advance(claim){
  if(claim.phase==='completed')return claim;
  const target=configuration.targets.find(t=>t.repository===claim.repository&&t.repositoryId===claim.repositoryId);need(target,'claim-target');
  const base='/repos/'+target.repository;
  if(!claim.runId){
   const runs=await list(target,base+'/actions/workflows/'+claim.workflowId+'/runs?event=workflow_dispatch&per_page=100','workflow_runs');
   const matching=runs.filter(r=>r.display_title===claim.title&&r.head_sha===claim.sourceCommit&&r.event==='workflow_dispatch');
   need(matching.length<=1,'ambiguous-original-run');if(!matching.length)return claim;
   claim=await save({...claim,phase:'run-observed',runId:matching[0].id});
  }
  const run=await get(target,base+'/actions/runs/'+claim.runId);
  need(run.id===claim.runId&&run.run_attempt===1&&run.repository?.id===target.repositoryId&&run.head_repository?.id===target.repositoryId
   &&run.display_title===claim.title&&run.head_sha===claim.sourceCommit&&run.path==='.github/workflows/nibfin-stage.yml'
   &&run.actor?.id===owner.memberId&&run.triggering_actor?.id===owner.memberId,'run-binding');
  if(run.status==='completed')return save({...claim,phase:'completed',conclusion:run.conclusion});
  if(claim.phase!=='enqueued'){
   const jobs=await list(target,base+'/actions/runs/'+run.id+'/attempts/1/jobs?per_page=100','jobs');
   const matching=jobs.filter(j=>j.status==='queued'&&j.name==='stage / execute'&&j.labels?.includes('nibfin-request-'+claim.id));
   need(matching.length<=1,'ambiguous-original-job');
   if(matching.length){await coordinator.enqueue({repository:target.repository,runId:run.id,jobId:matching[0].id});claim=await save({...claim,phase:'enqueued',jobId:matching[0].id});}
  }
  return claim;
 }
 async function tick(){
  if((await nativeActivationStatus(core,{root})).status!=='ready')return;
  await readNativeMember(core,{root,memberId:owner.memberId});const pool=await coordinator.status();need(pool.host==='running','pool-unavailable');
  const previous=[];for(const claim of await claims()){
   try{previous.push(await advance(claim));}catch(e){previous.push(claim);report(claim.id,{status:'blocked',issue:claim.issue,stage:claim.stage,reason:code(e)});}
  }
  let available=Math.max(0,configuration.capacity-previous.filter(c=>c.phase!=='completed').length);
  for(const target of configuration.targets){
   const issues=await list(target,'/repos/'+target.repository+'/issues?state=all&labels=scope&assignee='+encodeURIComponent(owner.memberLogin)+'&per_page=100');
   for(const issue of issues){if(closed||signal?.aborted)return;if(issue.pull_request)continue;
    const number=String(issue.number),key=target.repositoryId+':'+number;
    try{
     const selected={repository:target.repository,repositoryId:target.repositoryId,issueId:issue.node_id,databaseId:issue.id,number};
     let store;
     try{store=await openNativeCycle(core,{root,target:selected});}catch(e){if(e.code!=='ENOENT')throw e;if(issue.state==='closed')continue;}
     let state=store?await store.read():null;
     if(state?.fanout?.outcome==='discovered'&&state.completion)continue;
     if(state?.dispatch){
      const claim=previous.find(c=>c.id===state.dispatch.id);need(claim,'original-dispatch-missing');
      if(claim.phase!=='completed')continue;
      need(claim.conclusion==='success','original-action-failed');
     }
     if(!available)continue;
     store=await prepareNativeCycle(core,{root,configuration,repository:target.repository,number,provider,context,signal});state=await store.read();
     for(const[name,receipt]of Object.entries(state.receipts))core.compareEvidence(receipt,state.expectations[name]);
     const stage=core.nextDeliveryStage(state.receipts,null);need(!state.attempts[stage],'original-stage-reconciliation-required');
     const id='native-'+randomUUID();
     const checkpoint=async()=>{need(!signal?.aborted&&(await nativeActivationStatus(core,{root})).status==='ready','owner-restriction');};
     const inputs=await nativeExecutionInputs(core,{root,store,configuration,binding:{request:{id}},provider,signal,checkpoint,
      poolReadiness:async request=>{await readNativeMember(core,{root,memberId:request.memberId});const current=await coordinator.status();
       need(current.pool===core.contentIdentity(configuration)&&current.host==='running','prepared-pool');
       return {memberId:owner.memberId,pool:current.pool,status:'ready',observedAt:Date.now()};}});
     const deps=await inputs.dependencies.evaluate(inputs.dependencyInput,{signal});need(deps.outcome==='ready','dependencies-pending');
     const selection=await inputs.selector.select(inputs.selectionInput(stage),{signal});need(selection.outcome==='selected'&&selection.memberId===owner.memberId,'member-selection');
     const base='/repos/'+target.repository,workflow=await get(target,base+'/actions/workflows/nibfin-stage.yml'),head=(await get(target,base+'/git/ref/heads/main')).object.sha;
     need(workflow.path==='.github/workflows/nibfin-stage.yml'&&workflow.state==='active'&&/^[a-f0-9]{40}$/.test(head),'workflow');
     const source=await get(target,base+'/contents/.github/workflows/nibfin-stage.yml?ref='+head);
     const expected=managedWorkflowPlan(core,{executionPool:configuration,repository:target.repository}).files.get('.github/workflows/nibfin-stage.yml');
     need(source.encoding==='base64'&&Buffer.from(source.content,'base64').equals(Buffer.from(expected)),'workflow-source');
     const body={ref:'main',inputs:{nibfin_request:id,nibfin_correlation:id,nibfin_issue:number,nibfin_stage:stage,nibfin_member:owner.memberId}};
     const endpoint=base+'/actions/workflows/'+workflow.id+'/dispatches';
     const claim={version:1,id,provenance:{kind:'native-owner-dispatch',memberId:owner.memberId},repository:target.repository,repositoryId:target.repositoryId,
      issue:number,cycle:state.cycle,stage,workflowId:workflow.id,sourceCommit:head,issueBodySha256:sha(issue.body),
      title:core.actionRunName({issue:number,stage,correlation:id}),createdAt:Date.now(),phase:'dispatch-intent'};
     const guard=async()=>{
      await checkpoint();const latest=await get(target,base+'/issues/'+number),cycle=await store.read();
      need(sha(latest.body)===claim.issueBodySha256&&core.nextDeliveryStage(cycle.receipts,null)===stage,'fresh-scope-required');
      need((await inputs.authorization.eligibility({operation:'dispatch'},{signal})).ok===true,'dispatch-authorization');
      need((await inputs.dependencies.evaluate(inputs.dependencyInput,{signal})).outcome==='ready','dependencies-pending');
      need((await get(target,base+'/git/ref/heads/main')).object.sha===head,'workflow-ref-changed');
     };
     await guard();await save(claim);await store.update(s=>{need(!s.dispatch||s.dispatch.id===state.dispatch?.id,'dispatch-conflict');s.dispatch={id,stage};return s;});
     activeWrite={target,endpoint,body,guard};
     try{await writes.request({method:'POST',endpoint,target:target.repositoryId,operation:'installation',correlation:id,body,signal});}
     finally{activeWrite=null;}
     available--;await advance(claim);report(key,{status:'queued',issue:number,stage,reason:'native-owner-dispatch-retained'});
    }catch(e){report(key,{status:'blocked',issue:number,reason:code(e)});}
   }
  }
 }
 function wake(){if(closed)return;pending=true;if(!running)running=(async()=>{while(pending&&!closed){pending=false;try{await tick();}catch(e){report('observer',{status:'blocked',reason:code(e)});}}})().finally(()=>{running=null;});}
 const watcher=watch(root,(_event,name)=>{if(name===null||name.toString()==='activation.json')wake();});watcher.on('error',()=>report('observer',{status:'blocked',reason:'native-dispatch:subscription-unavailable'}));
 function next(){if(closed)return;timer=setTimeout(()=>{wake();next();},intervalMs);timer.unref();}
 next();wake();
 const stop=()=>{closed=true;clearTimeout(timer);watcher.close();};signal?.addEventListener('abort',stop,{once:true});
 return Object.freeze({wake,async settled(){await running;},async close(){stop();signal?.removeEventListener('abort',stop);await running;}});
}

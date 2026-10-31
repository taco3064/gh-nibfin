import test from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{createHash}from'node:crypto';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';
import{prepareNativeCycle}from'../../src/native-cycle-policy.mjs';import{initializeFileHostStore,openFileHostStore}from'../../src/host-store.mjs';
import{writePrivate,ensurePrivateDirectory}from'../../src/local-state.mjs';import{managedWorkflowPlan}from'../../src/workflow-inputs.mjs';
import{nativePoolStore}from'../../src/native-pool-store.mjs';import{nativeRescue}from'../../src/native-rescue.mjs';
import{restoreUnacceptedWorkspace,retainUnacceptedWorkspace}from'../../src/native-unaccepted-work.mjs';
import{retainedFinalMessages}from'../../src/native-rescue-agent.mjs';

test('diagnosis sees retained completed Shape envelopes with sanitized summaries and no transcript or body',async()=>withCurrentCore(async core=>{
 const body='The substantive Shape body must remain private',summary='Collected Shape; access_token=private-secret ::error::';
 const events=[{type:'response_item',payload:{type:'function_call_output',output:'private tool transcript'}},
  {type:'event_msg',timestamp:'2026-10-30T17:09:13Z',payload:{type:'task_complete',last_agent_message:JSON.stringify({body,result:{outcome:'ready',summary}})}},
  {type:'event_msg',payload:{type:'task_complete',last_agent_message:'not JSON'}},
  {type:'event_msg',payload:{type:'task_complete',last_agent_message:'null'}}];
 const result=retainedFinalMessages(core,{files:[{bytes:Buffer.from(events.map(e=>JSON.stringify(e)).join('\n')).toString('base64')}]});
 assert.equal(result.length,1);assert.equal(result[0].outcome,'ready');assert.equal(result[0].shapeEnvelope,true);assert.equal(result[0].bodyLength,body.length);
 assert.match(result[0].summary,/REDACTED/);assert.ok(!JSON.stringify(result).includes('private-secret'));assert.ok(!JSON.stringify(result).includes(body));assert.ok(!JSON.stringify(result).includes('private tool transcript'));
}));

for(const role of ['shape','deliver'])test('native '+role+' recovery preserves the original unaccepted work and uses only its reserved continuation',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),store=await prepareNativeCycle(core,f.options),s=await store.read();
 const request='native-11111111-1111-4111-8111-111111111111',execution='role-original',sessionId='11111111-1111-4111-8111-111111111111';
 const original=join(context.root,'original-session');await fs.mkdir(original,{mode:0o700});
 await fs.writeFile(join(original,sessionId+'.jsonl'),JSON.stringify({type:'session_meta',payload:{id:sessionId}})+'\n',{mode:0o600});
 const metadata={id:sessionId,memberId:'7',role:role,execution,adapter:'codex'},snapshot=core.captureRescueSession({directory:original,memberId:'7',role:role,session:sessionId,execution});
 const sessionReference=await store.retain({kind:'native-role-session',metadata,snapshot});
 await store.retain({kind:'native-role-request',input:{execution:{id:execution,role:role,source:{runId:'1000',attempt:'1'}}}});
 if(role==='deliver'){const writer=join(context.root,'original-writer');await fs.mkdir(writer,{mode:0o700});await fs.writeFile(join(writer,'draft.txt'),'unfinished original edits',{mode:0o600});await retainUnacceptedWorkspace({directory:writer,execution:{id:execution},retain:v=>store.retain(v)});}
 await store.update(v=>{v.attempts[role]={request,phase:'unresolved',reason:'owner-result'};v.dispatch={id:request,stage:role};if(role==='deliver')v.shapeBody=core.formatRequirementsDocument({scope:v.scopeBody,shape:'Original approved implementation plan'});return v;});
 const installation={executionPool:f.configuration};await writePrivate(join(f.root,'native-installation.json'),installation);
 const activation={version:1,phase:'active',installation:core.contentIdentity(installation)};await writePrivate(join(f.root,'activation.json'),{...activation,identity:core.contentIdentity(activation)});
 const host=await initializeFileHostStore(core,{directory:join(f.root,'state'),installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins},repositories:[{repository:'example/project',repositoryId:'100'}]});
 try{const before=await host.read();await host.updateWork({expected:before.identity,work:{...before.work,paused:false,reconciled:false,
  pending:[{kind:'native-stage',cycle:s.cycle,request,stage:role,status:'UNACCEPTED'}],conversations:[{reference:sessionReference,session:sessionId}]}});}finally{await host.close();}
 const pool=nativePoolStore(core,{root:f.root,configuration:f.configuration});await pool.initialize();
 const released={request:{id:request},phase:'released',runnerId:'21'};
 await pool.jobs.write({id:request,expected:null,value:{...released,identity:core.contentIdentity(released)}});
 const base='/repos/example/project',head='b'.repeat(40),files=managedWorkflowPlan(core,{executionPool:f.configuration,repository:'example/project'}).files;
 const blob=(path)=>createHash('sha1').update('blob '+Buffer.byteLength(files.get(path))+'\0').update(files.get(path)).digest('hex');
 const source={id:'1000',run_attempt:1,repository:f.repo,head_repository:f.repo,path:'.github/workflows/nibfin-stage.yml',event:'workflow_dispatch',head_branch:'main',head_sha:head,
  actor:{id:'7'},triggering_actor:{id:'7'},status:'completed',conclusion:'failure'};
 const dispatched=[],writes=[];let diagnoses=0,slowGuard=false,guardsDelayed=0,delayedRunList=false,visibleAfter=0,observationChurn=0;
 const reply=data=>({status:200,body:JSON.stringify(data)});
 f.intercept(async r=>{
  if(r.endpoint===base+'/issues/55'){
   const current=await store.read();
   if(Object.values(current.nativeRescueContinuations??{}).some(c=>c.status==='prepared'))
    await store.retain({kind:'native-recovery-observation',sequence:++observationChurn});
  }
  if(r.endpoint.includes('/contents/.github/workflows/nibfin-')){const path=r.endpoint.split('/contents/')[1].split('?')[0],text=files.get(path);return reply({type:'file',path,sha:blob(path),encoding:'base64',content:Buffer.from(text).toString('base64'),size:Buffer.byteLength(text)});}
  if(r.endpoint===base+'/commits/main')return reply({sha:head});
  if(r.endpoint.includes('/actions/workflows/')&&!r.endpoint.endsWith('/dispatches')&&!r.endpoint.includes('/runs?')){const rescue=r.endpoint.includes('rescue');return reply({id:rescue?'901':'900',state:'active',path:'.github/workflows/nibfin-'+(rescue?'rescue':'stage')+'.yml'});}
  if(r.endpoint===base+'/actions/runs/1000/attempts/1'){
   // A real original-source guard can outlive the provider's single-request
   // budget. Exercise the installed operation owner, not a timer/config mock.
   if(slowGuard&&guardsDelayed<2){guardsDelayed++;await new Promise(resolve=>setTimeout(resolve,16000));}
   return reply(source);
  }
  if(r.endpoint===base+'/actions/runs/1000/attempts/1/jobs?per_page=100')return reply({jobs:[{id:'5000',name:'stage / execute',status:'completed'}]});
  if(/\/actions\/runs\/6[0-9]+\/attempts\/1\/jobs\?/.test(r.endpoint))return reply({jobs:[{id:'5001',name:'stage / execute',status:'queued'}]});
  if(r.method==='POST'&&r.endpoint.endsWith('/dispatches')){
   const body=JSON.parse(r.body),rescue=r.endpoint.includes('/901/'),id=String(6000+dispatched.length);
   const run={...source,id,workflow_id:rescue?'901':'900',path:'.github/workflows/nibfin-'+(rescue?'rescue':'stage')+'.yml',status:'queued',conclusion:null,
    display_title:core.actionRunName({issue:'55',stage:role,correlation:body.inputs.nibfin_correlation,...(rescue?{rescue:true,mode:body.inputs.mode}:{})})};
   if(role==='deliver'&&dispatched.length===0)visibleAfter=Date.now()+15000;
   dispatched.push({run,body});writes.push(r);return{status:204,body:''};
  }
  if(r.endpoint.includes('/actions/workflows/')&&r.endpoint.includes('/runs?')){const workflow=r.endpoint.split('/workflows/')[1].split('/')[0];const runs=dispatched.filter(d=>d.run.workflow_id===workflow).map(d=>d.run);
   if(role==='deliver'&&runs.length===1&&Date.now()<visibleAfter){delayedRunList=true;return reply({total_count:0,workflow_runs:[]});}
   return reply({total_count:runs.length,workflow_runs:runs});}
  if(r.endpoint.includes('/actions/runs/')){const id=r.endpoint.split('/actions/runs/')[1].split('/')[0],run=dispatched.find(d=>d.run.id===id)?.run;if(run)return reply(run);}
 });
 const executionCore={...core,createControlledOperations:options=>{if(role==='shape')slowGuard=true;return core.createControlledOperations(options);}};
 const rescue=await nativeRescue(executionCore,{root:f.root,configuration:f.configuration,provider:f.provider,transport:f.transport,
  agent:async(_core,{target,caseId})=>({capabilities:{rescueReadOnly:true},diagnose:async()=>{diagnoses++;
   const h=await openFileHostStore(core,{directory:join(f.root,'state')});try{
    const records=h.rescue('100'),before=(await records.read()).cases[caseId];
    await records.update(v=>{v.cases[caseId].nextCheckAt=Date.now()-1;return v;});
    await rescue.refresh();assert.equal((await records.read()).cases[caseId].checks,before.checks,'background recovery cannot enter an executing diagnostic binding');
   }finally{await h.close();}
   return{execution:'diagnosis-new',terminated:true,memberId:'7',adapter:'codex',result:{outcome:'ready',failureClass:'implementation_defect',
   body:JSON.stringify({action:'retry-original-role',steps:['Resume the original Shape conversation using the corrected completed-message decoder']}),rootCause:'The previous parser discarded the valid Shape envelope',summary:'diagnosed',evidenceReferences:[caseId],contractConflicts:[]}};},close:async()=>{}})});
 try{
  const id=await rescue.observe({store,claim:{id:request,runId:'1000',stage:role,sourceCommit:head}});
  const observer=await openFileHostStore(core,{directory:join(f.root,'state')});let cases;
  try{cases=(await observer.rescue('100').read()).cases;}finally{await observer.close();}
  assert.equal(writes.length,1,JSON.stringify(cases[id]));assert.equal(diagnoses,0);
  if(role==='deliver')assert.equal(delayedRunList,true,'an acknowledged notification absent from the first Run list is read back without another POST');
  if(role==='shape')assert.equal(guardsDelayed,2,'the notification survives serial fresh guards totaling more than thirty seconds');
  const observedRevision=(await store.read()).revision;
  assert.equal(await rescue.observe({store,claim:{id:request,runId:'1000',stage:role,sourceCommit:head}}),id);
  assert.equal((await store.read()).revision,observedRevision,'duplicate blocked observations do not mutate the original cycle');
  assert.equal(writes.length,1,'the same stopped execution does not queue another notification');
  const notification=dispatched[0];assert.equal(notification.body.inputs.request_id,id);
  await assert.rejects(rescue.execute({request:{id:notification.body.inputs.nibfin_correlation,repository:'example/project',runId:'9999'},correlation:notification.body.inputs.nibfin_correlation,
   issue:'55',stage:role,jobName:'rescue',mode:'repair',jobRecord:{lease:{identity:'a'.repeat(64)}}}),/actual-rescue-invocation/);
  assert.equal(diagnoses,0,'a different Run cannot borrow the retained notification');
  const result=await rescue.execute({request:{id:notification.body.inputs.nibfin_correlation,repository:'example/project',runId:notification.run.id},correlation:notification.body.inputs.nibfin_correlation,
   issue:'55',stage:role,jobName:'rescue',mode:'repair',jobRecord:{lease:{identity:'a'.repeat(64)}}});
  assert.equal(result.rescue.outcome,'PENDING_VERIFICATION',JSON.stringify(result));assert.equal(diagnoses,1);assert.equal(writes.length,2);
  assert.ok(observationChurn>0,'unrelated retained observations occur during the guarded continuation');
  const after=await store.read(),resume=after.sessionResumes[role];
  if(role==='deliver'){assert.ok(resume.workspaceReference);const original=await store.get(resume.workspaceReference);assert.equal(original.status,'UNACCEPTED');assert.equal(after.candidate,null);}
  assert.equal(resume.reference,sessionReference);assert.equal(resume.next.session,sessionId);assert.notEqual(resume.next.execution,execution);
  assert.equal(dispatched[1].body.inputs.nibfin_member,'7');assert.equal(dispatched[1].body.inputs.nibfin_request,resume.next.request);
  assert.equal(after.attempts[role],undefined);assert.equal(after.receipts[role],undefined);assert.equal(after.recoveries[0].attempt.request,request);
  const h=await openFileHostStore(core,{directory:join(f.root,'state')});
  try{await h.rescue('100').update(v=>{v.cases[id].outcome='UNRESOLVED';v.cases[id].code='UNKNOWN_DISPATCH';v.cases[id].nextCheckAt=null;return v;});}finally{await h.close();}
  await rescue.refresh();const fresh=await openFileHostStore(core,{directory:join(f.root,'state')});
  try{const c=(await fresh.rescue('100').read()).cases[id];assert.equal(c.outcome,'DEFERRED');assert.equal(c.code,'CONTINUATION_ACTIVE');}finally{await fresh.close();}
  assert.equal(writes.length,2,'independent verification of an existing continuation never re-dispatches it');
  assert.equal((await store.read()).receipts[role],undefined,'a reporting error cannot create role acceptance');
 }finally{await rescue.close();}
}));

test('unaccepted Writer restore validates the complete snapshot before copying and preserves executable files',async()=>withCurrentCore(async(core,context)=>{
 const directory=join(context.root,'writer-restore');await fs.mkdir(directory,{mode:0o700});
 const bytes=Buffer.from('unfinished original edits');const entry={path:'draft.txt',kind:'file',mode:0o700,data:bytes.toString('base64'),sha256:createHash('sha256').update(bytes).digest('hex')};
 const snapshot={kind:'native-unaccepted-workspace',status:'UNACCEPTED',execution:{id:'original'},bytes:bytes.length,entries:[entry]};
 await assert.rejects(restoreUnacceptedWorkspace({directory,snapshot:{...snapshot,entries:[entry,{...entry,path:'../escape'}]},execution:'original'}),/restore-path/);
 assert.deepEqual(await fs.readdir(directory),[]);
 await restoreUnacceptedWorkspace({directory,snapshot,execution:'original'});assert.equal(await fs.readFile(join(directory,'draft.txt'),'utf8'),bytes.toString());assert.equal((await fs.stat(join(directory,'draft.txt'))).mode&0o777,0o700);
 await assert.rejects(restoreUnacceptedWorkspace({directory,snapshot,execution:'original'}),/restore-not-empty/);
}));

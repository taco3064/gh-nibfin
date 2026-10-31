import test from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{randomUUID}from'node:crypto';import{execFileSync}from'node:child_process';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';import{prepareNativeCycle}from'../../src/native-cycle-policy.mjs';
import{nativeShapeFeedback}from'../../src/native-shape-feedback.mjs';import{observeNativeObligations}from'../../src/native-obligations.mjs';
import{initializeFileHostStore}from'../../src/host-store.mjs';import{nativePoolStore}from'../../src/native-pool-store.mjs';

test('unpublished machine-format feedback retains both original sessions and requires fresh Author and independent validation',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),store=await prepareNativeCycle(core,f.options),s=await store.read(),claim={id:'native-11111111-1111-4111-8111-111111111111',stage:'shape-validator',runId:'3001'};
 const body=core.formatRequirementsDocument({scope:s.scopeBody,shape:'Tracked Markdown documentation.\n```nibfin-candidate\n{"artifacts":[],"hooks":[]}\n```\n`artifacts` is empty because Scope needs no extra files.\n`hooks` is empty based on the inspected repository setup.'});
 assert.equal(core.parseCandidateObligations(body).ok,false,'the active old pin exposes the real legacy grammar mismatch');
 const authority={authority:s.authority.identity,grant:core.contentIdentity(s.grant),policy:core.contentIdentity(s.policy),core:s.core,runtime:s.policy.runtime,repositoryId:s.target.repositoryId,issueId:s.target.issueId,scopeHash:s.scopeHash,shapeHash:null};
 const native=core.createNativeRoleBindings({ownership:'personal',retain:v=>store.retain(v),source:{repositoryId:'100',workflow:'a'.repeat(64),runId:'3000',attempt:'1'}});
 const instructions=core.selectInstructions({skill:'shape',role:'shape'}),a=await native.bind({role:'shape',skill:'shape',instructions,task:{authority,original:{kind:'raw-scope',body:s.scopeBody},requirementsDocument:{format:'scope-shape-v1',scope:s.scopeBody,frame:core.requirementsDocumentFrame(s.scopeBody)}}});
 const authorExpected={...a.current().expected,inputs:{body:core.bodyDigest(body)}},receipt=core.createEvidence({expected:authorExpected,consumed:[]});
 const validatorExecution={id:'independent-validator',role:'shape-validator',source:{runId:claim.runId,attempt:'1'}},references={};
 for(const[role,execution]of[['shape',a.input.execution.id],['shape-validator',validatorExecution.id]]){
  const directory=join(context.root,'session-'+role);await fs.mkdir(directory,{mode:0o700});const id=randomUUID();await fs.writeFile(join(directory,id+'.jsonl'),JSON.stringify({type:'session_meta',payload:{id}})+'\n',{mode:0o600});
  const metadata={id,memberId:'7',role,execution,adapter:'codex'},snapshot=core.captureRescueSession({directory,memberId:'7',role,session:id,execution});references[role]=await store.retain({kind:'native-role-session',metadata,snapshot});
 }
 await store.retain({kind:'role-collection',execution:validatorExecution,exitCode:0,evidence:{result:{outcome:'approved'}}});
 await store.retain({kind:'native-core-result',result:{request:claim.id,owner:{output:{outcome:'blocked',finding:'shape:obligations'}}}});
 const directory=join(context.root,'observed-source');await fs.mkdir(directory,{mode:0o700});execFileSync('/usr/bin/git',['init','--template='],{cwd:directory,stdio:'ignore'});await fs.writeFile(join(directory,'README.md'),'source');execFileSync('/usr/bin/git',['add','README.md'],{cwd:directory});
 await store.retain({kind:'native-source-obligations',observations:await observeNativeObligations(core,{directory,scopeBody:s.scopeBody,base:s.base,repository:s.target.repository})});
 await store.update(v=>{v.shapeBody=body;v.receipts.shape=receipt;v.expectations.shape={expected:authorExpected,consumed:[]};v.attempts={shape:{phase:'completed'},'shape-validator':{request:claim.id,phase:'unresolved'}};v.dispatch={id:claim.id,stage:'shape-validator'};return v;});
 const host=await initializeFileHostStore(core,{directory:join(f.root,'state'),installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins},repositories:[{repository:'example/project',repositoryId:'100'}]});
 const foreign={kind:'native-stage',cycle:'foreign',request:'other-work',stage:'deliver',status:'UNACCEPTED'};
 try{
  const before=await host.read();await host.updateWork({expected:before.identity,work:{...before.work,paused:false,pending:[foreign,{kind:'native-stage',cycle:s.cycle,request:claim.id,stage:'shape-validator',status:'UNACCEPTED'}],reconciled:false}});
  const pool=nativePoolStore(core,{root:f.root,configuration:f.configuration});await pool.initialize();const job={request:{id:claim.id,repository:s.target.repository,runId:claim.runId},issue:s.target.number,phase:'released'};await pool.jobs.write({id:claim.id,expected:null,value:{...job,identity:core.contentIdentity(job)}});
  const originalScope=f.issue.body;f.issue.body+='\nChanged Scope';await assert.rejects(nativeShapeFeedback(core,{root:f.root,store,claim,provider:f.provider}),/current-scope/);f.issue.body=originalScope;
  assert.equal((await store.read()).shapeBody,body,'a Scope change cannot authorize proposal revision');
  const result=await nativeShapeFeedback(core,{root:f.root,store,claim,provider:f.provider});assert.equal(result.outcome,'revision-prepared');
  const after=await store.read(),archived=await store.get(result.reference);assert.equal(archived.proposal,body);assert.deepEqual(archived.receipts.shape,receipt);assert.equal(after.scopeBody,s.scopeBody);assert.equal(after.scopeHash,s.scopeHash);
  assert.deepEqual(after.receipts,{});assert.equal(core.nextDeliveryStage(after.receipts),'shape');assert.equal(after.shapeBody,null);assert.equal(after.dispatch,undefined);
  assert.equal(after.sessionResumes.shape.reference,references.shape);assert.equal(after.sessionResumes['shape-validator'].reference,references['shape-validator']);assert.equal(after.sessionResumes.shape.next,undefined,'fresh normal executions are still required');
  assert.deepEqual((await host.read()).work.pending,[foreign],'another project work item is preserved');
  assert.equal((await nativeShapeFeedback(core,{root:f.root,store,claim,provider:f.provider})).outcome,'not-applicable','the same rejected proposal is not revised twice');
  await store.update(v=>{v.shapeRevision.phase='prepared';return v;});
  assert.equal((await nativeShapeFeedback(core,{root:f.root,store,claim:{},provider:f.provider})).id,result.id,'interrupted work-release journal is idempotently completed');
  assert.equal((await store.read()).shapeRevisions.length,1);assert.deepEqual((await host.read()).work.pending,[foreign]);
 }finally{await host.close();}
}));

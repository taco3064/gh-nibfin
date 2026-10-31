import test from'node:test';import assert from'node:assert/strict';import{join}from'node:path';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';
import{initializeFileHostStore}from'../../src/host-store.mjs';import{writePrivate}from'../../src/local-state.mjs';
import{nativeDispatchMonitor}from'../../src/native-dispatch-monitor.mjs';import{managedWorkflowPlan}from'../../src/workflow-inputs.mjs';
import fs from'node:fs/promises';

for(const scenario of ['normal','pre-send-restart','unknown-send'])test('native dispatch original write boundary: '+scenario,async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),installation={executionPool:f.configuration};await writePrivate(join(f.root,'native-installation.json'),installation);
 const activation={version:1,phase:'active',installation:core.contentIdentity(installation)};await writePrivate(join(f.root,'activation.json'),{...activation,identity:core.contentIdentity(activation)});
 const store=await initializeFileHostStore(core,{directory:join(f.root,'state'),installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins},repositories:[{repository:'example/project',repositoryId:'100'}]});
 try{const s=await store.read();await store.updateWork({expected:s.identity,work:{...s.work,paused:false}});}finally{await store.close();}
 let body,runVisible=false,failed=false;const writes=[],enqueued=[],progress=[];const base='/repos/example/project',workflow=managedWorkflowPlan(core,{executionPool:f.configuration,repository:'example/project'}).files.get('.github/workflows/nibfin-stage.yml');
 const reply=data=>({status:200,body:JSON.stringify(data)});
 const run=()=>({id:'4000',run_attempt:1,repository:{id:'100'},head_repository:{id:'100'},display_title:core.actionRunName({issue:'55',stage:'shape',correlation:body.inputs.nibfin_correlation}),head_sha:'b'.repeat(40),event:'workflow_dispatch',path:'.github/workflows/nibfin-stage.yml',actor:{id:'7'},triggering_actor:{id:'7'},status:failed?'completed':'queued',conclusion:failed?'failure':null});
 f.intercept(async r=>{
  if(r.endpoint.startsWith(base+'/issues?'))return reply([f.issue]);
  if(r.endpoint===base+'/actions/workflows/nibfin-stage.yml')return reply({id:'900',path:'.github/workflows/nibfin-stage.yml',state:'active'});
  if(r.endpoint.startsWith(base+'/contents/.github/workflows/nibfin-stage.yml'))return reply({encoding:'base64',content:Buffer.from(workflow).toString('base64')});
  if(r.endpoint===base+'/actions/workflows/900/dispatches'){writes.push(r);body=JSON.parse(r.body);if(scenario==='unknown-send')throw Error('response lost');return{status:204,body:''};}
  if(r.endpoint.startsWith(base+'/actions/workflows/900/runs?'))return reply({workflow_runs:runVisible?[run()]:[]});
  if(r.endpoint===base+'/actions/runs/4000')return reply(run());
  if(r.endpoint.startsWith(base+'/actions/runs/4000/attempts/1/jobs?'))return reply({jobs:[{id:'5000',status:'queued',name:'stage / execute',labels:['self-hosted','linux','x64','nibfin-request-'+body.inputs.nibfin_request]}]});
 });
 const coordinator={status:async()=>({host:'running',pool:core.contentIdentity(f.configuration)}),enqueue:async value=>enqueued.push(value)};
 let interrupted=false;const transport=async r=>f.transport(r);
 Object.defineProperty(transport,'prepareMutation',{value:async()=>{if(scenario==='pre-send-restart'&&!interrupted){interrupted=true;throw Error('stopped before write');}return transport;}});
 const options={root:f.root,record:installation,coordinator,context,provider:f.provider,transport,intervalMs:300000,onProgress:value=>progress.push(value)};
 let monitor=await nativeDispatchMonitor(core,options);
 try{
  if(scenario==='pre-send-restart'){
   await monitor.settled();assert.equal(writes.length,0);const names=await fs.readdir(join(f.root,'dispatches'));assert.equal(names.length,1);
   const original=JSON.parse(await fs.readFile(join(f.root,'dispatches',names[0]),'utf8'));assert.equal(original.phase,'dispatch-prepared');
   await monitor.close();monitor=await nativeDispatchMonitor(core,options);await monitor.settled();assert.equal(writes.length,1);assert.equal(body.inputs.nibfin_request,original.id);
  }
  await monitor.settled();assert.equal(writes.length,1,JSON.stringify(progress));assert.equal(enqueued.length,0);
  monitor.wake();await monitor.settled();assert.equal(writes.length,1);
  if(scenario==='unknown-send')assert.ok(progress.some(p=>p.reason==='native-dispatch:original-submission-unknown'));
  runVisible=true;monitor.wake();await monitor.settled();assert.deepEqual(enqueued,[{repository:'example/project',runId:'4000',jobId:'5000'}]);
  monitor.wake();await monitor.settled();assert.equal(enqueued.length,1);assert.equal(writes.length,1);
  failed=true;monitor.wake();await monitor.settled();assert.equal(writes.length,1);assert.ok(progress.some(p=>p.reason==='native-dispatch:original-action-failed'));
 }finally{await monitor.close();}
}));

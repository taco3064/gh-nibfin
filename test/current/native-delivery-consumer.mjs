import test from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{execFileSync}from'node:child_process';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';
import{executeNativeDelivery}from'../../src/native-delivery.mjs';import{initializeFileHostStore}from'../../src/host-store.mjs';
import{openNativeCycle}from'../../src/native-delivery-store.mjs';

test('native composition runs actual Shape, independent Validator and controlled publication owners across separate retained source Runs',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),host=await initializeFileHostStore(core,{directory:join(f.root,'state'),
  installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins},repositories:[{repository:'example/project',repositoryId:'100'}]});
 const before=await host.read();await host.updateWork({expected:before.identity,work:{...before.work,paused:false}});await host.close();
 const sourceDirectory=join(f.root,'fixture-source');await fs.mkdir(sourceDirectory,{mode:0o700});
 execFileSync('/usr/bin/git',['init','--template='],{cwd:sourceDirectory});await fs.writeFile(join(sourceDirectory,'README.md'),'original source');
 execFileSync('/usr/bin/git',['add','README.md'],{cwd:sourceDirectory});
 const original=f.issue.body,body=core.formatRequirementsDocument({scope:original,shape:[
  'Document the installed native Owner and Member commands in README.md.',
  'Candidate artifacts applicability: The Scope explicitly requires no additional ignored or untracked artifacts.',
  'Candidate hooks applicability: The inspected source has no applicable Hook setup.',
  '```nibfin-candidate','{"artifacts":[],"hooks":[]}','```'].join('\n')});
 const prepared=[],closed=[],progress=[];let preparationError=null;
 // Model/process doubles only; real native isolation is established separately.
 const roleFactory=async(_core,{source,retain,binding})=>{
  const metadata=core.createNativeRoleBindings({ownership:'personal',source,retain}),values=new Map();
  return{sourceDirectory,candidateDirectory:sourceDirectory,workspace:()=>sourceDirectory,
   async prepareRole(task){try{
    const bound=await metadata.bind(task),expected=bound.current().expected;
    if(task.role==='shape')expected.inputs={body:core.bodyDigest(body)};
    const current=()=>({expected:structuredClone(expected),consumed:[]});
    const backend={readiness:async()=>({login:'available',tool:'available',isolation:'verified',read:'verified',write:'unsupported',evidence:['recording-only'],limitations:[]}),
     start:async()=>{prepared.push({role:task.role,run:source.runId,id:bound.input.execution.id});return{status:'started'};},
     collect:async()=>({status:'complete',exitCode:0,evidence:structuredClone(expected)}),termination:async()=>({status:'terminated'}),
     stop:async()=>({status:'requested'}),cleanup:async()=>({status:'completed',residuals:[]})};
    const id='fixture-'+binding.request.id,lifecycle=core.createAgentLifecycle({environment:{id:id+'-environment',agent:'recording',loginSpace:'/fixture/login',instances:[
     {id,workspace:'/fixture/work-'+id,registration:'/fixture/reg-'+id,service:'/fixture/service-'+id}]},instanceId:id,backend,current,
     startCurrent:bound.startCurrent});
    const value={input:bound.input,lifecycle,current,readBody:()=>body,taskIdentity:core.contentIdentity(task.task)};
    values.set(bound.input.execution.id,value);return value;}catch(e){preparationError=e.stack;throw e;}
   },get:id=>{assert.ok(values.has(id));return values.get(id);},async close(){closed.push(binding.request.id);}}
 };
 for(const [i,stage]of['shape','shape-validator','publication'].entries()){
  const binding={configuration:f.configuration,ownerType:'User',memberId:'7',issue:'55',stage,correlation:'native-step-'+i,
   request:{id:'native-step-'+i,repository:'example/project',repositoryId:'100',runId:String(3000+i),attempt:'1'},
   jobRecord:{runnerId:'999'},sourceCommit:'b'.repeat(40)};
  const result=await executeNativeDelivery(core,{root:f.root,binding,context,provider:f.provider,transport:f.transport,roleFactory,progress:async value=>{progress.push(value);}});
  assert.equal(result.disposition,'completed',JSON.stringify({result,preparationError,prepared,progress}));assert.equal(result.executed,stage);
 }
 const target={repository:'example/project',repositoryId:'100',databaseId:'123',issueId:'I_123',number:'55'};
 const store=await openNativeCycle(core,{root:f.root,target}),state=await store.read();
 assert.deepEqual(prepared.map(x=>x.role),['shape','shape-validator']);assert.equal(new Set(prepared.map(x=>x.id)).size,2);
 assert.deepEqual(prepared.map(x=>x.run),['3000','3001']);assert.equal(closed.length,3);
 assert.equal(f.issue.body,body);assert.ok(f.issue.labels.some(l=>l.name==='shaped'));
 assert.equal(core.parseRequirementsDocument(f.issue.body).scope,original);assert.ok(state.receipts.publication);
 assert.equal(state.receipts.publication.execution.source.runId,'3002');
 assert.equal(core.nextDeliveryStage(state.receipts),'deliver');
}));

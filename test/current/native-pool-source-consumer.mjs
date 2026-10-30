import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {join} from 'node:path';
import {withCurrentCore,currentPin} from '../../src/current-core.mjs';
import {nativePoolSource} from '../../src/native-pool-source.mjs';
import {managedWorkflowPlan} from '../../src/workflow-inputs.mjs';
import {sha,ensurePrivateDirectory,writePrivate} from '../../src/local-state.mjs';

async function fixture(core,context){
 const target={version:1,repository:'example/project',repositoryId:'100',ownerId:'7',ownerType:'User'};
 const configuration={version:2,id:'member-default',capacity:2,targets:[target]},request={id:'request-0',repository:target.repository,
  repositoryId:'100',jobId:'200',runId:'300',attempt:'1'},sourceCommit='a'.repeat(40),issueBody='Current Issue scope';
 const body={version:1,request,memberId:'7',issue:'55',stage:'deliver',sourceCommit,workflowId:'400',
  correlation:'correlation-0',jobName:'stage / execute',issueBodySha256:sha(issueBody)};
 const retained={...body,identity:core.contentIdentity(body)},files=managedWorkflowPlan(core,{executionPool:configuration,repository:target.repository}).files;
 const runtime=JSON.stringify({version:1,package:currentPin,executionPool:configuration,allowedActorIds:['7']});
 const installationRoot=join(context.root,'source-installations'),directory=join(installationRoot,'example--project');
 await ensurePrivateDirectory(directory);
 await writePrivate(join(directory,'installation.json'),{version:1,repository:target.repository,repositoryId:'100',
  installation:{packageVersion:currentPin.packageVersion,pins:core.distributionIdentity().pins},
  executionPool:configuration,files:{'.nibfin/runtime.json':sha(runtime)}});
 const routes=new Map([['/user',{id:'7'}],['/repos/example/project',{id:'100',full_name:target.repository,private:false,visibility:'public',
  permissions:{admin:true},owner:{id:'7',type:'User'}}],
 ['/repos/example/project/actions/runs/300',{id:'300',run_attempt:1,workflow_id:'400',path:'.github/workflows/nibfin-stage.yml',event:'workflow_dispatch',
  head_sha:sourceCommit,head_branch:'main',repository:{id:'100',full_name:target.repository},head_repository:{id:'100'},actor:{id:'7'},triggering_actor:{id:'7'},
  display_title:core.actionRunName({issue:'55',stage:'deliver',correlation:'correlation-0'})}],
 ['/repos/example/project/actions/jobs/200',{id:'200',run_id:'300',run_attempt:1,head_sha:sourceCommit,name:'stage / execute',status:'queued',
  labels:['self-hosted','linux','x64',core.memberRunnerLabel('7'),'nibfin-request-request-0']}],
 ['/repos/example/project/issues/55',{number:55,state:'open',body:issueBody,assignees:[{id:'7'}]}]]);
 for(const [path,value]of [...files,['.nibfin/runtime.json',runtime]]){
  const bytes=Buffer.from(value);routes.set('/repos/example/project/contents/'+path+'?ref='+sourceCommit,
   {type:'file',path,encoding:'base64',content:bytes.toString('base64'),size:bytes.length,
    sha:createHash('sha1').update(`blob ${bytes.length}\0`).update(bytes).digest('hex')});
 }
 const calls=[],provider=core.createGithubProvider({attempts:1,transport:async r=>{
  calls.push(r);assert.equal(r.method,'GET');return {status:routes.has(r.endpoint)?200:404,body:JSON.stringify(routes.get(r.endpoint)??{})};
 }});
 const source=nativePoolSource(core,{configuration,memberId:'7',provider,installationRoot,
  dispatches:{read:async()=>structuredClone(retained)},onBlocked:async()=>({status:'retained'})});
 return {source,routes,calls,request,retained};
}

test('JIT source revalidates the project Run, Job, immutable installed workflow and live Issue without a write',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context),result=await f.source.inspect('request-0');
 assert.equal(result.authorized,true);assert.deepEqual(result.request,f.request);assert.equal(result.stage,'deliver');
 assert.equal(result.identity,f.retained.identity);
 for(const path of ['/user','/repos/example/project','/repos/example/project/actions/runs/300',
  '/repos/example/project/actions/jobs/200','/repos/example/project/issues/55'])assert.ok(f.calls.some(r=>r.endpoint===path));
 for(const path of ['.nibfin/runtime.json','.github/workflows/nibfin-stage.yml','.github/workflows/nibfin-delivery.yml'])
  assert.ok(f.calls.some(r=>r.endpoint.includes('/contents/'+path+'?ref='+f.retained.sourceCommit)));
 f.routes.get('/repos/example/project/actions/jobs/200').status='in_progress';
 assert.equal((await f.source.inspect('request-0')).identity,result.identity);
 assert.equal((await f.source.blocked({request:'request-0'})).status,'retained');
}));

test('foreign repository, unapproved actor, mismatched Job, absent request label and changed Scope all deny JIT authority',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);
 const cases=[['/user',v=>{v.id='8';}],['/repos/example/project',v=>{v.permissions.admin=false;}],
  ['/repos/example/project/actions/runs/300',v=>{v.head_repository.id='101';}],
  ['/repos/example/project/actions/runs/300',v=>{v.triggering_actor.id='8';}],
  ['/repos/example/project/actions/runs/300',v=>{v.display_title='#55 | accept';}],
  ['/repos/example/project/actions/jobs/200',v=>{v.run_attempt=2;}],
  ['/repos/example/project/actions/jobs/200',v=>{v.labels=v.labels.filter(s=>!s.startsWith('nibfin-request-'));}],
  ['/repos/example/project/issues/55',v=>{v.pull_request={};}]];
 for(const [path,change]of cases){const original=structuredClone(f.routes.get(path));change(f.routes.get(path));
  await assert.rejects(f.source.inspect('request-0'),/native-pool-source:/);f.routes.set(path,original);}
 assert.ok(f.calls.every(r=>r.method==='GET'));
 const issue=f.routes.get('/repos/example/project/issues/55'),original=structuredClone(issue);
 for(const change of [v=>{v.body+=' changed';},v=>{v.assignees=[];},v=>{v.state='closed';}]){
  Object.assign(issue,structuredClone(original));change(issue);
  const observation=await f.source.inspect('request-0');assert.equal(observation.authorized,true);
  assert.equal(observation.launchAuthorized,false);assert.equal(observation.identity,f.retained.identity);
 }
}));

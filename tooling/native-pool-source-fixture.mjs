import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {join} from 'node:path';
import {currentPin} from '../src/current-core.mjs';
import {nativePoolSource} from '../src/native-pool-source.mjs';
import {managedWorkflowPlan} from '../src/workflow-inputs.mjs';
import {sha,ensurePrivateDirectory,writePrivate} from '../src/local-state.mjs';
export async function nativePoolSourceFixture(core,context){
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
 return {source,routes,calls,request,retained,provider,configuration,installationRoot,issueBody};
}

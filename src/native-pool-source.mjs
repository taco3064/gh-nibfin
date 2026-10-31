import {createHash} from 'node:crypto';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {readPrivate,sha} from './local-state.mjs';
import {currentPin} from './current-core.mjs';
import {managedWorkflowPlan} from './workflow-inputs.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-pool-source:'+code);};
const id=v=>typeof v==='string'&&/^[1-9][0-9]*$/.test(v);
const hash=v=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
function contents(value,path){
 need(value?.type==='file'&&value.path===path&&value.encoding==='base64'&&typeof value.content==='string'
  &&value.content.length<=262144&&Number.isSafeInteger(value.size)&&value.size>0&&value.size<=131072,'contents');
 const encoded=value.content.replace(/[\r\n]/g,''),bytes=Buffer.from(encoded,'base64');
 need(bytes.length===value.size&&bytes.toString('base64')===encoded
  &&createHash('sha1').update(`blob ${bytes.length}\0`).update(bytes).digest('hex')===value.sha,'git-blob');return bytes;
}
/** A private retained Core dispatch is only a selector. This adapter rechecks
 * the execution account, protected installation, exact immutable workflows,
 * actual project Run/Job, member labels and current Issue SSOT before JIT writes.
 * Protected callbacks are composed by the maintained host, never read from JSON,
 * an event, a model answer or a configurable user module.
 */
export function nativePoolSource(core,{configuration,memberId,provider,dispatches,onBlocked,
 closedContinuation,launchAllowed,
 installationRoot=join(userInfo().homedir,'.local/share/nibfin/installations')}={}){
 const pool=core.validateExecutionPool(configuration);
 need(pool.version===2&&id(memberId)&&typeof provider?.request==='function'
  &&typeof dispatches?.read==='function'&&typeof onBlocked==='function','composition');
 need(closedContinuation===undefined||typeof closedContinuation==='function','closed-continuation');
 need(launchAllowed===undefined||typeof launchAllowed==='function','launch-restriction');
 return Object.freeze({
  async inspect(selector,{signal}={}){
   need(typeof selector==='string'&&/^[A-Za-z0-9_-]{1,80}$/.test(selector),'selector');
   const retained=await dispatches.read(selector);need(retained&&hash(retained.identity),'retained-dispatch');
   const {identity,...body}=retained;
   need(identity===core.contentIdentity(body)&&retained.version===1&&retained.memberId===memberId
    &&retained.request?.id===selector&&id(retained.issue)&&id(retained.workflowId)
    &&/^[a-f0-9]{40}$/.test(retained.sourceCommit)&&hash(retained.issueBodySha256)
    &&typeof retained.jobName==='string'&&['stage / execute','stage / compute','rescue'].includes(retained.jobName),'retained-identity');
   const request=retained.request,target=pool.targets.find(t=>t.repository===request.repository&&t.repositoryId===request.repositoryId);
   need(target&&['jobId','runId','attempt'].every(k=>id(request[k])),'target');
   const get=async endpoint=>{
    const r=await provider.request({endpoint,target:request.repositoryId,operation:'native-pool-source',correlation:selector,signal});
    need(r.outcome==='read','fresh-read');return r.data;
   };
   need((await get('/user')).id===memberId,'execution-account');
   const root='/repos/'+target.repository,repository=await get(root);
   need(repository.id===target.repositoryId&&repository.full_name?.toLowerCase()===target.repository
    &&repository.private===false&&repository.visibility==='public'&&repository.permissions?.admin===true
    &&repository.owner?.id===target.ownerId&&repository.owner.type===target.ownerType,'repository');
   const installation=await readPrivate(join(installationRoot,target.repository.replace('/','--'),'installation.json'));
   need(installation.version===1&&installation.repository===target.repository&&installation.repositoryId===target.repositoryId
    &&installation.installation?.packageVersion===currentPin.packageVersion
    &&core.canonicalJSON(installation.installation?.pins)===core.canonicalJSON(core.distributionIdentity().pins)
    &&core.canonicalJSON(installation.executionPool)===core.canonicalJSON(pool),'installation');
   const read=async path=>contents(await get(root+'/contents/'+path+'?ref='+retained.sourceCommit),path);
   const ownerBytes=await read('.nibfin/runtime.json'),policy=JSON.parse(ownerBytes);
   need(sha(ownerBytes)===installation.files?.['.nibfin/runtime.json']&&policy.version===1
    &&policy.package?.coreSha256===currentPin.coreSha256&&Array.isArray(policy.allowedActorIds)
    &&policy.allowedActorIds.includes(memberId)&&core.canonicalJSON(policy.executionPool)===core.canonicalJSON(pool),'owner-policy');
   const plan=managedWorkflowPlan(core,{executionPool:pool,repository:target.repository}),rescue=retained.jobName==='rescue';
   const path=rescue?'.github/workflows/nibfin-rescue.yml':'.github/workflows/nibfin-stage.yml';
   for(const file of rescue?[path]:[path,...(plan.files.has('.github/workflows/nibfin-delivery.yml')?['.github/workflows/nibfin-delivery.yml']:[])])
    need((await read(file)).equals(Buffer.from(plan.files.get(file))),'workflow-bytes');
   const run=await get(root+'/actions/runs/'+request.runId);
   need(run.id===request.runId&&String(run.run_attempt)===request.attempt&&run.workflow_id===retained.workflowId
    &&run.path===path&&run.event==='workflow_dispatch'&&run.head_sha===retained.sourceCommit&&run.head_branch==='main'
    &&run.repository?.id===target.repositoryId&&run.repository.full_name?.toLowerCase()===target.repository
    &&run.head_repository?.id===target.repositoryId&&policy.allowedActorIds.includes(run.actor?.id)
    &&policy.allowedActorIds.includes(run.triggering_actor?.id)
    &&run.display_title===core.actionRunName({issue:retained.issue,stage:retained.stage,correlation:retained.correlation,
      ...(rescue?{rescue:true,mode:retained.mode}:{})}),'run');
   const job=await get(root+'/actions/jobs/'+request.jobId);
   need(job.id===request.jobId&&job.run_id===request.runId&&(job.run_attempt===undefined||String(job.run_attempt)===request.attempt)
    &&job.head_sha===retained.sourceCommit&&job.name===retained.jobName&&['queued','in_progress','completed'].includes(job.status)
    &&Array.isArray(job.labels)&&job.labels.includes('nibfin-request-'+selector)
    &&(rescue||job.labels.includes(core.memberRunnerLabel(memberId))),'job');
   const issue=await get(root+'/issues/'+retained.issue);
   need(!issue.pull_request&&String(issue.number)===retained.issue&&['open','closed'].includes(issue.state)
    &&typeof issue.body==='string','issue-ssot');
   const closed=issue.state==='closed'&&(rescue||['delivered','fanout'].includes(retained.stage))&&closedContinuation
    ?await closedContinuation({issue,target,retained}):false;
   const launchAuthorized=(issue.state==='open'||closed===true)&&sha(issue.body)===retained.issueBodySha256
    &&issue.assignees?.some(a=>a.id===memberId)===true&&(!launchAllowed||await launchAllowed()===true);
   return {authorized:true,launchAuthorized,identity,memberId,request:structuredClone(request),issue:retained.issue,stage:retained.stage,status:job.status};
  },
  async blocked(value){return onBlocked(structuredClone(value));}
 });
}

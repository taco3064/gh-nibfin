import fs from 'node:fs/promises';
import {watch} from 'node:fs';
import {join} from 'node:path';
import {readPrivate,writePrivate,ensurePrivateDirectory,sha} from './local-state.mjs';
import {maintenanceLock} from './upgrade.mjs';
import {nativePoolSource} from './native-pool-source.mjs';
import {openNativeCycle} from './native-delivery-store.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-pool-intake:'+code);};
const id=v=>typeof v==='string'&&/^[1-9][0-9]*$/.test(v);
const selector=v=>typeof v==='string'&&/^[A-Za-z0-9_-]{1,80}$/.test(v);

/** Events are hints. Before retaining or consuming one, independently read the
 * actual project Run/Job and invoke the maintained source verifier. Display text
 * only locates an Issue; it cannot authenticate a workflow or authorize a role.
 * No Runner token or registration effect occurs in this intake owner.
 */
export async function nativePoolIntake(core,{root,configuration,memberId,provider,onBlocked,installationRoot,launchAllowed}={}){
 const pool=core.validateExecutionPool(configuration);need(pool.version===2&&id(memberId)&&typeof onBlocked==='function','composition');
 const directory=join(root,'pool-intake');await ensurePrivateDirectory(directory);
 const validate=value=>{const {identity,...body}=value;need(value.version===1&&identity===core.contentIdentity(body),'private-integrity');return value;};
 let tail=Promise.resolve();
 const readUnlocked=async key=>{need(selector(key),'selector');const value=await readPrivate(join(directory,key+'.json'),{missing:true});return value===null?null:validate(value);};
 const read=async key=>{await tail;return readUnlocked(key);};
 const sourceFor=dispatches=>nativePoolSource(core,{configuration:pool,memberId,provider,dispatches,onBlocked,launchAllowed,
  closedContinuation:async({issue,target,retained})=>{
   try{
    const selected={repository:target.repository,repositoryId:target.repositoryId,issueId:issue.node_id,databaseId:issue.id,number:String(issue.number)};
    const original=await openNativeCycle(core,{root,target:selected}),state=await original.read();
    need(core.parseRequirementsDocument(issue.body).scope===state.scopeBody&&state.closed===true,'original-closure');
    const stages=core.coreStages.slice(2,13),receipts=Object.fromEntries(stages.map(s=>[s,state.receipts[s]]));
    const expectations=Object.fromEntries(stages.map(s=>[s,state.expectations[s]]));
    core.verifyEvidenceChain({version:1,receipts},expectations,'closure');
    return retained.stage==='delivered'||state.completion!==null&&Boolean(core.verifyCompletion(state.completion,state.completionCurrent));
   }catch{return false;}
  },
  ...(installationRoot?{installationRoot}:{})});
 const source=sourceFor({read});
 async function get(target,endpoint,signal){const r=await provider.request({endpoint,target:target.repositoryId,
  operation:'native-pool-intake',correlation:'intake',signal});need(r.outcome==='read','provider-read');return r.data;}
 return Object.freeze({source,read,
  async list(){await tail;const files=await fs.readdir(directory);need(files.length<=10000,'intake-bound');
   const values=[];for(const file of files.sort()){if(file==='maintenance.lock')continue;
    if(/^[A-Za-z0-9_-]{1,80}\.json\.[a-f0-9-]{36}$/.test(file))continue;
    need(/^[A-Za-z0-9_-]{1,80}\.json$/.test(file),'private-inventory');values.push(await read(file.slice(0,-5)));}return values;
  },
  async accept({repository,runId,jobId},{signal}={}){
   const target=pool.targets.find(t=>t.repository===repository);need(target&&id(runId)&&id(jobId),'target');
   const base='/repos/'+repository,job=await get(target,base+'/actions/jobs/'+jobId,signal);
   need(job.id===jobId&&job.run_id===runId&&Array.isArray(job.labels),'job');
   const labels=job.labels.filter(l=>typeof l==='string'&&l.startsWith('nibfin-request-'));
   need(labels.length===1&&selector(labels[0].slice(15)),'request-label');const key=labels[0].slice(15);
   const prior=await read(key);if(prior){need(prior.request.runId===runId&&prior.request.jobId===jobId,'selector-reuse');await source.inspect(key,{signal});return {status:'retained',id:key,already:true};}
   need(job.status==='queued','new-job-not-queued');
   const run=await get(target,base+'/actions/runs/'+runId,signal),rescue=job.name==='rescue';
   const match=/^(Rescue \| )?#([1-9][0-9]*) \| ([a-z-]+)(?: \| (repair|diagnose|verify))? \| op:([A-Za-z0-9_-]{1,80})$/.exec(run.display_title??'');
   need(match&&!!match[1]===rescue&&id(run.workflow_id)&&/^[a-f0-9]{40}$/.test(run.head_sha)
    &&Number.isSafeInteger(run.run_attempt)&&run.run_attempt>0,'run-locator');
   const issue=await get(target,base+'/issues/'+match[2],signal);need(typeof issue.body==='string','issue');
   const body={version:1,request:{id:key,repository,repositoryId:target.repositoryId,jobId,runId,attempt:String(run.run_attempt)},
    memberId,issue:match[2],stage:match[3],sourceCommit:run.head_sha,workflowId:run.workflow_id,
    correlation:match[5],jobName:job.name,issueBodySha256:sha(issue.body),...(rescue?{mode:match[4]}:{})};
   const candidate={...body,identity:core.contentIdentity(body)};
   const approved=await sourceFor({read:async requested=>requested===key?candidate:null}).inspect(key,{signal});
   need(approved.authorized===true&&approved.launchAuthorized===true,'launch-authority');
   const write=tail.then(()=>maintenanceLock(directory,async()=>{
    const existing=await readUnlocked(key);if(existing){need(existing.identity===candidate.identity,'concurrent-intake');return {status:'retained',id:key,already:true};}
    await writePrivate(join(directory,key+'.json'),candidate,{exclusive:true});return {status:'retained',id:key};
   }));tail=write.catch(()=>{});return write;
  },
  subscribe(notify){need(typeof notify==='function','subscription');const watcher=watch(directory,(_event,name)=>{
   if(name===null||name.toString().endsWith('.json'))notify();});watcher.on('error',()=>notify({unavailable:true}));return ()=>watcher.close();}
 });
}

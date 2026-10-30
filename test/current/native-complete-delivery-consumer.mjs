import test from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{execFileSync}from'node:child_process';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';
import{executeNativeDelivery}from'../../src/native-delivery.mjs';import{initializeFileHostStore}from'../../src/host-store.mjs';
import{openNativeCycle}from'../../src/native-delivery-store.mjs';

// Real private records, Git objects and public Core stage owners. External
// model/GitHub observations are explicit doubles, not live delivery evidence.
test('one native cycle retains every actual owner output through independent reviews, protected merge and causal completion',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),source=join(f.root,'source'),remote=join(f.root,'remote.git');
 await fs.mkdir(source,{mode:0o700});const env={PATH:'/usr/bin:/bin',HOME:'/nonexistent',GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null'};
 const git=(cwd,args,input)=>execFileSync('/usr/bin/git',['-c','core.hooksPath=/dev/null',...args],{cwd,env,input,stdio:['pipe','pipe','pipe']}).toString().trim();
 git(source,['init','--initial-branch=main','--template=']);await fs.writeFile(join(source,'README.md'),'original source\n');
 await fs.mkdir(join(source,'.github/workflows'),{recursive:true});await fs.writeFile(join(source,'.github/workflows/ci.yml'),f.workflow);
 git(source,['add','.']);git(source,['-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-m','original']);
 const base=git(source,['rev-parse','HEAD']);git(f.root,['clone','--bare','--no-hardlinks',source,remote]);f.setBase(base);f.setTree(['.github','README.md']);
 Object.assign(f.repo,{allow_merge_commit:false,allow_squash_merge:false,allow_rebase_merge:true});f.repo.permissions.push=true;
 const host=await initializeFileHostStore(core,{directory:join(f.root,'state'),installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins},
  repositories:[{repository:'example/project',repositoryId:'100'}]});let state=await host.read();await host.updateWork({expected:state.identity,work:{...state.work,paused:false}});await host.close();
 const original=f.issue.body,body=core.formatRequirementsDocument({scope:original,shape:[
  'Update README.md with the native onboarding procedure.',
  'Candidate artifacts applicability: The Scope requires no additional untracked or ignored artifacts.',
  'Candidate hooks applicability: The fixed source has no applicable Hook configuration.',
  '```nibfin-candidate','{"artifacts":[],"hooks":[]}','```'].join('\n')});
 let pr=null,mergeCommit=null,events=[],writes=[];const reply=(data,status=200)=>({status,body:JSON.stringify(data)}),root='/repos/example/project';
 const protection={required_status_checks:{strict:true,contexts:['ci'],checks:[{context:'ci',app_id:'15368'}]},enforce_admins:{enabled:true}};
 f.intercept(async request=>{
  const path=request.endpoint,payload=request.body?JSON.parse(request.body):null;
  if(request.method!=='GET')writes.push({method:request.method,path});
  if(path===root+'/pulls'&&request.method==='POST'){
   const head=git(remote,['rev-parse','refs/heads/'+payload.head]);
   pr={id:'500',number:60,title:payload.title,body:payload.body,head:{sha:head,ref:payload.head,repo:{id:'100'}},
    base:{sha:base,ref:'main',repo:{id:'100'}},state:'open',merged:false,draft:false,mergeable:true,mergeable_state:'clean',merged_at:null};return reply(pr,201);
  }
  if(path===root+'/pulls?state=all&per_page=100')return reply(pr?[pr]:[]);
  if(path===root+'/pulls/60')return reply(pr);
  if(path===root+'/pulls/60/merge'&&request.method==='PUT'){
   assert.equal(payload.sha,pr.head.sha);assert.equal(payload.merge_method,'rebase');
   const tree=git(remote,['rev-parse',pr.head.sha+'^{tree}']);
   mergeCommit=git(remote,['-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit-tree',tree,'-p',base],Buffer.from('rebased original candidate\n'));
   git(remote,['update-ref','refs/heads/main',mergeCommit,base]);Object.assign(pr,{merged:true,state:'closed',merge_commit_sha:mergeCommit,merged_at:new Date().toISOString()});
   return reply({merged:true,sha:mergeCommit,message:'Merged'});
  }
  if(path===root+'/issues/55'&&request.method==='PATCH'&&payload.state==='closed'){
   Object.assign(f.issue,{state:'closed',state_reason:'completed',closed_at:new Date().toISOString()});
   events.push({id:'700',event:'closed',created_at:f.issue.closed_at,actor:{id:'7'}});return reply(f.issue);
  }
  if(path===root+'/issues/55/labels'&&request.method==='POST'&&payload.labels.includes('delivered')){
   f.issue.labels.push({id:'902',name:'delivered'});return reply(f.issue.labels);
  }
  if(path===root+'/issues/55/events?per_page=100')return reply(events);
  if(path===root+'/issues/55/dependencies/blocking?per_page=100')return reply([]);
  if(path===root+'/branches/main')return reply({name:'main',commit:{sha:mergeCommit??base},protected:true});
  if(path===root+'/branches/main/protection')return reply(protection);
  if(path===root+'/rules/branches/main?per_page=100')return reply([]);
  const commit=/^\/repos\/example\/project\/commits\/([a-f0-9]{40})(.*)$/.exec(path);
  if(commit){const[,head,suffix]=commit;
   if(suffix.startsWith('/check-runs'))return reply({total_count:1,check_runs:[{id:'800',name:'ci',head_sha:head,app:{id:'15368'},status:'completed',conclusion:'success'}]});
   if(suffix.startsWith('/statuses'))return reply([]);
   if(!suffix)return reply({sha:head,commit:{tree:{sha:git(remote,['rev-parse',head+'^{tree}'])}}});
  }
  if(path.startsWith(root+'/compare/'))return reply({status:'identical',ahead_by:0,behind_by:0,base_commit:{sha:mergeCommit},merge_base_commit:{sha:mergeCommit}});
 });
 const launched=[],progress=[];
 const roleFactory=async(_core,{source:provenance,retain,binding})=>{
  const directory=join(f.root,binding.request.id);await fs.mkdir(directory,{mode:0o700});
  const candidateDirectory=join(directory,'candidate');await fs.mkdir(candidateDirectory,{mode:0o700});
  const work=join(directory,'work');await fs.mkdir(work,{mode:0o700});let cloned=false;const values=new Map();
  const metadata=core.createNativeRoleBindings({ownership:'personal',source:provenance,retain});
  return{sourceDirectory:source,candidateDirectory,workspace:()=>work,
   async prepareRole(task){
    if(task.role==='deliver'){git(directory,['clone','--no-hardlinks',source,'work']);cloned=true;}
    if(task.task.workspace){const captured=core.importWorkspace(Buffer.from(core.canonicalJSON(task.task.workspace)),{expected:task.task.expected});
     core.materializeWorkspace(captured,{expected:task.task.expected,root:work});}
    const bound=await metadata.bind(task),expected=bound.current().expected;if(task.role==='shape')expected.inputs={body:core.bodyDigest(body)};
    const current=()=>({expected:structuredClone(expected),consumed:[]});
    const backend={readiness:async()=>({login:'available',tool:'available',isolation:'verified',read:'verified',write:'verified',evidence:['recording-only'],limitations:[]}),
     start:async()=>{launched.push({role:task.role,run:provenance.runId,id:bound.input.execution.id});
      if(task.role==='deliver'){assert.equal(cloned,true);await fs.writeFile(join(work,'README.md'),'native onboarding procedure\n');}return{status:'started'};},
     collect:async()=>({status:'complete',exitCode:0,evidence:structuredClone(expected)}),termination:async()=>({status:'terminated'}),stop:async()=>({status:'requested'}),cleanup:async()=>({status:'completed',residuals:[]})};
    const id='fixture-'+binding.request.id,lifecycle=core.createAgentLifecycle({environment:{id:id+'-environment',agent:'recording',loginSpace:'/fixture/login',
     instances:[{id,workspace:work,registration:join(directory,'registration'),service:join(directory,'service')}]},instanceId:id,backend,current,startCurrent:bound.startCurrent});
    const value={input:bound.input,lifecycle,current,readBody:()=>body,taskIdentity:core.contentIdentity(task.task),
     bindCandidate:async candidate=>{expected.inputs={...expected.inputs,candidate};}};values.set(bound.input.execution.id,value);return value;
   },get:id=>{assert.ok(values.has(id));return values.get(id);},async close(){}}
 };
 const stages=[...core.coreStages.slice(0,14),'fanout'];
 for(const[i,stage]of stages.entries()){
  const binding={configuration:f.configuration,ownerType:'User',memberId:'7',issue:'55',stage,correlation:'full-native-'+i,
   request:{id:'full-native-'+i,repository:'example/project',repositoryId:'100',runId:String(4000+i),attempt:'1'},jobRecord:{runnerId:'999'},sourceCommit:base};
  const result=await executeNativeDelivery(core,{root:f.root,binding,context,provider:f.provider,transport:f.transport,roleFactory,
   publicationGitFactory:options=>core.createPublicationGit({...options,localFixture:remote}),progress:async value=>progress.push(value)});
  let history,completionDebug;
  if(result.disposition!=='completed'){const target={repository:'example/project',repositoryId:'100',databaseId:'123',issueId:'I_123',number:'55'};
   const stored=await openNativeCycle(core,{root:f.root,target}),snapshot=await stored.read();completionDebug={phases:snapshot.completions,operations:snapshot.operations,records:[]};for(const ref of Object.keys(snapshot.records)){const value=await stored.get('native-record:'+ref);if(value.kind.includes('diagnostic'))completionDebug.records.push(value);}history=Object.values(snapshot.publications).map(records=>records.map(r=>r.event.kind));}
  assert.equal(result.disposition,'completed',JSON.stringify({stage,result,progress:progress.slice(-5),history,writes,completionDebug}));
 }
 const target={repository:'example/project',repositoryId:'100',databaseId:'123',issueId:'I_123',number:'55'},store=await openNativeCycle(core,{root:f.root,target});state=await store.read();
 assert.equal(core.verifyCompletion(state.completion,state.completionCurrent).identity,state.completion.identity);
 assert.deepEqual(launched.map(v=>v.role),['shape','shape-validator','deliver','accept','shaper-review']);assert.equal(new Set(launched.map(v=>v.run)).size,5);
 assert.equal(f.issue.state,'closed');assert.ok(f.issue.labels.some(l=>l.name==='delivered'));assert.equal(state.fanout.outcome,'discovered');
 assert.equal(writes.filter(v=>v.path.endsWith('/merge')).length,1);assert.equal(writes.filter(v=>v.method==='PATCH'&&v.path.endsWith('/55')).length,2);
}));

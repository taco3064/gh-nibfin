import test from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{execFileSync}from'node:child_process';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';
import{prepareNativeCycle}from'../../src/native-cycle-policy.mjs';import{initializeFileHostStore,openFileHostStore}from'../../src/host-store.mjs';
import{prepareNativeBaseRevision}from'../../src/native-base-revision.mjs';import{prepareDeferredNativeRole}from'../../src/native-deferred-role.mjs';
import{ensurePrivateDirectory,writePrivate}from'../../src/local-state.mjs';import{restoreUnacceptedWorkspace}from'../../src/native-unaccepted-work.mjs';

for(const published of [false,true])test('main advancement preserves Candidate and Writer, repeats dependent gates; published PR='+published,async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),source=join(f.root,'source');await fs.mkdir(source,{mode:0o700});
 const env={PATH:'/usr/bin:/bin',HOME:'/nonexistent',GIT_CONFIG_GLOBAL:'/dev/null',GIT_CONFIG_NOSYSTEM:'1'};
 const git=(cwd,args)=>execFileSync('/usr/bin/git',['-c','core.hooksPath=/dev/null',...args],{cwd,env,stdio:'pipe'}).toString().trim();
 git(source,['init','--initial-branch=main','--template=']);await fs.writeFile(join(source,'README.md'),'old\n');git(source,['add','.']);
 const commit=()=>git(source,['-c','user.name=Fixture','-c','user.email=f@example.invalid','commit','-m','fixture']);commit();const old=git(source,['rev-parse','HEAD']);f.setBase(old);
 const store=await prepareNativeCycle(core,f.options),initial=await store.read(),hash='a'.repeat(64),shape=core.formatRequirementsDocument({scope:f.issue.body,shape:'Candidate artifacts applicability: No additional artifacts are required.\nCandidate hooks applicability: No hooks apply.\n```nibfin-candidate\n{"artifacts":[],"hooks":[]}\n```'});
 f.issue.body=shape;const shapeHash=core.bodyDigest(shape),execution={role:'deliver',skill:'deliver',id:'original-writer',request:'original-request',result:'original-result',source:{repositoryId:'100',workflow:hash,runId:'4000',attempt:'1'}};
 await fs.writeFile(join(source,'README.md'),'preserved authorized documentation\n');
 const captured=core.captureWorkspace({root:source,base:old,scopeBody:initial.scopeBody,shapeBody:shape,permittedPaths:['README.md'],binding:{repository:'example/project',cycle:initial.cycle,policy:core.contentIdentity(initial.policy),scopeHash:initial.scopeHash,shapeHash}});
 const binding={authority:initial.authority.identity,grant:core.contentIdentity(initial.grant),policy:core.contentIdentity(initial.policy),core:initial.core,runtime:hash,instructions:hash,repositoryId:'100',issueId:'I_123',scopeHash:initial.scopeHash,shapeHash};
 const receipts={},expectations={},attempts={};
 const inputs={shape:{body:shapeHash},'shape-validator':{body:shapeHash,author:'author'},publication:{body:shapeHash,author:'author',validator:'validator',validation:hash,readback:hash},deliver:{publication:hash,candidate:captured.candidate},'pre-accept-hooks':{candidate:captured.candidate,phase:'pre-accept',receipt:hash},accept:{candidate:captured.candidate,hooks:hash,deliver:execution.id},'pre-commit-hooks':{candidate:captured.candidate,phase:'pre-commit',accept:hash,receipt:hash}};
 for(const[stage,value]of Object.entries(inputs)){
  const role=['shape','shape-validator','deliver','accept'].includes(stage)?stage:'deliver',id=stage==='deliver'?execution.id:stage+'-execution';
  const result=['shape','shape-validator','deliver','accept'].includes(stage)?{version:1,outcome:{shape:'ready','shape-validator':'approved',deliver:'ready',accept:'accepted'}[stage],failureClass:'none',summary:'Fixture only',conflicts:[]}:null;
  const current={expected:{version:1,stage,binding,execution:{...execution,role,skill:core.skillForRole(role),id},inputs:value,result},consumed:[]};
  receipts[stage]=core.createEvidence(current);expectations[stage]=current;attempts[stage]={request:stage,phase:'completed'};
 }
 const claim={id:'original-pr',stage:'pr',runId:'5000',conclusion:'failure'};attempts.pr={request:claim.id,phase:'unresolved'};
 const sessionReference=await store.retain({kind:'native-role-session',metadata:{id:'00000000-0000-4000-8000-000000000001',execution:execution.id,role:'deliver',memberId:'7'},snapshot:{terminated:true,digest:hash}});
 await store.update(s=>Object.assign(s,{shapeBody:shape,receipts,expectations,attempts,dispatch:{id:claim.id,stage:'pr'},workspace:JSON.parse(core.exportWorkspace(captured)),workspaceExpected:captured.expected,candidate:captured.candidate}));
 const host=await initializeFileHostStore(core,{directory:join(f.root,'state'),installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins},repositories:[{repository:'example/project',repositoryId:'100'}]});
 let h=await host.read();await host.updateWork({expected:h.identity,work:{...h.work,paused:false,unknownWrite:true,pending:[{kind:'native-stage',cycle:initial.cycle,request:claim.id,status:'UNACCEPTED'}]}});await host.close();
 await ensurePrivateDirectory(join(f.root,'pool-state'));await writePrivate(join(f.root,'pool-state/ledger.json'),{jobs:{writer:{phase:'released',runnerId:'9',issue:'55',request:{runId:'4000',repository:'example/project'}}}});
 await fs.writeFile(join(source,'README.md'),'old\n');await fs.writeFile(join(source,'other.md'),'concurrent work\n');git(source,['add','.']);commit();const fresh=git(source,['rev-parse','HEAD']);f.setBase(fresh);
 const oldBranch='nibfin/issue-55-'+initial.cycle.slice(-12),commitSha='c'.repeat(40),pr={id:'500',number:60,state:'open',merged:false,merged_at:null,head:{ref:oldBranch,sha:commitSha,repo:{id:'100'}}};
 if(published)await store.update(s=>{s.publications.fixture=[{event:{kind:'intent',pin:{target:{headRef:oldBranch}}}},{event:{kind:'commit',value:{commit:commitSha}}}];return s;});
 const reply=x=>({status:200,body:JSON.stringify(x)});let retirements=0;f.intercept(r=>{
  if(r.endpoint.includes('/git/matching-refs/'))return reply(published?[{ref:'refs/heads/'+oldBranch,object:{sha:commitSha}}]:[]);
  if(r.endpoint.startsWith('/repos/example/project/pulls?'))return reply(published?[pr]:[]);
  if(r.endpoint==='/repos/example/project/pulls/60'){
   if(r.method==='PATCH'){assert.deepEqual(JSON.parse(r.body),{state:'closed'});pr.state='closed';retirements++;}return reply(pr);
  }
 });
 const restrictions=await openFileHostStore(core,{directory:join(f.root,'state')});try{
  const before=await restrictions.read();await restrictions.updateWork({expected:before.identity,work:{...before.work,pending:[...before.work.pending,{kind:'native-stage',cycle:'unrelated',request:'other',status:'UNACCEPTED'}]}});
  await assert.rejects(prepareNativeBaseRevision(core,{root:f.root,store,claim,provider:f.provider,transport:f.transport}),/unrelated-write-unknown/);
  const after=await restrictions.read();assert.equal(after.work.unknownWrite,true);await restrictions.updateWork({expected:after.identity,work:before.work});
 }finally{await restrictions.close();}
 if(published){pr.merged_at=new Date().toISOString();await assert.rejects(prepareNativeBaseRevision(core,{root:f.root,store,claim,provider:f.provider,transport:f.transport}),/published-pr-changed-or-merged/);assert.equal(retirements,0);pr.merged_at=null;}
 const result=await prepareNativeBaseRevision(core,{root:f.root,store,claim,provider:f.provider,transport:f.transport});assert.equal(result.outcome,'revision-prepared');
 let s=await store.read();assert.equal(s.base,fresh);assert.deepEqual(Object.keys(s.receipts),['shape','shape-validator','publication']);assert.equal(s.workspace,null);
 assert.equal(retirements,published?1:0);assert.equal(published?pr.state:'closed','closed');
 assert.equal(s.scopeHash,initial.scopeHash);assert.equal(s.shapeBody,shape);assert.deepEqual(s.grant,initial.grant);assert.equal(s.deferredRoles.deliver.sessionReference,sessionReference);
 const archived=await store.get(result.reference);assert.deepEqual(archived.previous.workspace,JSON.parse(core.exportWorkspace(captured)));assert.equal(archived.previous.receipts.accept.result.outcome,'accepted');
 const work=join(f.root,'restored');await fs.mkdir(work,{mode:0o700});const snapshot=await store.get(s.deferredRoles.deliver.workspaceReference);
 await restoreUnacceptedWorkspace({directory:work,snapshot,execution:execution.id});assert.equal(await fs.readFile(join(work,'README.md'),'utf8'),'preserved authorized documentation\n');assert.equal(git(work,['rev-parse','HEAD']),old);
 const next=await prepareDeferredNativeRole(core,{store,binding:{stage:'deliver',issue:'55',memberId:'7',request:{id:'normal-next',repository:'example/project',repositoryId:'100'}},memberId:'7'});
 assert.equal(next.next.session,'00000000-0000-4000-8000-000000000001');assert.equal(next.status,'UNACCEPTED');
 const after=await openFileHostStore(core,{directory:join(f.root,'state')});try{h=await after.read();assert.equal(h.work.unknownWrite,false);assert.deepEqual(h.work.pending,[]);}finally{await after.close();}
 assert.equal((await fs.readdir(f.root)).some(n=>n.startsWith('base-revision-')),false);
 assert.equal((await prepareNativeBaseRevision(core,{root:f.root,store,claim,provider:f.provider})).outcome,'not-applicable');
}));

import{nativeGithubProvider}from'../../src/native-github-provider.mjs';
import test from'node:test';import assert from'node:assert/strict';import{join}from'node:path';import fs from'node:fs/promises';import{execFileSync}from'node:child_process';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';
import{installNativeSource}from'../../src/native-source-install.mjs';import{writePrivate,sha}from'../../src/local-state.mjs';

test('administrative installation resumes one PR, waits for CI and never bypasses strict protection',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),repository='example/project',repositoryId='100',desired={files:new Map([['.nibfin/runtime.json','reviewed\n']])},observations=new Map([['.nibfin/runtime.json',{state:'absent'}]]);
 const files=Object.fromEntries([...desired.files].map(([p,b])=>[p,sha(b)])),identity=core.contentIdentity({version:1,repository,repositoryId,files}),commit='c'.repeat(40),branch='nibfin/install-'+identity.slice(0,24),pr={id:'400',number:40,html_url:'https://github.com/example/project/pull/40',head:{sha:commit,ref:branch,repo:{id:repositoryId}},base:{sha:'b'.repeat(40),ref:'main'},state:'open',draft:false,merged:false};
 let present=false,strict=true,qualified=false,writes=[];
 const proposal={version:1,kind:'administrative-installation',identity,repository,repositoryId,files,base:'b'.repeat(40),branch,commit,phase:'pr-intent'};
 await writePrivate(join(f.root,'managed-source-pr.json'),proposal);
 f.intercept(async r=>{
  const path='/repos/'+repository;
  if(r.endpoint.startsWith(path+'/pulls?'))return{status:200,body:JSON.stringify(present?[pr]:[])};
  if(r.endpoint===path+'/pulls/40')return{status:200,body:JSON.stringify(pr)};
  if(r.endpoint===path+'/branches/main/protection')return{status:200,body:JSON.stringify({enforce_admins:{enabled:true},required_status_checks:{strict}})};
  if(r.method!=='GET'){writes.push(r);assert.equal(r.endpoint,path+'/pulls/40/merge');assert.deepEqual(JSON.parse(r.body),{sha:commit,merge_method:'rebase'});Object.assign(pr,{merged:true,merge_commit_sha:'d'.repeat(40)});return{status:200,body:'{"merged":true}'};}
 });
 const selected={...core,observeGithubCI:async()=>({qualifies:qualified,status:qualified?'satisfied':'pending',policy:{requirements:[{context:'ci'}]}})},options={root:f.root,repository,repositoryId,desired,observations,provider:core.createGithubProvider({transport:f.transport,attempts:1,authorize:async()=>true})};
 await assert.rejects(installNativeSource(selected,options),/original-pr-reconciliation-required/);assert.equal(writes.length,0);
 present=true;assert.equal((await installNativeSource(selected,options)).status,'pending');assert.equal(writes.length,0);
 qualified=true;strict=false;await assert.rejects(installNativeSource(selected,options),/protected-pr-required/);assert.equal(writes.length,0);
 strict=true;assert.equal((await installNativeSource(selected,options)).status,'installed');assert.equal(writes.length,1);
 assert.equal((await installNativeSource(selected,options)).status,'installed');assert.equal(writes.length,1);
}));
test('source installation preserves user modifications before creating a proposal',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context);
 await assert.rejects(installNativeSource(core,{root:f.root,repository:'example/project',repositoryId:'100',provider:f.provider,desired:{files:new Map([['.nibfin/runtime.json','reviewed']])},observations:new Map([['.nibfin/runtime.json',{state:'present',bytes:Buffer.from('user-owned')} ]])}),/user-modification-preserved/);
}));

test('real source capture retains unchanged files and resumes the same unpushed installation plan',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),source=join(context.root,'source'),remote=join(context.root,'remote.git');await fs.mkdir(source,{mode:0o700});
 const git=(cwd,args,input)=>execFileSync('/usr/bin/git',['-c','core.hooksPath=/dev/null',...args],{cwd,input,encoding:'utf8',env:{PATH:'/usr/bin:/bin',HOME:'/nonexistent',GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null'}}).trim();
 git(source,['init','--initial-branch=main','--template=']);await fs.writeFile(join(source,'README.md'),'original source\n');git(source,['add','.']);git(source,['-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-m','base']);const base=git(source,['rev-parse','HEAD']);f.setBase(base);git(context.root,['clone','--bare','--no-hardlinks',source,remote]);
 const desired={files:new Map([['.nibfin/runtime.json','reviewed source\n']])},observations=new Map([['.nibfin/runtime.json',{state:'absent'}]]);let pr=null,posts=0,opens=0;
 const reply=data=>({status:200,body:JSON.stringify(data)});f.intercept(async r=>{
  if(r.endpoint==='/repos/example/project/pulls?state=all&per_page=100')return reply(pr?[pr]:[]);
  if(r.endpoint==='/repos/example/project/pulls'&&r.method==='POST'){posts++;const value=JSON.parse(r.body);pr={id:'600',number:60,html_url:'https://github.com/example/project/pull/60',head:{sha:git(remote,['rev-parse','refs/heads/'+value.head]),ref:value.head,repo:{id:'100'}},base:{sha:base,ref:'main'},state:'open',draft:false,merged:false};return reply(pr);}
  if(r.endpoint==='/repos/example/project/pulls/60')return reply(pr);
 });
 const selected={...core,observeGithubCI:async()=>({status:'pending',qualifies:false})},provider=core.createGithubProvider({transport:f.transport,attempts:1,authorize:async()=>true});
 const options={root:f.root,repository:'example/project',repositoryId:'100',desired,observations,provider,checkout:async(_repository,commit,path)=>{git(path,['clone','--no-hardlinks',source,'.']);git(path,['checkout','--detach',commit]);},publicationGitFactory:input=>({open(...args){opens++;assert.deepEqual(args[0].candidate.paths,['.nibfin/runtime.json']);if(opens===1)throw Error('synthetic-before-push-interruption');return core.createPublicationGit({...input,localFixture:remote}).open(...args);}})};
 await assert.rejects(installNativeSource(selected,options),/synthetic-before-push-interruption/);const original=JSON.parse(await fs.readFile(join(f.root,'managed-source-pr.json'),'utf8'));assert.equal(original.phase,'planning');
 assert.equal((await installNativeSource(selected,options)).status,'pending');const current=JSON.parse(await fs.readFile(join(f.root,'managed-source-pr.json'),'utf8'));assert.equal(current.identity,original.identity);assert.equal(current.base,original.base);assert.equal(current.branch,original.branch);assert.equal(posts,1);assert.equal(git(remote,['show',current.commit+':README.md']),'original source');
 assert.equal((await installNativeSource(selected,options)).status,'pending');assert.equal(opens,2);assert.equal(posts,1);await assert.rejects(fs.stat(original.directory),{code:'ENOENT'});
}));

test('canonical numeric pagination remains limited to the exact named repository and preserves write denial',async()=>withCurrentCore(async core=>{
 for(const next of['100','999']){
  const calls=[],transport=async request=>{calls.push(request.endpoint);return request.endpoint.startsWith('/repos/')?{status:200,headers:{link:'<https://api.github.com/repositories/'+next+'/pulls?state=all&per_page=100&page=2>; rel="next"'},body:'[{"id":"1"}]'}:{status:200,body:'[{"id":"2"}]'};};
  const provider=nativeGithubProvider(core,{transport,attempts:1}),result=await provider.collection({endpoint:'/repos/example/project/pulls?state=all&per_page=100',target:'100',operation:'installation',correlation:'test',allowPage:path=>path.split('?')[0]==='/repos/example/project/pulls'});
  if(next==='100'){assert.equal(result.outcome,'complete');assert.deepEqual(result.data.map(v=>v.id),['1','2']);assert.equal(calls.length,2);}else{assert.equal(result.outcome,'incomplete');assert.equal(calls.length,1);}
  const before=calls.length;assert.equal((await provider.request({method:'POST',endpoint:'/repos/example/project/pulls',target:'100',operation:'installation',correlation:'test',body:{}})).outcome,'failed');assert.equal(calls.length,before);
 }
}));

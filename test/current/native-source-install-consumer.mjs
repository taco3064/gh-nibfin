import test from'node:test';import assert from'node:assert/strict';import{join}from'node:path';
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

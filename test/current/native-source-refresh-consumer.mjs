import test from'node:test';import assert from'node:assert/strict';import{join}from'node:path';
import{withCurrentCore}from'../../src/current-core.mjs';import{nativeCycleFixture}from'./native-cycle-fixture.mjs';
import{readPrivate,writePrivate,sha}from'../../src/local-state.mjs';import{refreshNativeSource}from'../../src/native-source-refresh.mjs';

test('same-PR source refresh retains original intent, sends once and requires parentage plus exact managed bytes',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativeCycleFixture(core,context),path=join(f.root,'managed-source-pr.json'),oldBase='b'.repeat(40),main='c'.repeat(40),oldHead='d'.repeat(40),head='e'.repeat(40),name='.nibfin/runtime.json',desired={files:new Map([[name,'reviewed bytes']])};
 const original={version:1,identity:'a'.repeat(64),base:oldBase,commit:oldHead,branch:'nibfin/install-original',createdAt:1,phase:'pr',files:{[name]:sha('reviewed bytes')},pr:{id:'400',number:'40',url:'https://github.com/example/project/pull/40'},ci:{old:true}};await writePrivate(path,original);
 let visible=false,conflict=true,badParent=false,badBytes=false,writes=0;
 const pr=()=>({id:'400',state:'open',merged:false,draft:false,head:{sha:visible?head:oldHead,ref:original.branch,repo:{id:'100'}}}),reply=(data,status=200)=>({status,body:JSON.stringify(data)});
 f.intercept(async r=>{
  const root='/repos/example/project';if(r.endpoint===root+'/git/ref/heads/main')return reply({object:{sha:main}});
  if(r.endpoint===root+'/pulls/40')return reply(pr());
  if(r.endpoint===root+'/compare/'+oldBase+'...'+main)return reply({base_commit:{sha:oldBase},merge_base_commit:{sha:oldBase},status:'ahead',files:[{filename:conflict?name:'README.md',status:'modified'}]});
  if(r.endpoint===root+'/pulls/40/update-branch'){writes++;assert.deepEqual(JSON.parse(r.body),{expected_head_sha:oldHead});return reply({message:'Updating pull request branch'},202);}
  if(r.endpoint===root+'/commits/'+head)return reply({sha:head,parents:[{sha:badParent?'f'.repeat(40):oldHead},{sha:main}],commit:{tree:{sha:'1'.repeat(40)}}});
  if(r.endpoint===root+'/compare/'+main+'...'+head)return reply({base_commit:{sha:main},merge_base_commit:{sha:main},status:'ahead',files:[{filename:name,status:'added'}]});
  if(r.endpoint===root+'/contents/'+name+'?ref='+head)return reply({type:'file',path:name,encoding:'base64',content:Buffer.from(badBytes?'unexpected':'reviewed bytes').toString('base64')});
 });
 const provider=core.createGithubProvider({transport:f.transport,attempts:1,authorize:async()=>true}),run=async()=>refreshNativeSource(core,{proposal:await readPrivate(path),pr:pr(),path,desired,repository:'example/project',repositoryId:'100',provider});
 await assert.rejects(run(),/managed-base-conflict/);assert.equal(writes,0);conflict=false;
 assert.equal((await run()).pending,true);assert.equal(writes,1);assert.equal((await run()).pending,true);assert.equal(writes,1);
 visible=true;badParent=true;await assert.rejects(run(),/parentage/);badParent=false;badBytes=true;await assert.rejects(run(),/managed-bytes/);badBytes=false;
 const result=await run();assert.equal(result.pending,false);assert.equal(result.proposal.identity,original.identity);assert.equal(result.proposal.branch,original.branch);assert.equal(result.proposal.createdAt,original.createdAt);assert.equal(result.proposal.base,main);assert.equal(result.proposal.commit,head);assert.equal(result.proposal.ci,undefined);assert.equal(result.proposal.refresh,undefined);assert.equal(result.proposal.revisions[0].fromHead,oldHead);assert.equal(writes,1);
}));

import {test}from'node:test';import assert from'node:assert/strict';
import fs from'node:fs/promises';import{join}from'node:path';
import{withCurrentCore}from'../../src/current-core.mjs';
import{nativeControlHost}from'../../src/native-host.mjs';
import{initializeRepository}from'../../src/installation.mjs';
const pool=()=>({version:1,source:{version:1,repository:'example/core',repositoryId:'42',ownerId:'7',ownerType:'User'},
 targets:['core','cli','action'].map((name,i)=>({version:1,repository:'example/'+name,repositoryId:String(42+i),ownerId:'7',ownerType:'User'}))});

test('first-party staging retains actual installed runtime and exactly two unregistered Runner applications; check never stages or starts',async()=>{
 await withCurrentCore(async(core,context)=>{
  const root=join(context.root,'native-control');let downloads=0,verifications=0;
  // Explicit application-acquisition doubles. Runtime files and private filesystem
  // are real; this test does not establish official Runner or service readiness.
  const prepare=async(directory,{archive}={})=>{if(!archive)downloads++;await fs.writeFile(join(directory,'config.sh'),'PINNED RUNNER FIXTURE',{flag:'wx',mode:0o600});};
  const verify=async directory=>{verifications++;assert.equal(await fs.readFile(join(directory,'config.sh'),'utf8'),'PINNED RUNNER FIXTURE');};
  const service={check:async()=>({status:'blocked',diagnostics:[{capability:'host',code:'fixture-control-pending'}]}),
   install:async()=>({status:'running',diagnostics:[{capability:'host',code:'native-delivery-composition-pending'}]})};
  const host=nativeControlHost({root,prepare,verify,service}),request={core,context,executionPool:pool()};
  assert.equal((await host.check(request)).diagnostics[0].code,'native-host-not-staged');
  await assert.rejects(fs.stat(root),{code:'ENOENT'});assert.equal(downloads,0);
  assert.equal((await host.prepare(request)).status,'staged');assert.equal(downloads,1);
  const record=JSON.parse(await fs.readFile(join(root,'native-installation.json')));
  assert.equal(record.runnerCount,2);assert.deepEqual(record.executionPool,pool());
  assert.equal(await fs.readFile(join(root,'runtime/node_modules/@nibfin/core/src/index.mjs'),'utf8'),context.files.get('src/index.mjs').toString());
  assert.ok(record.runtimeInventory.some(f=>f.path==='client/native-host.mjs'));
  const checked=await host.check(request);assert.equal(checked.status,'blocked');assert.equal(checked.phase,'staged');
  assert.ok(checked.diagnostics.some(d=>d.code==='native-runner-registrations-pending'));
  const before=await fs.readFile(join(root,'native-installation.json'));assert.equal((await host.prepare(request)).status,'staged');
  assert.equal(downloads,1);assert.deepEqual(await fs.readFile(join(root,'native-installation.json')),before);assert.ok(verifications>=4);
  for(const name of ['runner-1','runner-2'])await assert.rejects(fs.stat(join(root,name,'.credentials')),{code:'ENOENT'});
  const changed=pool();changed.targets.pop();await assert.rejects(host.prepare({...request,executionPool:changed}),/preserve-existing-installation/);
  assert.deepEqual(await fs.readFile(join(root,'native-installation.json')),before);
  await fs.writeFile(join(root,'runtime/client/user-work'),'preserve');
  assert.equal((await host.check(request)).diagnostics[0].code,'native-installation-reconciliation-required');
  await assert.rejects(host.prepare(request),/runtime-file/);assert.equal(await fs.readFile(join(root,'runtime/client/user-work'),'utf8'),'preserve');
 });
});

test('native preparation is completed before any repository mutation; incomplete local wiring cannot publish active workflows',async()=>{
 await withCurrentCore(async(core,context)=>{
  const root=join(context.root,'installations');let prepared=0,writes=0;
  const host={check:async()=>({status:'blocked',diagnostics:[{capability:'host',code:'not-staged'}]}),
   prepare:async()=>{prepared++;return{status:'staged',diagnostics:[{capability:'host',code:'control-pending'}]};},
   install:async()=>assert.fail('must not activate incomplete host')};
  const transport=async r=>{
   if(r.method!=='GET'){writes++;assert.fail('must not publish before local host preparation');}
   const name=r.endpoint.slice(7),target=pool().targets.find(t=>t.repository===name);
   if(target)return{status:200,headers:{},body:JSON.stringify({id:target.repositoryId,full_name:name,private:false,visibility:'public',owner:{id:'7',type:'User'},permissions:{admin:true}})};
   if(r.endpoint.endsWith('/labels?per_page=100'))return{status:200,headers:{},body:'[]'};
   return{status:404,headers:{},body:'{}'};
  };
  const args=['--repo','example/core','--execution-repo','example/core','--pool-repos','example/core,example/cli,example/action'];
  const dependencies={core,context,transport,memberId:'8',root,host};
  assert.equal((await initializeRepository([...args,'--check'],dependencies)).status,'blocked');assert.equal(prepared,0);
  const result=await initializeRepository(args,dependencies);assert.equal(result.phase,'staged');assert.equal(result.status,'blocked');
  assert.equal(prepared,1);assert.equal(writes,0);assert.equal(result.diagnostics[0].code,'control-pending');
 });
});

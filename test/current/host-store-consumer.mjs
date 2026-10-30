import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {execFileSync} from 'node:child_process';
import {withCurrentCore} from '../../src/current-core.mjs';
import {initializeFileHostStore,openFileHostStore,managedHostRecords} from '../../src/host-store.mjs';
const repositories=[{repository:'example/a',repositoryId:'42'},{repository:'example/b',repositoryId:'43'}];
const installation=context=>({packageVersion:context.identity.packageVersion,pins:context.identity.pins});
const clone=structuredClone;
async function fixture(consume){
  return withCurrentCore(async(core,context)=>{
    const directory=join(context.root,'host-state'),current=installation(context);
    const store=await initializeFileHostStore(core,{directory,installation:current,repositories});
    try{await consume({core,context,directory,current,store});}finally{await store.close();}
  });
}
const safeWork={restrictionsChecked:true,hold:null,paused:false,unknownWrite:false,active:false,terminated:true,reconciled:true,
  conversations:['original-session'],pending:[{workspace:'original',status:'UNACCEPTED'}],receipts:[{id:'historical',identity:'a'.repeat(64)}]};
const activate=async store=>{const state=await store.read();await store.updateWork({expected:state.identity,work:clone(safeWork)});};

test('real private host state survives reopen and cross-process CAS writes wake filesystem subscriptions without polling',()=>fixture(async({core,context,directory,store})=>{
  assert.equal((await store.read()).work.paused,true);
  const records=store.rescue('42');let received;
  const changed=new Promise((resolve,reject)=>{const timer=setTimeout(()=>reject(Error('No filesystem notification.')),5000);
    received=async()=>{try{if((await records.read()).budgets.child===2){clearTimeout(timer);resolve();}}catch(error){clearTimeout(timer);reject(error);}};});
  const unsubscribe=await records.subscribe(received);
  try{
    const script=`import * as core from ${JSON.stringify(pathToFileURL(join(context.root,'consumer.mjs')).href)};
      import {openFileHostStore} from ${JSON.stringify(new URL('../../src/host-store.mjs',import.meta.url).href)};
      const store=await openFileHostStore(core,{directory:${JSON.stringify(directory)}});
      try{await store.rescue('42').update(v=>{v.budgets.child=2;return v;});}finally{await store.close();}`;
    execFileSync(process.execPath,['--input-type=module','-e',script],{timeout:15000,env:{PATH:process.env.PATH,HOME:context.root,LANG:'C'}});
    await changed;
    assert.equal((await store.rescue('43').read()).revision,0);
    const reopened=await openFileHostStore(core,{directory});
    try{assert.equal((await reopened.rescue('42').read()).budgets.child,2);
      const before=await reopened.read();await store.updateWork({expected:before.identity,work:clone(safeWork)});
      await assert.rejects(reopened.updateWork({expected:before.identity,work:{...safeWork,conversations:[]}}),/compare-and-swap/);
      assert.deepEqual((await reopened.read()).work,safeWork);
    }finally{await reopened.close();}
  }finally{unsubscribe();}
}));

test('installed transition owner uses durable intent and atomic complete pin selection; explicit rollback preserves original work',()=>fixture(async({core,current,store,directory})=>{
  await activate(store);
  const next={...current,pins:{...current.pins,runtime:'b'.repeat(64),source:'b'.repeat(64)}};
  let verifications=0;
  // Metadata-coordinator fixture only: no runtime archive, service upgrade or
  // platform support is certified by this trusted verifier double.
  const capabilities=store.transitionCapabilities({verifyInstallation:async selected=>{verifications++;return {status:'verified',installation:selected};}});
  const result=await core.applyInstallationTransition({current,next,...capabilities});
  assert.equal(result.status,'switched');assert.equal(verifications,1);
  const state=await store.read();assert.deepEqual(state.installation,next);assert.deepEqual(state.work,safeWork);
  assert.equal(state.transitions[result.intent].status,'applied');
  assert.equal((await core.applyInstallationTransition({current:next,next:current,...capabilities})).status,'switched');
  assert.deepEqual((await store.read()).installation,current);assert.deepEqual((await store.read()).work,safeWork);
  const reopened=await openFileHostStore(core,{directory});
  try{
    const replay=await core.applyInstallationTransition({current,next,...reopened.transitionCapabilities({verifyInstallation:()=>assert.fail('Old switch must not be replayed')})});
    assert.equal(replay.status,'unknown');assert.deepEqual((await reopened.read()).installation,current);
  }finally{await reopened.close();}
}));

test('original pause, active work, holds and unknown writes block; a crash after retaining intent never retries selection',()=>fixture(async({core,current,store,directory})=>{
  const next={...current,pins:{...current.pins,skills:'c'.repeat(64)}};
  const forbidden=store.transitionCapabilities({verifyInstallation:()=>assert.fail('Blocked work must not reach verifier')});
  for(const patch of [{paused:true},{active:true},{hold:{id:'owner'}},{unknownWrite:true},{reconciled:false},{restrictionsChecked:false}]){
    const state=await store.read();await store.updateWork({expected:state.identity,work:{...clone(safeWork),...patch}});
    const before=await fs.readFile(join(directory,'store.json'));
    assert.equal((await core.applyInstallationTransition({current,next,...forbidden})).status,'blocked');
    assert.deepEqual(await fs.readFile(join(directory,'store.json')),before);
  }
  await activate(store);
  const plan=core.planInstallationTransition({version:1,current,next,work:safeWork});
  const intent={version:1,kind:'installation-switch',identity:core.releaseHash(core.canonicalJSON(plan)),plan};
  assert.equal((await store.retainTransition(intent)).prior,'new');
  const reopened=await openFileHostStore(core,{directory});
  try{const outcome=await core.applyInstallationTransition({current,next,...reopened.transitionCapabilities({verifyInstallation:()=>assert.fail('Unresolved intent cannot repeat a switch')})});
    assert.equal(outcome.status,'unknown');assert.deepEqual((await reopened.read()).work,safeWork);assert.deepEqual((await reopened.read()).installation,current);
  }finally{await reopened.close();}
}));

test('failed runtime verification cannot change pins; existing, oversized, corrupt and missing records are preserved',()=>fixture(async({core,current,store,directory})=>{
  await assert.rejects(initializeFileHostStore(core,{directory,installation:current,repositories}),{code:'EEXIST'});
  await activate(store);const before=await store.read();
  await assert.rejects(store.updateWork({expected:before.identity,work:{...safeWork,pending:['x'.repeat(1100000)]}}),/size/);
  assert.deepEqual(await store.read(),before);
  const next={...current,pins:{...current.pins,recovery:'d'.repeat(64)}};
  const outcome=await core.applyInstallationTransition({current,next,...store.transitionCapabilities({verifyInstallation:async()=>({status:'unknown'})})});
  assert.equal(outcome.status,'failed');assert.deepEqual((await store.read()).installation,current);
  const path=join(directory,'store.json'),original=await fs.readFile(path);
  await fs.writeFile(path,original.toString().replace('original-session','changed-session'));
  await assert.rejects(openFileHostStore(core,{directory}),/integrity/);
  await fs.writeFile(path,original);
  await fs.rename(path,join(directory,'retained-original.json'));
  await assert.rejects(openFileHostStore(core,{directory}),{code:'ENOENT'});
  await assert.rejects(initializeFileHostStore(core,{directory,installation:current,repositories}),{code:'EEXIST'});
  assert.deepEqual((await fs.readdir(directory)).sort(),['.host-store.lock','retained-original.json']);
  assert.equal(await fs.readFile(join(directory,'.host-store.lock'),'utf8'),'');
}));

const policy=(repository,repositoryId)=>({version:1,repository,repositoryId,visibility:'public',ref:'main',stageWorkflow:'.github/workflows/nibfin-stage.yml',
  stageBlob:'a'.repeat(40),rescueBlob:'b'.repeat(40),actorIds:['7'],historical:[],maxRepairs:3,cooldownMs:30000,jobTimeoutMinutes:60,authority:'a'.repeat(64),expiresAt:Date.now()+60000});
test('managed host composition allocates record callbacks once; check cannot initialize, and external original ownership is retained',async()=>withCurrentCore(async(core,context)=>{
  const directory=join(context.root,'host');await fs.mkdir(directory,{mode:0o700});
  const hosts=repositories.map(r=>({configuration:{policy:policy(r.repository,r.repositoryId),current:async()=>({original:true})}})),input={directory,hosts,installation:installation(context)};
  await assert.rejects(managedHostRecords(core,input),/missing/);
  assert.deepEqual(await fs.readdir(directory),[]);
  let bound=await managedHostRecords(core,{...input,initialize:true});
  try{
    await assert.rejects(bound.hosts[0].configuration.current({}),/intentional-pause/);
    await bound.hosts[0].configuration.records.update(v=>{v.notifications.retained={status:'accepted'};return v;});
    const owner=await openFileHostStore(core,{directory:join(directory,'state')});
    try{await activate(owner);}finally{await owner.close();}
    assert.deepEqual(await bound.hosts[0].configuration.current({}),{original:true});
  }finally{await bound.close();}
  bound=await managedHostRecords(core,input);
  try{assert.deepEqual((await bound.hosts[0].configuration.records.read()).notifications.retained,{status:'accepted'});}finally{await bound.close();}
  await assert.rejects(managedHostRecords(core,{...input,installation:{...input.installation,pins:{...input.installation.pins,source:'e'.repeat(64)}}}),/identity/);
  const records={read:async()=>({original:true})},legacy=hosts.map(entry=>({...entry,configuration:{...entry.configuration,records}}));
  bound=await managedHostRecords(core,{hosts:legacy});assert.equal(bound.hosts[0].configuration.records,records);await bound.close();
  await assert.rejects(managedHostRecords(core,{...input,hosts:[legacy[0],hosts[1]]}),/mixed-record-ownership/);
}));

import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {withCurrentCore} from '../../src/current-core.mjs';
import {initializeFileHostStore} from '../../src/host-store.mjs';
import {coordinateUpgrade,rollbackUpgrade,upgradeOptions} from '../../src/upgrade.mjs';

// Actual private filesystem and installed Core/store. Service observations are
// explicit doubles; these tests do not certify a real Runner or systemd upgrade.
async function fixture(consume){return withCurrentCore(async(core,context)=>{
  const directory=join(context.root,'maintenance');await fs.mkdir(directory,{mode:0o700});
  const current={packageVersion:context.identity.packageVersion,pins:context.identity.pins};
  const next={...current,pins:{...current.pins,runtime:'b'.repeat(64),source:'c'.repeat(64)}};
  const store=await initializeFileHostStore(core,{directory:join(directory,'state'),installation:current,
    repositories:[{repository:'example/a',repositoryId:'42'}]});
  const before=await store.read(),work={...before.work,conversations:['original-session'],
    pending:[{id:'original-candidate',status:'UNACCEPTED'}],receipts:[{id:'accepted-history',identity:'d'.repeat(64)}]};
  await store.updateWork({expected:before.identity,work});
  const resources=[];
  for(const name of ['runtime','runner-1','runner-2']){
    const r={name,current:join(directory,name),next:join(directory,name+'-next'),previous:join(directory,name+'-previous')};
    for(const [position,version] of [['current','original'],['next','updated']]){
      await fs.mkdir(r[position],{mode:0o700});await fs.writeFile(join(r[position],'version'),version,{mode:0o600});
    }resources.push(r);
  }
  const services=['nibfin-control.service','nibfin-runner-1.service','nibfin-runner-2.service'];
  const active=new Set(services),calls=[],flags={};
  const manager={observe:async unit=>({active:active.has(unit),stopped:!active.has(unit)}),
    stop:async unit=>{calls.push(['stop',unit]);active.delete(unit);},
    start:async unit=>{calls.push(['start',unit]);if(flags.startFails)throw Error('Start failed');active.add(unit);},
    reload:async()=>{calls.push(['reload']);}};
  const verify=async(r,position)=>{
    if(flags.activationFails&&position==='activated')return false;
    const path=r[position==='activated'?'current':position];
    return (await fs.readFile(join(path,'version'),'utf8'))===(['current','previous'].includes(position)?'original':'updated');
  };
  const options={core,directory,store,current,next,resources,services,verify,manager};
  try{await consume({...options,options,work,calls,active,flags});}finally{await store.close();}
});}

test('coordinated upgrade and rollback preserve pause, pending work and sessions; a second upgrade has a fresh intent',()=>fixture(async f=>{
  const snapshot=await fs.readFile(join(f.directory,'state/store.json'));
  assert.equal((await coordinateUpgrade({...f.options,check:true})).status,'checked');
  assert.deepEqual(await fs.readFile(join(f.directory,'state/store.json')),snapshot);assert.equal(f.calls.length,0);
  for(let n=0;n<2;n++){
    assert.equal((await coordinateUpgrade(f.options)).status,'switched');
    let state=await f.store.read();assert.deepEqual(state.installation,f.next);assert.deepEqual(state.work,f.work);
    assert.equal(f.active.size,3);
    assert.equal((await rollbackUpgrade(f.options)).status,'rolled-back');
    state=await f.store.read();assert.deepEqual(state.installation,f.current);assert.deepEqual(state.work,f.work);
    const count=f.calls.length;
    assert.equal((await rollbackUpgrade(f.options)).already,true);assert.equal(f.calls.length,count);
  }
  assert.equal(Object.keys((await f.store.read()).transitions).length,4);
}));

test('an unknown activation stops services, preserves both generations, rejects replay and rolls back the exact original journal',()=>fixture(async f=>{
  f.flags.activationFails=true;
  const result=await coordinateUpgrade(f.options);assert.equal(result.status,'unknown');assert.equal(result.servicesStopped,true);
  assert.equal(f.active.size,0);assert.deepEqual((await f.store.read()).installation,f.current);
  assert.deepEqual((await f.store.read()).work,f.work);
  const journal=await fs.readFile(join(f.directory,'upgrade.json'));
  await assert.rejects(coordinateUpgrade(f.options),/reconciliation-required/);
  assert.deepEqual(await fs.readFile(join(f.directory,'upgrade.json')),journal);
  f.flags.activationFails=false;assert.equal((await rollbackUpgrade(f.options)).status,'rolled-back');
  assert.deepEqual((await f.store.read()).work,f.work);assert.equal(f.active.size,3);
}));

test('unsafe work never reaches service or filesystem effects; check is read-only and option parsing is finite',()=>fixture(async f=>{
  for(const patch of [{paused:false},{unknownWrite:true},{active:true},{terminated:false},{reconciled:false},{hold:{id:'owner-hold'}}]){
    const state=await f.store.read();await f.store.updateWork({expected:state.identity,work:{...f.work,...patch}});
    await assert.rejects(coordinateUpgrade(f.options),/unsafe-work-boundary/);assert.equal(f.calls.length,0);
    await assert.rejects(fs.lstat(join(f.directory,'upgrade.json')),{code:'ENOENT'});
    for(const r of f.resources)assert.equal(await f.verify(r,'current'),true);
  }
  assert.deepEqual(upgradeOptions([]),{operation:'check'});
  assert.deepEqual(upgradeOptions(['--rollback']),{operation:'rollback'});
  for(const args of [['--apply','--rollback'],['--force'],['--apply','other']])assert.throws(()=>upgradeOptions(args));
}));

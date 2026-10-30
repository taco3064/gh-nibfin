import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {withCurrentCore} from '../../src/current-core.mjs';
import {initializeFileHostStore} from '../../src/host-store.mjs';
import {coordinateUpgrade,rollbackUpgrade,upgradeOptions,maintenanceLock} from '../../src/upgrade.mjs';
import {managedUpgradeLayout,upgradeManagedInstallation} from '../../src/managed-upgrade.mjs';
import {writePrivate} from '../../src/local-state.mjs';

// Actual private filesystem and installed Core/store. Service observations are
// explicit doubles; these tests do not certify a real Runner or systemd upgrade.
async function fixture(consume,count=2){return withCurrentCore(async(core,context)=>{
  const directory=join(context.root,'maintenance');await fs.mkdir(directory,{mode:0o700});
  const current={packageVersion:context.identity.packageVersion,pins:context.identity.pins};
  const next={...current,pins:{...current.pins,runtime:'b'.repeat(64),source:'c'.repeat(64)}};
  const store=await initializeFileHostStore(core,{directory:join(directory,'state'),installation:current,
    repositories:[{repository:'example/a',repositoryId:'42'}]});
  const before=await store.read(),work={...before.work,conversations:['original-session'],
    pending:[{id:'original-candidate',status:'UNACCEPTED'}],receipts:[{id:'accepted-history',identity:'d'.repeat(64)}]};
  await store.updateWork({expected:before.identity,work});
  const resources=[];
  const names=['runtime',...Array.from({length:count},(_,i)=>'runner-'+(i+1))];
  for(const name of names){
    const r={name,current:join(directory,name),next:join(directory,name+'-next'),previous:join(directory,name+'-previous')};
    for(const [position,version] of [['current','original'],['next','updated']]){
      await fs.mkdir(r[position],{mode:0o700});await fs.writeFile(join(r[position],'version'),version,{mode:0o600});
    }resources.push(r);
  }
  const services=['nibfin-control.service',...names.slice(1).map(n=>'nibfin-'+n+'.service')];
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
  const resource={name:'state',current:f.directory,next:f.directory+'-next',previous:f.directory+'-previous'};
  await assert.rejects(coordinateUpgrade({...f.options,resources:[resource]}),/journal-resource-overlap/);
}));

test('a killed coordinator releases its kernel lease without deleting the private lock file or resetting its journal',()=>fixture(async f=>{
  const module=new URL('../../src/upgrade.mjs',import.meta.url).href;
  const script=`import{maintenanceLock}from ${JSON.stringify(module)};await maintenanceLock(${JSON.stringify(f.directory)},async()=>{
    process.stdout.write('acquired\\n');await new Promise(()=>{});
  });`;
  const child=spawn(process.execPath,['--input-type=module','-e',script],{env:{PATH:process.env.PATH,HOME:f.directory,LANG:'C'},stdio:['ignore','pipe','pipe']});
  const ended=new Promise(resolve=>child.once('close',resolve));
  try{
    await new Promise((resolve,reject)=>{
      const timer=setTimeout(()=>reject(Error('Child lease did not acquire')),10000);
      child.stdout.once('data',bytes=>{clearTimeout(timer);assert.equal(bytes.toString(),'acquired\n');resolve();});
      child.once('error',error=>{clearTimeout(timer);reject(error);});
      ended.then(()=>{clearTimeout(timer);reject(Error('Child lease ended early'));});
    });
    const path=join(f.directory,'maintenance.lock'),before=await fs.stat(path);
    await assert.rejects(maintenanceLock(f.directory,()=>assert.fail('Concurrent lease acquired')),/maintenance-lock-unavailable/);
    child.kill('SIGKILL');await ended;
    let acquired=false;
    for(let n=0;n<100&&!acquired;n++){
      try{await maintenanceLock(f.directory,()=>{acquired=true;});}
      catch(e){if(!e.message.includes('maintenance-lock-unavailable'))throw e;await new Promise(resolve=>setTimeout(resolve,20));}
    }
    assert.equal(acquired,true);const after=await fs.stat(path);assert.equal(after.ino,before.ino);
    assert.deepEqual((await f.store.read()).work,f.work);
  }finally{child.kill('SIGKILL');await ended;}
}));

test('the maintained upgrade entry uses a finite data-only layout and checks real staged bytes before switching',()=>fixture(async f=>{
  const inventory=text=>[{kind:'file',path:'version',mode:0o600,sha256:createHash('sha256').update(text).digest('hex')}];
  const body={version:1,current:f.current,next:f.next,resources:f.resources.map(r=>({name:r.name,current:inventory('original'),next:inventory('updated')}))};
  const plan={...body,identity:f.core.contentIdentity(body)};
  await writePrivate(join(f.directory,'upgrade-plan.json'),plan);
  const options={core:f.core,context:{identity:f.next},root:f.directory,manager:f.options.manager};
  assert.equal((await upgradeManagedInstallation(['--check'],options)).status,'checked');assert.equal(f.calls.length,0);
  const layout=managedUpgradeLayout(f.core,f.directory,plan);
  assert.deepEqual(layout.resources,f.resources);assert.deepEqual(layout.services,f.services);
  const hostile=structuredClone(body);hostile.resources[0].path='/etc';
  assert.throws(()=>managedUpgradeLayout(f.core,f.directory,{...hostile,identity:f.core.contentIdentity(hostile)}),/resource-set/);
  await fs.writeFile(join(f.resources[0].next,'extra.mjs'),'unreviewed',{mode:0o600});
  await assert.rejects(upgradeManagedInstallation(['--apply'],options),/unknown-file/);assert.equal(f.calls.length,0);
  await fs.rm(join(f.resources[0].next,'extra.mjs'));
  assert.equal((await upgradeManagedInstallation(['--apply'],options)).status,'switched');
  assert.equal((await upgradeManagedInstallation(['--rollback'],options)).status,'rolled-back');
  assert.deepEqual((await f.store.read()).work,f.work);
}));

test('six-runner upgrade stops and switches every slot and rolls back without losing original work',()=>fixture(async f=>{
 const inventory=text=>[{kind:'file',path:'version',mode:0o600,sha256:createHash('sha256').update(text).digest('hex')}];
 const body={version:1,current:f.current,next:f.next,resources:f.resources.map(r=>({name:r.name,current:inventory('original'),next:inventory('updated')}))};
 const plan={...body,identity:f.core.contentIdentity(body)},layout=managedUpgradeLayout(f.core,f.directory,plan);
 assert.equal(layout.resources.length,7);assert.equal(layout.services.length,7);
 assert.equal((await coordinateUpgrade({...f.options,...layout})).status,'switched');assert.equal(f.active.size,7);
 assert.deepEqual(f.calls.filter(c=>c[0]==='stop').map(c=>c[1]),f.services);
 assert.equal((await rollbackUpgrade({...f.options,...layout})).status,'rolled-back');assert.equal(f.active.size,7);
 assert.deepEqual((await f.store.read()).work,f.work);
 const hostile=structuredClone(body);hostile.resources[6].name='runner-99';
 assert.throws(()=>managedUpgradeLayout(f.core,f.directory,{...hostile,identity:f.core.contentIdentity(hostile)}),/resource-set/);
},6));

test('native metadata file and runtime directory switch and roll back at one protected service boundary',()=>fixture(async f=>{
 const metadata={name:'installation',kind:'file',current:join(f.directory,'native-installation.json'),next:join(f.directory,'native-installation-next.json'),previous:join(f.directory,'native-installation-previous.json')};
 for(const [key,body]of [['current','original metadata'],['next','updated metadata']])await fs.writeFile(metadata[key],body,{mode:0o600});
 const verify=async(r,position)=>r.name!=='installation'?f.verify(r,position):
  await fs.readFile(r[position==='activated'?'current':position],'utf8')===(['current','previous'].includes(position)?'original metadata':'updated metadata');
 const options={...f.options,resources:[...f.resources,metadata],verify};
 assert.equal((await coordinateUpgrade({...options,check:true})).status,'checked');assert.equal(f.calls.length,0);
 assert.equal((await coordinateUpgrade(options)).status,'switched');assert.equal(await fs.readFile(metadata.current,'utf8'),'updated metadata');
 assert.equal(f.active.size,1);assert.deepEqual((await f.store.read()).work,f.work);
 assert.equal((await rollbackUpgrade(options)).status,'rolled-back');assert.equal(await fs.readFile(metadata.current,'utf8'),'original metadata');
 assert.deepEqual((await f.store.read()).work,f.work);
},0));

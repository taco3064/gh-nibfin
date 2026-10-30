import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join,dirname} from 'node:path';
import {withCurrentCore} from '../../src/current-core.mjs';
import {nativeControlHost,nativeRuntimeInputs} from '../../src/native-host.mjs';
import {archiveCompletedNativeUpgrade} from '../../src/native-upgrade-history.mjs';
import {stageUnactivatedNativeUpgrade} from '../../src/native-upgrade-stage.mjs';
import {upgradeManagedInstallation,nativeInstallationSelection,managedUpgradeLayout} from '../../src/managed-upgrade.mjs';
import {openFileHostStore} from '../../src/host-store.mjs';
import {writePrivate,readPrivate,sha} from '../../src/local-state.mjs';

async function fixture(core,context){
 const root=join(context.root,'native-upgrade'),pool={version:2,id:'member-default',capacity:2,
  targets:[{version:1,repository:'example/project',repositoryId:'42',ownerId:'7',ownerType:'User'}]};
 // Only acquisition/systemd/resource observation are doubles. Both installed
 // Core generations, byte inventories, kernel locks, store and renames are real.
 const verify=async(path,options)=>{assert.equal(await fs.readFile(join(path,'fixture'),'utf8'),'runner');};
 const service={install:async()=>({diagnostics:[]}),check:async()=>({diagnostics:[]}),stageUnit:async()=>({status:'linked'})};
 await nativeControlHost({root,service,verify,prepare:async(path)=>fs.writeFile(join(path,'fixture'),'runner',{mode:0o600})})
  .prepare({core,context,executionPool:pool});
 const next=await readPrivate(join(root,'native-installation.json')),previous=structuredClone(next);
 const path='client/native-host.mjs',changed=Buffer.concat([await fs.readFile(join(root,'runtime',path)),Buffer.from('\n// earlier reviewed CLI generation\n')]);
 await fs.writeFile(join(root,'runtime',path),changed);
 previous.runtimeInventory.find(f=>f.path===path).sha256=sha(changed);await writePrivate(join(root,'native-installation.json'),previous);
 const files=await nativeRuntimeInputs(context);
 const stage=async()=>{const target=join(root,'runtime-next');await fs.mkdir(target,{mode:0o700});
  for(const [path,bytes]of files){await fs.mkdir(dirname(join(target,path)),{recursive:true,mode:0o700});await fs.writeFile(join(target,path),bytes,{mode:0o600});}};
 const args={root,previous,next,context,stage,control:service,verifyRunner:verify,observeResources:async()=>({status:'unused'})};
 let active=false;const calls=[],manager={stop:async u=>{calls.push(['stop',u]);active=false;},start:async u=>{calls.push(['start',u]);active=true;},
  observe:async()=>({active,stopped:!active}),reload:async()=>{}};
 return {root,pool,previous,next,args,calls,manager};
}

test('CLI-only native generation uses the existing upgrade owner and keeps original work during rollback',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context),original=await fs.readFile(join(f.root,'native-installation.json'));
 const external=join(context.root,'unaccepted-original');await fs.writeFile(external,'ORIGINAL',{mode:0o600});
 assert.equal((await stageUnactivatedNativeUpgrade(core,f.args)).status,'staged');
 const plan=await readPrivate(join(f.root,'upgrade-plan.json')),layout=managedUpgradeLayout(core,f.root,plan);
 assert.equal(plan.version,2);assert.deepEqual(layout.services,['nibfin-control.service']);
 assert.notEqual(plan.current.pins.source,plan.next.pins.source);
 assert.deepEqual(plan.next,nativeInstallationSelection(core,context.identity,f.next));
 const store=await openFileHostStore(core,{directory:join(f.root,'state')});const work=(await store.read()).work;await store.close();
 const options={core,context,root:f.root,manager:f.manager};
 assert.equal((await upgradeManagedInstallation(['--apply'],options)).status,'switched');
 assert.deepEqual(await readPrivate(join(f.root,'native-installation.json')),f.next);
 assert.equal((await upgradeManagedInstallation(['--rollback'],options)).status,'rolled-back');
 assert.deepEqual(await fs.readFile(join(f.root,'native-installation.json')),original);
 const after=await openFileHostStore(core,{directory:join(f.root,'state')});assert.deepEqual((await after.read()).work,work);await after.close();
 assert.equal(await fs.readFile(external,'utf8'),'ORIGINAL');assert.deepEqual(f.calls.filter(c=>c[0]==='start').map(c=>c[1]),['nibfin-control.service','nibfin-control.service']);
}));

test('an interrupted original staging is reconciled only with the retained completed transition; missing activated ledgers are held',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);await fs.rm(join(f.root,'pool-state'),{recursive:true});
 await assert.rejects(stageUnactivatedNativeUpgrade(core,f.args),/ENOENT/);
 const body={version:1,phase:'completed',result:{status:'switched'},next:core.contentIdentity(f.previous)};
 await writePrivate(join(f.root,'staging-transition.json'),{...body,identity:core.contentIdentity(body)});
 assert.equal((await stageUnactivatedNativeUpgrade(core,f.args)).status,'staged');
 assert.deepEqual(await readPrivate(join(f.root,'native-installation.json')),f.previous);
}));

test('existing work and unknown upgrade outcomes cannot be discarded to stage another generation',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);await fs.mkdir(join(f.root,'jobs'),{mode:0o700});
 await fs.writeFile(join(f.root,'jobs','original'),'ORIGINAL',{mode:0o600});
 await assert.rejects(stageUnactivatedNativeUpgrade(core,f.args),/existing-work/);
 assert.equal(await fs.readFile(join(f.root,'jobs/original'),'utf8'),'ORIGINAL');
 await fs.rm(join(f.root,'jobs'),{recursive:true});await writePrivate(join(f.root,'upgrade.json'),{phase:'unknown'});
 await assert.rejects(stageUnactivatedNativeUpgrade(core,f.args),/original-upgrade-reconciliation-required/);
 await assert.rejects(fs.stat(join(f.root,'runtime-next')),{code:'ENOENT'});
 assert.deepEqual(await readPrivate(join(f.root,'native-installation.json')),f.previous);
}));


test('a completed unused generation is parked with exact rollback evidence before the next upgrade; unknown journals are preserved',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);await stageUnactivatedNativeUpgrade(core,f.args);
 const options={core,context,root:f.root,manager:f.manager};assert.equal((await upgradeManagedInstallation(['--apply'],options)).status,'switched');
 const journal=await readPrivate(join(f.root,'upgrade.json'));
 const args={root:f.root,current:journal.next,installation:f.next,observeUnused:async()=>({status:'unused'})};
 await writePrivate(join(f.root,'upgrade.json'),{...journal,phase:'unknown'});
 await assert.rejects(archiveCompletedNativeUpgrade(core,args),/unfinished-or-different-transaction/);
 assert.equal((await readPrivate(join(f.root,'upgrade.json'))).phase,'unknown');
 await writePrivate(join(f.root,'upgrade.json'),journal);
 const old=await fs.readFile(join(f.root,'native-installation-previous.json'));
 assert.equal((await archiveCompletedNativeUpgrade(core,args)).status,'retained');
 const path=join(f.root,'upgrade-history',journal.transaction);
 assert.deepEqual(await fs.readFile(join(path,'native-installation.json')),old);
 assert.deepEqual(await readPrivate(join(path,'upgrade.json')),journal);
 assert.equal((await readPrivate(join(path,'archive.json'))).phase,'retained');
 assert.deepEqual(await readPrivate(join(f.root,'native-installation.json')),f.next);
 await assert.rejects(fs.stat(join(f.root,'runtime-previous')),{code:'ENOENT'});
 assert.equal((await archiveCompletedNativeUpgrade(core,args)).status,'absent');
}));

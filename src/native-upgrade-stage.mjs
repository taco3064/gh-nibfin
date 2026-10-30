import fs from 'node:fs/promises';
import {join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {readPrivate,writePrivate,privatePath,sha} from './local-state.mjs';
import {maintenanceLock} from './upgrade.mjs';
import {initializeFileHostStore,openFileHostStore} from './host-store.mjs';
import {nativePoolStore} from './native-pool-store.mjs';
import {verifyNativeRuntime} from './native-control-service.mjs';
import {observeUnusedStagedResources} from './native-staged-transition.mjs';
import {archiveCompletedNativeUpgrade} from './native-upgrade-history.mjs';
import {nativeInstallationSelection} from './managed-upgrade.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-upgrade-stage:'+code);};
const absent=async path=>{try{await fs.lstat(path);return false;}catch(e){if(e.code==='ENOENT')return true;throw e;}};

/** Installer-owned preparation only. A never-used pool may reconcile the final
 * steps of its completed original staging transaction and prepare the existing
 * upgrade owner. This cannot adopt conversations or infer empty activated work.
 */
export async function stageUnactivatedNativeUpgrade(core,{root,previous,next,context,stage,verifyRunner,control,observeResources=observeUnusedStagedResources}={}){
 need(previous?.phase==='staged'&&previous.executionPool?.version===2&&next.executionPool?.version===2
  &&core.canonicalJSON(previous.executionPool)===core.canonicalJSON(next.executionPool)
  &&previous.runnerCount===next.runnerCount,'pool-transition');
 await privatePath(root,{directory:true});
 return maintenanceLock(root,async assertLease=>{
  const selected=await readPrivate(join(root,'native-installation.json'));
  need(core.canonicalJSON(selected)===core.canonicalJSON(previous),'installation-drift');
  await verifyNativeRuntime(root,previous);
  await control.stageUnit();
  need((await observeResources(root,previous.runnerCount)).status==='unused','resources');
  const oldCore=await import(pathToFileURL(join(root,'runtime/node_modules/@nibfin/core/src/index.mjs')).href);
  const current=nativeInstallationSelection(oldCore,oldCore.distributionIdentity(),previous);
  const destination=nativeInstallationSelection(core,context.identity,next);
  const known=new Set(['runtime','native-installation.json','control-lease','control-service.json','nibfin-control.service','maintenance.lock',
   'previous-installation','staging-transition.json','control-retirement.json','pool-state','pool-intake','pool-blockages','jobs','state',
   'upgrade-plan.json','upgrade.json','runtime-next','native-installation-next.json',
   'member-runtime.json','owner-policy.json','owner-signing-key.pem','stage-records','dispatches','runtime-previous','native-installation-previous.json','upgrade-history','original-work','control.sock']);
  for(let i=1;i<=previous.runnerCount;i++)known.add('runner-'+i);
  need((await fs.readdir(root)).every(name=>known.has(name)),'unrecognized-state');
  for(const name of ['jobs','pool-intake','pool-blockages','original-work','stage-records','dispatches'])if(!await absent(join(root,name))){
   await privatePath(join(root,name),{directory:true});
   need((await fs.readdir(join(root,name))).every(name=>name==='maintenance.lock'),'existing-work');
  }
  const pool=nativePoolStore(oldCore,{root,configuration:previous.executionPool});
  if(await absent(join(root,'pool-state'))){
   const completed=await readPrivate(join(root,'staging-transition.json'));
   const {identity,...body}=completed;
   need(identity===core.contentIdentity(body)&&completed.phase==='completed'&&completed.result?.status==='switched'
    &&completed.next===core.contentIdentity(previous),'original-staging-unconfirmed');
   // Only finish the already committed original installation; no missing
   // activated ledger is reinterpreted as a new pool.
   await pool.initialize();
  }
  const status=await pool.status(),ledger=await readPrivate(join(root,'pool-state/ledger.json'),{limit:33554432});
  need(status.occupied===0&&status.pending===0&&Object.keys(ledger.jobs).length===0
   &&Object.keys(ledger.capacity.completed).length===0,'previous-execution');
  for(let i=1;i<=previous.runnerCount;i++)await verifyRunner(join(root,'runner-'+i),{unregistered:true});
  let store;
  try{
   if(await absent(join(root,'state'))){
    store=await initializeFileHostStore(core,{directory:join(root,'state'),installation:current,
     repositories:previous.executionPool.targets.map(({repository,repositoryId})=>({repository,repositoryId}))});
   }else store=await openFileHostStore(core,{directory:join(root,'state')});
   const state=await store.read(),work=state.work;
   need(core.canonicalJSON(state.installation)===core.canonicalJSON(current)&&work.paused===true&&work.hold===null
    &&work.unknownWrite===false&&work.active===false&&work.terminated===true&&work.reconciled===true
    &&['conversations','pending','receipts'].every(k=>work[k].length===0),'work-reconciliation-required');
  }finally{await store?.close();}
  const journal=await readPrivate(join(root,'upgrade.json'),{missing:true});
  need(!journal||['complete','rolled-back'].includes(journal.phase),'original-upgrade-reconciliation-required');
  if(journal)await archiveCompletedNativeUpgrade(core,{root,current,installation:previous});
  await stage();assertLease();
  const nextPath=join(root,'native-installation-next.json'),bytes=JSON.stringify(next,null,2)+'\n';
  if(await absent(nextPath))await writePrivate(nextPath,next,{exclusive:true});
  else need((await fs.readFile(nextPath)).equals(Buffer.from(bytes)),'next-metadata-conflict');
  const inventory=record=>record.runtimeInventory.map(f=>({kind:'file',...f,mode:0o600}));
  const metadata=bytes=>[{kind:'file',path:'native-installation.json',mode:0o600,sha256:sha(bytes)}];
  const body={version:2,current,next:destination,resources:[{name:'runtime',current:inventory(previous),next:inventory(next)},
   {name:'installation',current:metadata(await fs.readFile(join(root,'native-installation.json'))),next:metadata(bytes)}]};
  const plan={...body,identity:core.contentIdentity(body)},oldPlan=await readPrivate(join(root,'upgrade-plan.json'),{missing:true,limit:4000000});
  need(!oldPlan||core.canonicalJSON(oldPlan)===core.canonicalJSON(plan),'original-plan-conflict');
  if(!oldPlan)await writePrivate(join(root,'upgrade-plan.json'),plan,{exclusive:true});
  need((await observeResources(root,previous.runnerCount)).status==='unused','resources');assertLease();return {status:'staged',plan};
 });
}

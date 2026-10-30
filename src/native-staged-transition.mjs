import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {pathToFileURL} from 'node:url';
import {spawnSync} from 'node:child_process';
import {ensurePrivateDirectory,readPrivate,writePrivate,sha,privatePath} from './local-state.mjs';
import {maintenanceLock} from './upgrade.mjs';
import {verifyNativeRuntime} from './native-control-service.mjs';
import {assertNativeResourcesUnused} from './native-resource-cleanup.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-staged-transition:'+code);};
const inside=(a,b)=>a===b||a.startsWith(b+'/');
const absent=async p=>{try{await fs.lstat(p);return false;}catch(e){if(e.code==='ENOENT')return true;throw e;}};

/** Actual local observations, separate from source/fixture readiness. */
export async function observeUnusedStagedResources(root,count){
 const roots=[join(root,'runtime'),...Array.from({length:count},(_,i)=>join(root,'runner-'+(i+1)))];
 await assertNativeResourcesUnused(roots,'/nibfin-retired-staging');
 const socket='/run/user/'+userInfo().uid+'/docker.sock',s=await fs.lstat(socket);
 need(s.isSocket()&&s.uid===userInfo().uid,'docker-owner');
 const call=args=>{const r=spawnSync('/usr/bin/docker',['--host','unix://'+socket,...args],{
  encoding:'utf8',timeout:15000,maxBuffer:8388608,env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:userInfo().homedir,LANG:'C'}});
  need(!r.error&&r.status===0,'docker-observation');return r.stdout;};
 const ids=call(['ps','-aq']).trim().split(/\s+/).filter(Boolean);
 need(ids.every(id=>/^[a-f0-9]{12,64}$/.test(id)),'docker-inventory');
 if(ids.length){const containers=JSON.parse(call(['inspect',...ids]));
  need(Array.isArray(containers)&&containers.length===ids.length,'docker-inventory');
  const seen=new Set();for(const c of containers){
   need(/^[a-f0-9]{64}$/.test(c.Id)&&!seen.has(c.Id)&&ids.some(id=>c.Id.startsWith(id))&&Array.isArray(c.Mounts),'docker-container');seen.add(c.Id);
   for(const m of c.Mounts){need(typeof m.Source==='string'&&m.Source.startsWith('/'),'docker-mount');
    need(!roots.some(root=>inside(m.Source,root)||inside(root,m.Source)),'docker-reference');}
  }
 }
 return {status:'unused'};
}

/** Only a never-activated v1 staging layout. This is not an active-work upgrade,
 * an adoption of external conversations, or delivery readiness. A failed switch
 * retains its exact intent and original runtime for independent reconciliation.
 */
export async function transitionStagedNativeHost(core,{root,previous,next,pool,publish,verifyRunner,control,
 observeResources=observeUnusedStagedResources}={}){
 need(previous?.version===1&&previous.phase==='staged'&&previous.executionPool?.version===1
  &&previous.runnerCount===pool.capacity&&previous.core!==next.core,'preserve-existing-installation');
 need(core.canonicalJSON(previous.executionPool.targets)===core.canonicalJSON(pool.targets),'pool-membership');
 need(typeof control?.retire==='function'&&typeof publish==='function','capabilities');
 const path=join(root,'staging-transition.json'),old=await readPrivate(path,{missing:true});
 // Do not blindly replay a filesystem switch after an interrupted invocation.
 need(old===null,'original-transition-reconciliation-required');
 const allowed=new Set(['runtime','native-installation.json','control-lease','control-service.json','nibfin-control.service','maintenance.lock']);
 for(let i=1;i<=pool.capacity;i++)allowed.add('runner-'+i);
 need((await fs.readdir(root)).every(name=>allowed.has(name)),'unrecognized-staging-state');
 await ensurePrivateDirectory(join(root,'control-lease'));
 return maintenanceLock(join(root,'control-lease'),async assertLease=>{
  await verifyNativeRuntime(root,previous);
  for(let i=1;i<=pool.capacity;i++){
   const runner=join(root,'runner-'+i);await verifyRunner(runner,{unregistered:true});
   for(const name of ['.runner','.credentials','.credentials_rsaparams','_work'])need(await absent(join(runner,name)),'runner-work-or-registration');
  }
  need((await observeResources(root,pool.capacity)).status==='unused','resources');assertLease();
  const original=await fs.readFile(join(root,'native-installation.json'));
  need(sha(original)===sha(JSON.stringify(previous,null,2)+'\n'),'installation-drift');
  const oldCore=await import(pathToFileURL(join(root,'runtime/node_modules/@nibfin/core/src/index.mjs')).href);
  const oldIdentity=oldCore.distributionIdentity(),selection=v=>({packageVersion:v.packageVersion,pins:v.pins});
  need(oldIdentity.components?.core?.sha256===previous.core,'previous-core-identity');
  const current=selection(oldIdentity),destination=selection(core.distributionIdentity());
  const record={version:1,phase:'pending',previous:sha(original),next:core.contentIdentity(next),intent:null};
  const archive=join(root,'previous-installation');need(await absent(archive),'previous-installation-conflict');
  const work={restrictionsChecked:true,hold:null,paused:true,unknownWrite:false,active:false,terminated:true,reconciled:true,
   conversations:[],pending:[],receipts:[]};
  const observe=async()=>{
   assertLease();const actual=await readPrivate(join(root,'native-installation.json'));
   if(actual.core===previous.core){await verifyNativeRuntime(root,previous);return{installation:current,work};}
   need(core.canonicalJSON(actual)===core.canonicalJSON(next),'installation-drift');
   await verifyNativeRuntime(root,next);return{installation:destination,work};
  };
  const result=await core.applyInstallationTransition({current,next:destination,maintenance:true,transaction:'unactivated-native-staging',observe,
   retain:async intent=>{need(record.intent===null,'duplicate-intent');record.intent=intent;
    await writePrivate(path,{...record,identity:core.contentIdentity(record)},{exclusive:true});return{status:'retained',identity:intent.identity,prior:'new'};},
   switchPins:async()=>{
    assertLease();need((await observeResources(root,pool.capacity)).status==='unused','resources');
    await control.retire();assertLease();
    await fs.mkdir(archive,{mode:0o700});
    await fs.writeFile(join(archive,'native-installation.json'),original,{flag:'wx',mode:0o600});
    await fs.rename(join(root,'runtime'),join(archive,'runtime'));
    await verifyNativeRuntime(archive,previous);await publish();assertLease();
    await writePrivate(join(root,'native-installation.json'),next);
   }});
  if(result.status!=='switched')throw Error('native-staged-transition:'+result.code);
  const complete={...record,phase:'completed',result};await writePrivate(path,{...complete,identity:core.contentIdentity(complete)});
  await privatePath(archive,{directory:true});return {status:'switched',previous:archive};
 });
}

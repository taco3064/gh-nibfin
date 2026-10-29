import fs from 'node:fs/promises';
import {join,dirname,resolve} from 'node:path';
import {userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {privatePath,readPrivate,writePrivate,localLock} from './local-state.mjs';

const need=(ok,code)=>{if(!ok)throw Error('upgrade:'+code);};
const same=(core,a,b)=>core.canonicalJSON(a)===core.canonicalJSON(b);
const absent=async path=>{try{await fs.lstat(path);return false;}catch(e){if(e.code==='ENOENT')return true;throw e;}};
async function resourcePaths(resources,services){
  need(Array.isArray(resources)&&resources.length>0&&resources.length<=16&&Array.isArray(services)
    &&services.length>0&&services.length<=3&&new Set(services).size===services.length
    &&services.every(unit=>/^nibfin-[a-z0-9-]+\.service$/.test(unit)),'resource-set');
  const seen=new Set();
  const names=new Set();
  for(const r of resources){
    need(r&&Object.keys(r).sort().join(' ')==='current name next previous'&&/^[a-z0-9-]+$/.test(r.name),'resource-schema');
    need(!names.has(r.name),'resource-name');names.add(r.name);
    for(const k of ['current','next','previous']){
      need(typeof r[k]==='string'&&resolve(r[k])===r[k]&&!seen.has(r[k]),'resource-path');seen.add(r[k]);
      need(dirname(r[k])===dirname(r.current),'resource-parent');await privatePath(dirname(r[k]),{directory:true});
    }
  }
  const paths=[...seen];need(!paths.some((p,i)=>paths.some((q,j)=>i!==j&&p.startsWith(q+'/'))),'resource-overlap');
}
export function upgradeOptions(args){
  need(Array.isArray(args)&&args.length<=1&&args.every(a=>['--check','--apply','--rollback'].includes(a)),'options');
  return {operation:(args[0]??'--check').slice(2)};
}
export function upgradeServiceManager(){
  const run=args=>spawnSync('/usr/bin/systemctl',['--user',...args],{encoding:'utf8',timeout:60000,maxBuffer:8192,
    env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:userInfo().homedir,XDG_RUNTIME_DIR:'/run/user/'+userInfo().uid,LANG:'C'}});
  return Object.freeze({
    async observe(unit){
      need(/^nibfin-[a-z0-9-]+\.service$/.test(unit),'service-name');
      const r=run(['show',unit,'--property=LoadState','--property=ActiveState','--property=MainPID','--property=ControlPID','--property=TasksCurrent','--property=ControlGroup']);
      need(!r.error&&r.status===0,'service-observation');const p=Object.fromEntries(r.stdout.trim().split('\n').map(l=>{const n=l.indexOf('=');return[l.slice(0,n),l.slice(n+1)];}));
      need(p.LoadState==='loaded','service-unavailable');
      return {active:p.ActiveState==='active',stopped:p.ActiveState==='inactive'&&p.MainPID==='0'&&p.ControlPID==='0'
        &&(p.TasksCurrent==='0'||p.TasksCurrent==='[not set]'&&p.ControlGroup==='')};
    },
    async stop(unit){const r=run(['stop',unit]);need(!r.error&&r.status===0,'service-stop');},
    async start(unit){const r=run(['start',unit]);need(!r.error&&r.status===0,'service-start');},
    async reload(){const r=run(['daemon-reload']);need(!r.error&&r.status===0,'service-reload');}
  });
}

/** A managed installer supplies sealed resource inventories, never executable
 * user bindings. All code/binary/policy directories switch at one stopped service
 * boundary. Original work is owned by the persistent store outside these paths.
 * Unknown outcomes retain the original journal and do not repeat a move.
 */
export async function coordinateUpgrade({core,directory,store,current,next,resources,services,verify,
  manager=upgradeServiceManager(),check=false}={}){
  need(core&&typeof store?.read==='function'&&typeof store?.transitionCapabilities==='function'
    &&typeof verify==='function'&&Array.isArray(resources)&&resources.length>0&&resources.length<=16
    &&Array.isArray(services)&&services.length>0&&services.length<=3&&new Set(services).size===services.length,'configuration');
  await privatePath(directory,{directory:true});
  await resourcePaths(resources,services);
  const inspect=async()=>{
    const state=await store.read();need(same(core,state.installation,current),'installation-drift');
    const plan=core.planInstallationTransition({version:1,current,next,work:state.work},{maintenance:true});
    need(plan.status==='planned','unsafe-work-boundary');
    for(const r of resources){await privatePath(r.current,{directory:true});need(await verify(r,'current')===true,'current-resource-unverified');}
    return {state,plan};
  };
  if(check){const result=await inspect();return {status:'checked',operation:result.plan.operation,current,next,services:[...services]};}
  return localLock(directory,'upgrade',async()=>{
    const path=join(directory,'upgrade.json'),existing=await readPrivate(path,{missing:true});
    // Resume/rollback must reconcile this exact journal rather than stage a new
    // transaction over paths whose rename outcome may not have been observed.
    need(!existing||existing.phase==='complete'||existing.phase==='rolled-back','reconciliation-required');
    const {state,plan}=await inspect();
    if(plan.operation==='unchanged')return {status:'unchanged'};
    for(const r of resources){await privatePath(r.next,{directory:true});need(await absent(r.previous)&&await verify(r,'next')===true,'staged-resource-unverified');}
    const journal={version:1,transaction:randomUUID(),phase:'prepared',current,next,protectedState:core.contentIdentity(state.work),
      resources:structuredClone(resources),services:[...services],moves:[]};
    await writePrivate(path,journal);
    try{
      journal.phase='stopping';await writePrivate(path,journal);
      for(const unit of services)await manager.stop(unit);
      for(const unit of services)need((await manager.observe(unit)).stopped===true,'service-not-terminated');
      need(same(core,(await store.read()).work,state.work),'work-drift');
      journal.phase='switching';await writePrivate(path,journal);
      for(const r of resources){
        need(await verify(r,'current')===true&&await verify(r,'next')===true,'resource-drift');
        journal.moves.push({name:r.name,step:'retire-intent'});await writePrivate(path,journal);
        await fs.rename(r.current,r.previous);journal.moves.at(-1).step='activate-intent';await writePrivate(path,journal);
        await fs.rename(r.next,r.current);journal.moves.at(-1).step='activated';await writePrivate(path,journal);
      }
      const capabilities=store.transitionCapabilities({verifyInstallation:async installation=>{
        for(const unit of services)need((await manager.observe(unit)).stopped===true,'service-not-terminated');
        for(const r of resources)need(await verify(r,'activated')===true,'activated-resource-unverified');
        return {status:'verified',installation};
      }});
      journal.phase='pins';await writePrivate(path,journal);
      const switched=await core.applyInstallationTransition({...capabilities,current,next,maintenance:true,transaction:journal.transaction});
      need(switched.status==='switched','pin-switch-unconfirmed');
      journal.phase='starting';await writePrivate(path,journal);await manager.reload();
      for(const unit of [...services].reverse())await manager.start(unit);
      for(const unit of services)need((await manager.observe(unit)).active===true,'service-not-ready');
      const after=await store.read();need(same(core,after.installation,next)&&same(core,after.work,state.work),'readback-drift');
      journal.phase='complete';await writePrivate(path,journal);
      return {status:'switched',current:next,previous:current,paused:after.work.paused};
    }catch{
      // Stop potentially mixed components. Never restart an unknown composition
      // or delete either generation to make a subsequent attempt look new.
      for(const unit of services)try{await manager.stop(unit);}catch{}
      journal.failedPhase=journal.phase;journal.phase='unknown';await writePrivate(path,journal);
      const stopped=await Promise.all(services.map(async unit=>{try{return (await manager.observe(unit)).stopped===true;}catch{return false;}}));
      return {status:'unknown',code:'reconcile-original-upgrade',servicesStopped:stopped.every(Boolean)};
    }
  });
}

export async function rollbackUpgrade({core,directory,store,verify,manager=upgradeServiceManager()}={}){
  await privatePath(directory,{directory:true});
  return localLock(directory,'upgrade',async()=>{
    const path=join(directory,'upgrade.json'),journal=await readPrivate(path);
    need(journal?.version===1&&['complete','unknown','rollback','rolled-back'].includes(journal.phase)
      &&Array.isArray(journal.resources)&&journal.resources.length>0&&Array.isArray(journal.services),'rollback-journal');
    await resourcePaths(journal.resources,journal.services);
    const state=await store.read();need(core.contentIdentity(state.work)===journal.protectedState,'work-drift');
    need(same(core,state.installation,journal.current)||same(core,state.installation,journal.next),'installation-drift');
    if(journal.phase==='rolled-back'){
      need(same(core,state.installation,journal.current),'installation-drift');
      for(const r of journal.resources)need(await verify(r,'current')===true,'restored-resource-unverified');
      return {status:'rolled-back',already:true,current:journal.current,paused:state.work.paused};
    }
    journal.phase='rollback';journal.rollbackTransaction??=randomUUID();await writePrivate(path,journal);
    try{
      for(const unit of journal.services)await manager.stop(unit);
      for(const unit of journal.services)need((await manager.observe(unit)).stopped===true,'service-not-terminated');
      for(const r of [...journal.resources].reverse()){
        if(await absent(r.previous)){need(await verify(r,'current')===true,'original-resource-unavailable');continue;}
        await privatePath(r.previous,{directory:true});need(await verify(r,'previous')===true,'previous-resource-unverified');
        if(!await absent(r.current)){
          need(await absent(r.next)&&await verify(r,'activated')===true,'rollback-resource-conflict');
          await fs.rename(r.current,r.next);
        }
        await fs.rename(r.previous,r.current);need(await verify(r,'current')===true,'restored-resource-unverified');
      }
      const capabilities=store.transitionCapabilities({verifyInstallation:async installation=>{
        for(const unit of journal.services)need((await manager.observe(unit)).stopped===true,'service-not-terminated');
        for(const r of journal.resources)need(await verify(r,'current')===true,'restored-resource-unverified');
        return {status:'verified',installation};
      }});
      const observed=await store.read();
      if(same(core,observed.installation,journal.next)){
        const result=await core.applyInstallationTransition({...capabilities,current:journal.next,next:journal.current,
          maintenance:true,transaction:journal.rollbackTransaction});need(result.status==='switched','rollback-pins-unconfirmed');
      }
      need(core.contentIdentity((await store.read()).work)===journal.protectedState,'work-drift');
      await manager.reload();for(const unit of [...journal.services].reverse())await manager.start(unit);
      for(const unit of journal.services)need((await manager.observe(unit)).active===true,'service-not-ready');
      journal.phase='rolled-back';await writePrivate(path,journal);return {status:'rolled-back',current:journal.current,paused:true};
    }catch{
      for(const unit of journal.services)try{await manager.stop(unit);}catch{}
      journal.phase='unknown';await writePrivate(path,journal);return {status:'unknown',code:'reconcile-original-upgrade'};
    }
  });
}

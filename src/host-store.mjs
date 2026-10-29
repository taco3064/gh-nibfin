import fs from 'node:fs/promises';
import {watch} from 'node:fs';
import {join,dirname} from 'node:path';
import {privatePath,readPrivate,writePrivate,localLock} from './local-state.mjs';

const need=(ok,code)=>{if(!ok)throw Error('host-store:'+code);};
const fields=(v,names)=>need(v&&Object.getPrototypeOf(v)===Object.prototype
  &&Object.keys(v).sort().join(' ')===names.split(' ').sort().join(' '),'schema');
const initialWork=()=>({restrictionsChecked:true,hold:null,paused:true,unknownWrite:false,
  active:false,terminated:true,reconciled:true,conversations:[],pending:[],receipts:[]});
function workShape(value){
  fields(value,'restrictionsChecked hold paused unknownWrite active terminated reconciled conversations pending receipts');
  need(['restrictionsChecked','paused','unknownWrite','active','terminated','reconciled'].every(k=>typeof value[k]==='boolean')
    &&['conversations','pending','receipts'].every(k=>Array.isArray(value[k])),'work');
}
function installationShape(core,value){
  // Syntax only. This does not verify an archive, activate a runtime, or clear
  // the actual store's pause/hold. Core independently guards every transition.
  const work={...initialWork(),paused:false};
  need(core.planInstallationTransition({version:1,current:value,next:value,work}).status==='planned','installation');
}
function repositoriesShape(core,repositories){
  need(Array.isArray(repositories)&&repositories.length>0&&repositories.length<=32,'repositories');
  const names=new Set(),ids=new Set();
  for(const value of repositories){
    fields(value,'repository repositoryId');core.githubId(value.repositoryId);
    need(typeof value.repository==='string'&&value.repository.split('/').length===2
      &&core.repositoryCoordinates(...value.repository.split('/')).key===value.repository
      &&!names.has(value.repository)&&!ids.has(value.repositoryId),'repositories');
    names.add(value.repository);ids.add(value.repositoryId);
  }
}
const same=(core,a,b)=>core.canonicalJSON(a)===core.canonicalJSON(b);
const transitionIdentity=(core,plan)=>core.releaseHash(core.canonicalJSON(plan));
function validate(core,state){
  need(Buffer.byteLength(JSON.stringify(state,null,2)+'\n')<=1048576,'size');
  fields(state,'version revision installation repositories work rescue transitions identity');
  need(state.version===1&&Number.isSafeInteger(state.revision)&&state.revision>=0,'version');
  installationShape(core,state.installation);repositoriesShape(core,state.repositories);workShape(state.work);
  const {identity,...body}=state;need(core.contentIdentity(body)===identity,'integrity');
  need(state.rescue&&Object.getPrototypeOf(state.rescue)===Object.prototype
    &&same(core,Object.keys(state.rescue).sort(),state.repositories.map(r=>r.repositoryId).sort()),'rescue-repositories');
  for(const value of Object.values(state.rescue)){
    fields(value,'version revision cases inbox budgets notifications identity');
    const {identity,...body}=value;need(value.version===1&&Number.isSafeInteger(value.revision)&&value.revision>=0
      &&core.contentIdentity(body)===identity,'rescue-integrity');
    for(const k of ['cases','inbox','budgets','notifications'])need(value[k]&&Object.getPrototypeOf(value[k])===Object.prototype,'rescue-map');
  }
  need(state.transitions&&Object.getPrototypeOf(state.transitions)===Object.prototype,'transitions');
  for(const [id,entry] of Object.entries(state.transitions)){
    fields(entry,'intent status');fields(entry.intent,'version kind identity plan');
    need(entry.intent.version===1&&entry.intent.kind==='installation-switch'&&entry.intent.identity===id
      &&transitionIdentity(core,entry.intent.plan)===id&&['pending','applied'].includes(entry.status),'transition-identity');
  }
  return state;
}
const seal=(core,value)=>{const {identity,...body}=value;return {...body,identity:core.contentIdentity(body)};};

/** Explicit new-host allocation only. An existing/missing/corrupt store is never
 * interpreted as empty work. init must open and reconcile it instead of reset.
 */
export async function initializeFileHostStore(core,{directory,installation,repositories}={}){
  installationShape(core,installation);repositoriesShape(core,repositories);
  await privatePath(dirname(directory),{directory:true});
  await privatePath(directory,{directory:true,missing:true});
  await fs.mkdir(directory,{mode:0o700}); // exclusive allocation; EEXIST preserves original work
  const state=seal(core,{version:1,revision:0,installation:structuredClone(installation),repositories:structuredClone(repositories),
    work:initialWork(),rescue:Object.fromEntries(repositories.map(r=>[r.repositoryId,core.initialRescueRecords()])),transitions:{}});
  validate(core,state);
  await writePrivate(join(directory,'store.json'),state,{exclusive:true});
  return openFileHostStore(core,{directory});
}

/** Host-account capabilities only; never expose these methods to event/model
 * input. One atomic private record contains all pin selection and protected work.
 * Filesystem notifications wake the maintained Core notifier without polling.
 */
export async function openFileHostStore(core,{directory}={}){
  await privatePath(directory,{directory:true});
  const path=join(directory,'store.json');let closed=false,watcher=null,watchError=false,tail=Promise.resolve();
  const subscribers=new Set();
  const read=async()=>{need(!closed,'closed');need(!watchError,'subscription-unavailable');return validate(core,await readPrivate(path));};
  await read();
  const exclusive=work=>{
    const pending=tail.then(()=>localLock(directory,'host-store',()=>{need(!closed,'closed');return work();}));
    tail=pending.catch(()=>{});return pending;
  };
  const commit=async(before,value)=>{
    const current=await read();need(current.identity===before.identity,'compare-and-swap');
    const next=seal(core,{...structuredClone(value),revision:before.revision+1});validate(core,next);
    await writePrivate(path,next);
    need(same(core,await read(),next),'readback');return next;
  };
  const subscribe=async callback=>{
    need(typeof callback==='function','subscription');await read();
    if(!watcher){
      watcher=watch(directory,{persistent:true},(_event,name)=>{
        if(name!==null&&name.toString()!=='store.json')return;
        for(const notify of subscribers)queueMicrotask(notify);
      });
      watcher.on('error',()=>{watchError=true;for(const notify of subscribers)queueMicrotask(notify);});
    }
    subscribers.add(callback);queueMicrotask(callback);
    let removed=false;return ()=>{if(removed)return;removed=true;subscribers.delete(callback);if(!subscribers.size){watcher?.close();watcher=null;}};
  };
  function rescue(repositoryId){
    core.githubId(repositoryId);
    return core.createRescueRecords({
      load:async()=>{const state=await read();need(Object.hasOwn(state.rescue,repositoryId),'repository');return state.rescue[repositoryId];},
      exclusive,subscribe,
      commit:async({expected,value})=>{
        const before=await read(),old=before.rescue[repositoryId];
        need(old&&old.identity===expected&&value.revision===old.revision+1,'rescue-compare-and-swap');
        await commit(before,{...before,rescue:{...before.rescue,[repositoryId]:value}});return {outcome:'retained'};
      }
    });
  }
  async function retainTransition(intent){return exclusive(async()=>{
    fields(intent,'version kind identity plan');need(intent.version===1&&intent.kind==='installation-switch'
      &&intent.identity===transitionIdentity(core,intent.plan),'transition-intent');
    const state=await read(),prior=state.transitions[intent.identity];
    if(prior){need(same(core,prior.intent,intent),'transition-conflict');return {status:'retained',identity:intent.identity,prior:'unresolved'};}
    const plan=core.planInstallationTransition({version:1,current:intent.plan.expected,next:intent.plan.next,work:state.work});
    need(plan.status==='planned'&&plan.operation==='switch'&&same(core,plan,intent.plan)
      &&same(core,state.installation,plan.expected),'transition-checkpoint');
    await commit(state,{...state,transitions:{...state.transitions,[intent.identity]:{intent:structuredClone(intent),status:'pending'}}});
    return {status:'retained',identity:intent.identity,prior:'new'};
  });}
  return Object.freeze({read,rescue,retainTransition,
    // Called by the protected lifecycle owner, not by init/upgrade to fabricate a
    // safe checkpoint. Owner pause, unknown writes and original evidence persist.
    updateWork:({expected,work})=>exclusive(async()=>{
      workShape(work);const state=await read();need(state.identity===expected,'work-compare-and-swap');
      return commit(state,{...state,work:structuredClone(work)});
    }),
    transitionCapabilities({verifyInstallation}={}){
      need(typeof verifyInstallation==='function','runtime-verifier');
      return Object.freeze({
        observe:async()=>{const state=await read();return {installation:state.installation,work:state.work};},
        retain:retainTransition,
        switchPins:request=>exclusive(async()=>{
          fields(request,'expected next protectedState intent');const before=await read();
          const retained=before.transitions[request.intent];need(retained?.status==='pending','transition-retention');
          const plan=core.planInstallationTransition({version:1,current:request.expected,next:request.next,work:before.work});
          need(plan.status==='planned'&&plan.operation==='switch'&&same(core,plan,retained.intent.plan)
            &&plan.protectedState===request.protectedState&&same(core,before.installation,request.expected),'transition-checkpoint');
          // The coordinator must verify the entire staged runtime and terminated
          // service boundary. This adapter switches selection, never service files.
          const verified=await verifyInstallation(structuredClone(request.next));
          need(verified?.status==='verified'&&same(core,verified.installation,request.next),'runtime-unverified');
          need(same(core,await read(),before),'transition-drift');
          await commit(before,{...before,installation:structuredClone(request.next),transitions:{...before.transitions,
            [request.intent]:{...retained,status:'applied'}}});
        })
      });
    },
    async close(){await tail;closed=true;subscribers.clear();watcher?.close();watcher=null;}
  });
}

/** Compatibility preserves an existing record owner. New protected hosts omit
 * executable record callbacks; the official installer allocates their one store.
 */
export async function managedHostRecords(core,{directory,hosts,installation,initialize=false}={}){
  need(Array.isArray(hosts)&&hosts.length>0&&hosts.length<=32,'hosts');
  const repositories=hosts.map(entry=>{
    const policy=core.validateRescuePolicy(entry.configuration?.policy);
    return {repository:policy.repository,repositoryId:policy.repositoryId};
  }).sort((a,b)=>a.repository.localeCompare(b.repository,'en'));
  repositoriesShape(core,repositories);
  const supplied=hosts.map(entry=>Object.hasOwn(entry.configuration,'records'));
  if(supplied.every(Boolean))return {hosts,close:async()=>{}};
  need(supplied.every(v=>!v),'mixed-record-ownership');
  installationShape(core,installation);
  const root=join(directory,'state');let exists=true;
  try{await fs.lstat(root);}catch(error){if(error.code!=='ENOENT')throw error;exists=false;}
  need(exists||initialize,'missing');
  const storage=exists?await openFileHostStore(core,{directory:root}):await initializeFileHostStore(core,{directory:root,installation,repositories});
  try{
    const state=await storage.read();need(same(core,state.repositories,repositories)&&same(core,state.installation,installation),'installed-identity');
    return {hosts:hosts.map(entry=>{
      const original=entry.configuration,current=original.current;
      return {...entry,configuration:{...original,records:storage.rescue(original.policy.repositoryId),
        ...(typeof current==='function'?{current:async target=>{
          const before=await storage.read(),restriction=core.protectedWorkRestriction(before.work);
          need(restriction===null,restriction??'restrictions');
          const observed=await current.call(original,target),after=await storage.read();
          need(same(core,before.work,after.work)&&core.protectedWorkRestriction(after.work)===null,'work-changed');
          return observed;
        }}:{})}};
    }),close:storage.close};
  }catch(error){await storage.close();throw error;}
}

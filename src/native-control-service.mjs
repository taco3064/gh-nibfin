import fs from 'node:fs/promises';
import {join,resolve} from 'node:path';
import {userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {connect} from 'node:net';
import {randomUUID} from 'node:crypto';
import{pathToFileURL}from'node:url';import{openFileHostStore}from'./host-store.mjs';import{nativePoolStore}from'./native-pool-store.mjs';
import {privatePath,readPrivate,writePrivate,sha} from './local-state.mjs';

const need=(ok,code)=>{if(!ok)throw Error('native-service:'+code);};
export const nativeServiceName='nibfin-control.service';
const quote=value=>{
 need(typeof value==='string'&&resolve(value)===value&&!/[\x00-\x1f\x7f]/.test(value),'path');
 return '"'+value.replace(/\\/g,'\\\\').replace(/"/g,'\\"').replace(/%/g,'%%').replace(/\$/g,()=> '$$')+'"';
};
export function nativeServiceUnit(root,node=process.execPath){
 return `[Unit]\nDescription=Nibfin native control host\nStartLimitIntervalSec=60\nStartLimitBurst=3\n[Service]\nType=notify\nNotifyAccess=all\nTimeoutStartSec=45\nTimeoutStopSec=30\nExecStart=${quote(node)} ${quote(join(root,'runtime/client/native-control-main.mjs'))} ${quote(root)}\nEnvironment=HOME=${quote(userInfo().homedir)}\nEnvironment=PATH=/usr/local/bin:/usr/bin:/bin\nUnsetEnvironment=GH_TOKEN GITHUB_TOKEN NODE_OPTIONS NODE_PATH CODEX_HOME\nUMask=0077\nRestart=on-failure\nNoNewPrivileges=true\nKillMode=mixed\n[Install]\nWantedBy=default.target\n`;
}
export function nativeSystemctl(args){
 const r=spawnSync('/usr/bin/systemctl',['--user',...args],{encoding:'utf8',timeout:60000,maxBuffer:8192,
  env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:userInfo().homedir,LANG:'C',XDG_RUNTIME_DIR:'/run/user/'+userInfo().uid}});
 return {ok:!r.error&&r.status===0,status:r.status??null,output:r.stdout??''};
}
export async function nativeServiceStatus(root){
 await privatePath(root,{directory:true});const path=join(root,'control.sock'),s=await fs.lstat(path);
 need(s.isSocket()&&!s.isSymbolicLink()&&s.uid===userInfo().uid&&(s.mode&0o777)===0o600,'socket');
 return new Promise((resolve,reject)=>{
  const socket=connect(path);let bytes='';
  const fail=()=>{socket.destroy();reject(Error('native-service:unavailable'));};
  socket.setTimeout(3000,fail);socket.on('error',fail);
  socket.once('connect',()=>socket.end('{"version":1,"operation":"status"}\n'));
  socket.on('data',b=>{bytes+=b;if(bytes.length>4096)fail();});
  socket.once('end',()=>{socket.destroy();try{resolve(JSON.parse(bytes));}catch{fail();}});
 });
}
export async function verifyNativeRuntime(root,record,{directory=join(root,'runtime')}={}){
 need(record?.version===1&&record.phase==='staged'&&Array.isArray(record.runtimeInventory)
  &&record.runtimeInventory.length>0&&record.runtimeInventory.length<=10000,'installation');
 const inventory=new Map();
 for(const f of record.runtimeInventory){
  need(f&&Object.keys(f).sort().join(' ')==='path sha256'&&typeof f.path==='string'
   &&f.path.split('/').every(p=>/^[A-Za-z0-9_.@+-]+$/.test(p)&&p!=='.'&&p!=='..')
   &&/^[a-f0-9]{64}$/.test(f.sha256)&&!inventory.has(f.path),'inventory');inventory.set(f.path,f.sha256);
 }
 const seen=new Set();await privatePath(directory,{directory:true});
 const walk=async relative=>{
  for(const name of await fs.readdir(join(directory,relative))){
   const path=relative?relative+'/'+name:name,absolute=join(directory,path),s=await fs.lstat(absolute);
   if(s.isDirectory()){await privatePath(absolute,{directory:true});need([...inventory.keys()].some(k=>k.startsWith(path+'/')),'directory');await walk(path);}
   else{await privatePath(absolute);need((s.mode&0o777)===0o600&&inventory.has(path)&&sha(await fs.readFile(absolute))===inventory.get(path),'bytes');seen.add(path);}
  }
 };await walk('');need(seen.size===inventory.size,'incomplete');
}
/** Installs the maintained process only. Its live response reports the remaining
 * delivery/registration/handover holds; a running service is not host readiness.
 * These injectable transports are test seams, never public JSON or module paths. */
export function nativeControlService({root,run=nativeSystemctl,status=nativeServiceStatus}={}){
 const unit=join(root,nativeServiceName),receipt=join(root,'control-service.json');
 async function inspect({allowLegacy=false}={}){
  const record=await readPrivate(receipt);need(record.version===1&&record.account===userInfo().uid
   &&/^[a-f0-9-]{36}$/.test(record.generation),'receipt');
  await privatePath(unit);const approved=nativeServiceUnit(root);need(!record.unitTransition&&sha(await fs.readFile(unit))===record.unit
   &&(record.unit===sha(approved)||allowLegacy&&record.unit===sha(approved.replace('KillMode=mixed','KillMode=control-group'))),'unit');
  const r=run(['show',nativeServiceName,'--property=ActiveState','--property=FragmentPath','--property=MainPID','--property=Type']);
  need(r.ok,'observe');const p=Object.fromEntries(r.output.trim().split('\n').map(l=>{const i=l.indexOf('=');return[l.slice(0,i),l.slice(i+1)];}));
  need(p.ActiveState==='active'&&p.Type==='notify'&&/^[1-9][0-9]*$/.test(p.MainPID)
   &&await fs.realpath(p.FragmentPath)===unit,'inactive');
  const live=await status(root),installation=await readPrivate(join(root,'native-installation.json'));
  need(live.version===1&&live.generation===record.generation&&live.pid===Number(p.MainPID)
   &&live.installation===sha(JSON.stringify(installation))&&live.service==='running'
   &&(live.delivery==='ready'&&live.reason==='native-core-ready'&&live.stageEntry==='configured'
    ||live.delivery==='blocked'&&[installation.executionPool?.version===2?'native-stage-composition-pending':'native-delivery-composition-pending',
      'native-project-installation-and-activation-pending','native-owner-restriction'].includes(live.reason))
   &&(installation.executionPool?.version!==2||live.runnerPool==='running'&&live.pool?.host==='running'),'live-identity');
  return {status:'running',delivery:live.delivery,...(live.stageEntry?{stageEntry:live.stageEntry}:{}),
   diagnostics:live.delivery==='ready'?[]:[{capability:'host',code:live.reason}]};
 }
 async function check(){try{return await inspect();}catch{return {status:'blocked',diagnostics:[{capability:'host',code:'native-control-service-unavailable'}]};}}
 async function install(){
  await privatePath(root,{directory:true});let previous=await readPrivate(receipt,{missing:true});const bytes=nativeServiceUnit(root);
  if(previous&&(previous.unit!==sha(bytes)||previous.unitTransition)){await stageUnit();previous=await readPrivate(receipt);}
  if(previous){need(previous.unit===sha(bytes),'preserve-service');await privatePath(unit);need(sha(await fs.readFile(unit))===previous.unit,'preserve-unit');}
  else{
   // A conflicting unit is never overwritten or adopted as a fresh install.
   const h=await fs.open(unit,'wx',0o600);try{await h.writeFile(bytes);await h.sync();}finally{await h.close();}
   await writePrivate(receipt,{version:1,account:userInfo().uid,generation:randomUUID(),unit:sha(bytes)},{exclusive:true});
  }
  for(const args of [['link',unit],['daemon-reload'],['enable',nativeServiceName],['start',nativeServiceName]])need(run(args).ok,'installation-interrupted');
  return inspect();
 }
 async function retire(){
  await privatePath(root,{directory:true});
  const retained=await readPrivate(receipt),bytes=await fs.readFile(unit);
  need(retained.version===1&&retained.account===userInfo().uid&&retained.unit===sha(bytes)
   &&retained.unit===sha(nativeServiceUnit(root)),'retirement-identity');
  const path=join(root,'control-retirement.json');
  const previous=await readPrivate(path,{missing:true});
  if(previous)need(previous.version===1&&previous.generation===retained.generation&&previous.unit===retained.unit,'retirement-intent');
  const observe=()=>{
   const r=run(['show',nativeServiceName,'--property=LoadState','--property=ActiveState','--property=FragmentPath','--property=MainPID','--property=ControlPID','--property=ControlGroup']);
   const value=Object.fromEntries(r.output.trim().split('\n').map(line=>{const i=line.indexOf('=');return[line.slice(0,i),line.slice(i+1)];}));
   need(r.ok||r.status===4&&value.LoadState==='not-found','retirement-observation');return value;
  };
  let observed=observe();
  need(observed.ActiveState==='inactive'&&observed.MainPID==='0'&&observed.ControlPID==='0','retirement-active-work');
  if(observed.LoadState==='not-found'){
   need(observed.FragmentPath===''&&(!previous||['intent','retired'].includes(previous.phase)),'retirement-unknown-service');
   if(!previous||previous.phase==='intent')await writePrivate(path,{version:1,generation:retained.generation,unit:retained.unit,phase:'retired'});
  }
  else{
   need(observed.LoadState==='loaded'&&await fs.realpath(observed.FragmentPath)===unit,'retirement-fragment');
   if(observed.ControlGroup){const group=observed.ControlGroup;need(group.startsWith('/')&&!group.includes('..'),'retirement-group');
    try{need((await fs.readFile('/sys/fs/cgroup'+group+'/cgroup.procs','utf8')).trim()==='','retirement-processes');}catch(e){if(e.code!=='ENOENT')throw e;}}
   await writePrivate(path,{version:1,generation:retained.generation,unit:retained.unit,phase:'intent'});
   need(run(['disable',nativeServiceName]).ok&&run(['daemon-reload']).ok,'retirement-interrupted');
   observed=observe();need(observed.LoadState==='not-found'&&observed.ActiveState==='inactive'
    &&observed.MainPID==='0'&&observed.ControlPID==='0','retirement-unconfirmed');
   await writePrivate(path,{version:1,generation:retained.generation,unit:retained.unit,phase:'retired'});
  }
  return {status:'retired',generation:retained.generation,unit:retained.unit};
 }
 async function stageUnit(){
  await privatePath(root,{directory:true});let saved=await readPrivate(receipt);await privatePath(unit);
  const bytes=nativeServiceUnit(root),current=sha(bytes),legacy=sha(bytes.replace('KillMode=mixed','KillMode=control-group')),stored=sha(await fs.readFile(unit));
  need(saved.version===1&&saved.account===userInfo().uid&&[current,legacy].includes(saved.unit),'stage-unit-identity');
  if(saved.unitTransition)need(saved.unit===legacy&&saved.unitTransition.previous===legacy&&saved.unitTransition.next===current&&[legacy,current].includes(stored),'stage-unit-transition');
  else need(stored===saved.unit,'stage-unit-identity');
  const observe=()=>{const r=run(['show',nativeServiceName,'--property=LoadState','--property=ActiveState','--property=FragmentPath','--property=MainPID','--property=ControlPID']);
   const p=Object.fromEntries(r.output.trim().split('\n').map(line=>{const i=line.indexOf('=');return[line.slice(0,i),line.slice(i+1)];}));
   need(r.ok||r.status===4&&p.LoadState==='not-found','stage-unit-observation');return p;};
  const recoverEmptyFailure=async before=>{
   need(before.ActiveState==='failed'&&before.MainPID==='0'&&before.ControlPID==='0','stage-unit-active');
   const installation=await readPrivate(join(root,'native-installation.json'));
   need(installation.phase==='staged'&&installation.executionPool?.version===2,'failed-stage-installation');
   const core=await import(pathToFileURL(join(root,'runtime/node_modules/@nibfin/core/src/index.mjs')).href);
   const pool=nativePoolStore(core,{root,configuration:installation.executionPool}),status=await pool.status();
   const ledger=await readPrivate(join(root,'pool-state/ledger.json'));
   need(status.occupied===0&&status.pending===0&&Object.keys(ledger.jobs).length===0&&Object.keys(ledger.capacity.completed).length===0,'failed-stage-work');
   const store=await openFileHostStore(core,{directory:join(root,'state')});try{const{work}=await store.read();
    need(work.restrictionsChecked&&work.paused&&work.hold===null&&!work.unknownWrite&&!work.active&&work.terminated&&work.reconciled
     &&['pending','conversations','receipts'].every(k=>work[k].length===0),'failed-stage-work');}finally{await store.close();}
   const{observeUnusedStagedResources}=await import('./native-staged-transition.mjs');
   need((await observeUnusedStagedResources(root,installation.runnerCount)).status==='unused','failed-stage-resources');
   need(run(['reset-failed',nativeServiceName]).ok,'failed-stage-reset');return observe();
  };
  let before=observe();
  if(before.ActiveState==='active'){
   need(before.LoadState==='loaded'&&!saved.unitTransition,'stage-unit-active');let verified=false;
   try{await inspect({allowLegacy:true});const live=await status(root);verified=live.delivery==='blocked'
    &&['native-stage-composition-pending','native-project-installation-and-activation-pending'].includes(live.reason)
    &&live.pool?.occupied===0&&live.pool?.pending===0&&live.pool?.host==='running';}catch{}
   need(verified,'stage-unit-active');need(run(['stop',nativeServiceName]).ok,'stage-unit-stop');before=observe();
  }
  if(before.ActiveState==='failed')before=await recoverEmptyFailure(before);
  need(before.ActiveState==='inactive'&&before.MainPID==='0'&&before.ControlPID==='0','stage-unit-active');
  need(before.LoadState==='not-found'&&before.FragmentPath===''||before.LoadState==='loaded'&&await fs.realpath(before.FragmentPath)===unit,'stage-unit-fragment');
  if(saved.unit!==current||saved.unitTransition){
   const transition={...saved,unitTransition:{previous:legacy,next:current}};await writePrivate(receipt,transition);
   const h=await fs.open(unit,fs.constants.O_WRONLY|fs.constants.O_TRUNC|fs.constants.O_NOFOLLOW);try{await h.writeFile(bytes);await h.sync();}finally{await h.close();}
   saved={...saved,unit:current};delete saved.unitTransition;await writePrivate(receipt,saved);
  }
  for(const args of [['link',unit],['daemon-reload'],['enable',nativeServiceName]])need(run(args).ok,'stage-unit-interrupted');
  const after=observe();need(after.ActiveState==='inactive'&&after.MainPID==='0'&&after.ControlPID==='0'&&after.LoadState==='loaded'&&await fs.realpath(after.FragmentPath)===unit,'stage-unit-readback');
  return {status:'linked'};
 }

 return Object.freeze({check,install,retire,stageUnit});
}

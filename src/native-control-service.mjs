import fs from 'node:fs/promises';
import {join,resolve} from 'node:path';
import {userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {connect} from 'node:net';
import {randomUUID} from 'node:crypto';
import {privatePath,readPrivate,writePrivate,sha} from './local-state.mjs';

const need=(ok,code)=>{if(!ok)throw Error('native-service:'+code);};
export const nativeServiceName='nibfin-control.service';
const quote=value=>{
 need(typeof value==='string'&&resolve(value)===value&&!/[\x00-\x1f\x7f]/.test(value),'path');
 return '"'+value.replace(/\\/g,'\\\\').replace(/"/g,'\\"').replace(/%/g,'%%').replace(/\$/g,()=> '$$')+'"';
};
export function nativeServiceUnit(root,node=process.execPath){
 return `[Unit]\nDescription=Nibfin native control host\nStartLimitIntervalSec=60\nStartLimitBurst=3\n[Service]\nType=notify\nNotifyAccess=all\nTimeoutStartSec=45\nTimeoutStopSec=30\nExecStart=${quote(node)} ${quote(join(root,'runtime/client/native-control-main.mjs'))} ${quote(root)}\nEnvironment=HOME=${quote(userInfo().homedir)}\nEnvironment=PATH=/usr/local/bin:/usr/bin:/bin\nUnsetEnvironment=GH_TOKEN GITHUB_TOKEN NODE_OPTIONS NODE_PATH CODEX_HOME\nUMask=0077\nRestart=on-failure\nNoNewPrivileges=true\nKillMode=control-group\n[Install]\nWantedBy=default.target\n`;
}
export function nativeSystemctl(args){
 const r=spawnSync('/usr/bin/systemctl',['--user',...args],{encoding:'utf8',timeout:60000,maxBuffer:8192,
  env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:userInfo().homedir,LANG:'C',XDG_RUNTIME_DIR:'/run/user/'+userInfo().uid}});
 return {ok:!r.error&&r.status===0,output:r.status===0?r.stdout:''};
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
export async function verifyNativeRuntime(root,record){
 need(record?.version===1&&record.phase==='staged'&&Array.isArray(record.runtimeInventory)
  &&record.runtimeInventory.length>0&&record.runtimeInventory.length<=10000,'installation');
 const inventory=new Map();
 for(const f of record.runtimeInventory){
  need(f&&Object.keys(f).sort().join(' ')==='path sha256'&&typeof f.path==='string'
   &&f.path.split('/').every(p=>/^[A-Za-z0-9_.@+-]+$/.test(p)&&p!=='.'&&p!=='..')
   &&/^[a-f0-9]{64}$/.test(f.sha256)&&!inventory.has(f.path),'inventory');inventory.set(f.path,f.sha256);
 }
 const seen=new Set(),directory=join(root,'runtime');await privatePath(directory,{directory:true});
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
 async function inspect(){
  const record=await readPrivate(receipt);need(record.version===1&&record.account===userInfo().uid
   &&/^[a-f0-9-]{36}$/.test(record.generation),'receipt');
  await privatePath(unit);need(sha(await fs.readFile(unit))===record.unit&&record.unit===sha(nativeServiceUnit(root)),'unit');
  const r=run(['show',nativeServiceName,'--property=ActiveState','--property=FragmentPath','--property=MainPID','--property=Type']);
  need(r.ok,'observe');const p=Object.fromEntries(r.output.trim().split('\n').map(l=>{const i=l.indexOf('=');return[l.slice(0,i),l.slice(i+1)];}));
  need(p.ActiveState==='active'&&p.Type==='notify'&&/^[1-9][0-9]*$/.test(p.MainPID)
   &&await fs.realpath(p.FragmentPath)===unit,'inactive');
  const live=await status(root),installation=await readPrivate(join(root,'native-installation.json'));
  need(live.version===1&&live.generation===record.generation&&live.pid===Number(p.MainPID)
   &&live.installation===sha(JSON.stringify(installation))&&live.service==='running'
   &&live.delivery==='blocked'&&live.reason==='native-delivery-composition-pending','live-identity');
  return {status:'running',delivery:'blocked',diagnostics:[{capability:'host',code:live.reason}]};
 }
 async function check(){try{return await inspect();}catch{return {status:'blocked',diagnostics:[{capability:'host',code:'native-control-service-unavailable'}]};}}
 async function install(){
  await privatePath(root,{directory:true});const previous=await readPrivate(receipt,{missing:true}),bytes=nativeServiceUnit(root);
  if(previous){need(previous.unit===sha(bytes),'preserve-service');await privatePath(unit);need(sha(await fs.readFile(unit))===previous.unit,'preserve-unit');}
  else{
   // A conflicting unit is never overwritten or adopted as a fresh install.
   const h=await fs.open(unit,'wx',0o600);try{await h.writeFile(bytes);await h.sync();}finally{await h.close();}
   await writePrivate(receipt,{version:1,account:userInfo().uid,generation:randomUUID(),unit:sha(bytes)},{exclusive:true});
  }
  for(const args of [['link',unit],['daemon-reload'],['enable',nativeServiceName],['start',nativeServiceName]])need(run(args).ok,'installation-interrupted');
  return inspect();
 }
 return Object.freeze({check,install});
}

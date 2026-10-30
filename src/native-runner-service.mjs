import fs from 'node:fs/promises';
import {join,resolve} from 'node:path';
import {userInfo} from 'node:os';
import {privatePath,readPrivate,writePrivate,sha} from './local-state.mjs';
import {nativeSystemctl} from './native-control-service.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-runner-service:'+code);};
const quote=value=>{
 need(typeof value==='string'&&resolve(value)===value&&!/[\x00-\x1f\x7f]/.test(value),'path');
 return '"'+value.replace(/\\/g,'\\\\').replace(/"/g,'\\"').replace(/%/g,'%%').replace(/\$/g,()=> '$$')+'"';
};
export const nativeRunnerUnitName=slot=>{
 need(Number.isSafeInteger(slot)&&slot>=1&&slot<=128,'slot');return 'nibfin-runner-'+slot+'.service';
};
export function nativeRunnerUnit(root,selector,slot,node=process.execPath){
 need(typeof selector==='string'&&/^[A-Za-z0-9_-]{1,80}$/.test(selector),'selector');nativeRunnerUnitName(slot);
 return `[Unit]\nDescription=Nibfin ephemeral Runner slot ${slot}\n[Service]\nType=exec\nExecStart=${quote(node)} ${quote(join(root,'runtime/client/native-runner-main.mjs'))} ${quote(root)} "${selector}"\nEnvironment=HOME=${quote(userInfo().homedir)}\nEnvironment=PATH=/usr/local/bin:/usr/bin:/bin\nUnsetEnvironment=GH_TOKEN GITHUB_TOKEN NODE_OPTIONS NODE_PATH CODEX_HOME OPENAI_API_KEY ANTHROPIC_API_KEY GEMINI_API_KEY\nUMask=0077\nRestart=no\nKillMode=control-group\nTimeoutStopSec=45\nNoNewPrivileges=true\nStandardOutput=null\nStandardError=journal\n`;
}
export function nativeRunnerService({root,selector,slot,lease,run=nativeSystemctl}={}){
 need(typeof lease==='string'&&/^[a-f0-9]{64}$/.test(lease),'lease');
 const name=nativeRunnerUnitName(slot),unit=join(root,name),directory=join(root,'jobs',lease),path=join(directory,'service.json');
 const bytes=nativeRunnerUnit(root,selector,slot);
 async function state(){
  const receipt=await readPrivate(path);need(receipt.version===1&&receipt.lease===lease&&receipt.selector===selector
   &&receipt.unit===sha(bytes),'receipt');await privatePath(unit);need(sha(await fs.readFile(unit))===receipt.unit,'unit');
  const observed=run(['show',name,'--property=ActiveState','--property=MainPID','--property=InvocationID','--property=ControlGroup','--property=FragmentPath']);
  need(observed.ok,'observe');const properties=Object.fromEntries(observed.output.trim().split('\n').map(line=>{const at=line.indexOf('=');return[line.slice(0,at),line.slice(at+1)];}));
   need(await fs.realpath(properties.FragmentPath)===unit&&/^[0-9]+$/.test(properties.MainPID),'service-identity');
   if(properties.InvocationID===''&&['inactive','failed'].includes(properties.ActiveState)&&properties.MainPID==='0'){
    // systemd clears InvocationID after a successful one-shot service exit.
    // Retain attribution to the original terminated process, never infer that
    // missing process evidence or an active replacement is the same execution.
    const process=await readPrivate(join(directory,'process.json'));
    need(process.phase==='terminated'&&process.lease===lease&&/^[a-f0-9]{32}$/.test(process.invocation),'service-identity');
    properties.InvocationID=process.invocation;
   }
   need(/^[a-f0-9]{32}$/.test(properties.InvocationID),'service-identity');
  need(properties.ControlGroup===''||properties.ControlGroup.startsWith('/user.slice/user-'+userInfo().uid+'.slice/')
   &&properties.ControlGroup.split('/').slice(1).every(p=>/^[A-Za-z0-9_.@:-]+$/.test(p)),'control-group');
  return {status:properties.ActiveState,pid:Number(properties.MainPID),invocation:properties.InvocationID,controlGroup:properties.ControlGroup};
 }
 const absent=async()=>{
  const result=run(['show',name,'--property=LoadState','--property=ActiveState','--property=MainPID','--property=FragmentPath']);
  if(!result.ok&&result.status!==4)return false;
  const p=Object.fromEntries(result.output.trim().split('\n').map(line=>{const at=line.indexOf('=');return[line.slice(0,at),line.slice(at+1)];}));
  return p.LoadState==='not-found'&&p.ActiveState==='inactive'&&p.MainPID==='0'&&p.FragmentPath==='';
 };
 return Object.freeze({state,absent,
  async start(){
   await privatePath(root,{directory:true});await privatePath(directory,{directory:true});
   // The Core owner has retained launch-intent before calling us. Never adopt an
   // existing unit or enable/restart a persistent listener after an unknown call.
   await writePrivate(path,{version:1,lease,selector,unit:sha(bytes)},{exclusive:true});
   const file=await fs.open(unit,'wx',0o600);try{await file.writeFile(bytes);await file.sync();}finally{await file.close();}
   for(const args of [['link',unit],['daemon-reload'],['start',name]])need(run(args).ok,'start-unconfirmed');
   const observed=await state();need(['active','activating'].includes(observed.status)&&observed.pid>0,'start-unconfirmed');
   return {status:'started',lease};
  },
  async retire(){
   const receipt=await readPrivate(path);need(receipt.version===1&&receipt.lease===lease&&receipt.selector===selector&&receipt.unit===sha(bytes),'receipt');
   if(!await absent()){
    const observed=await state();need(['inactive','failed'].includes(observed.status)&&observed.pid===0,'still-running');
    await writePrivate(path,{...receipt,retirement:'intent'});
    const commands=[['stop',name],...(observed.status==='failed'?[['reset-failed',name]]:[]),['disable',name],['daemon-reload']];
    for(const args of commands)need(run(args).ok,'retirement-unconfirmed');
   }else need(['intent','completed'].includes(receipt.retirement),'retirement-intent-missing');
   need(await absent(),'retirement-readback');
   try{await privatePath(unit);need(sha(await fs.readFile(unit))===receipt.unit,'unit');await fs.unlink(unit);}
   catch(error){if(error.code!=='ENOENT')throw error;}
   await writePrivate(path,{...receipt,retirement:'completed'});return {status:'retired',lease};
  }
 });
}

import fs from 'node:fs/promises';
import {createReadStream} from 'node:fs';
import {createHash} from 'node:crypto';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {privatePath,readPrivate,writePrivate} from './local-state.mjs';
import {maintenanceLock} from './upgrade.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-cleanup:'+code);};
const inside=(value,root)=>value===root||value.startsWith(root+'/');
const absent=async path=>{try{await fs.lstat(path);return false;}catch(e){if(e.code==='ENOENT')return true;throw e;}};
async function unused(roots){
 // Check actual user processes, open descriptors and visible mounts. Model
 // containers and their mounts additionally require the fresh custody proof.
 for(const id of await fs.readdir('/proc')){
  if(!/^[0-9]+$/.test(id))continue;const directory='/proc/'+id;
  try{
   if((await fs.stat(directory)).uid!==userInfo().uid)continue;
   const values=[];
   for(const file of ['cmdline','environ','maps']){
    try{values.push(...(await fs.readFile(join(directory,file),'utf8')).split('\0'));}catch(e){if(e.code!=='ENOENT')throw e;}
   }
   for(const file of ['cwd','root','exe']){try{values.push(await fs.readlink(join(directory,file)));}catch(e){if(e.code!=='ENOENT')throw e;}}
   try{for(const fd of await fs.readdir(join(directory,'fd'))){try{values.push(await fs.readlink(join(directory,'fd',fd)));}catch(e){if(e.code!=='ENOENT')throw e;}}}
   catch(e){if(e.code!=='ENOENT')throw e;}
   need(!values.some(value=>roots.some(root=>inside(value,root)||value.includes('='+root)||value.includes(root+'/'))),'process-reference');
  }catch(e){if(e.code!=='ENOENT'&&e.code!=='ESRCH')throw e;}
 }
 const mounts=(await fs.readFile('/proc/self/mountinfo','utf8')).split('\n').filter(Boolean);
 for(const line of mounts){const target=line.split(' ')[4]?.replace(/\\([0-7]{3})/g,(_,value)=>String.fromCharCode(parseInt(value,8)));
  need(target&&!roots.some(root=>inside(target,root)),'mount-reference');}
}
async function inventory(root){
 const entries=[];let bytes=0;
 const walk=async relative=>{
  const path=relative?join(root,relative):root,s=await fs.lstat(path);
  need(s.uid===userInfo().uid,'owner');need(entries.length<20000&&bytes<=10737418240,'inventory-limit');
  if(s.isDirectory()){
   entries.push([relative,'directory',s.mode&0o777]);
   for(const name of (await fs.readdir(path)).sort())await walk(relative?relative+'/'+name:name);
  }else if(s.isFile()){
   need(s.nlink===1,'linked-file');const hash=createHash('sha256');
   for await(const chunk of createReadStream(path)){bytes+=chunk.length;need(bytes<=10737418240,'inventory-limit');hash.update(chunk);}
   entries.push([relative,'file',s.mode&0o777,s.size,hash.digest('hex')]);
  }else if(s.isSymbolicLink())entries.push([relative,'link',await fs.readlink(path)]);
  else need(false,'special-file');
 };await walk('');need(Buffer.byteLength(JSON.stringify(entries))<=12582912,'inventory-limit');
 return {identity:createHash('sha256').update(JSON.stringify(entries)).digest('hex'),files:entries.length,bytes,entries};
}
/** Owned disposable Runner tree only. The caller has already retained original
 * work, independently settled model resources, and retired the exact service.
 * Intent/atomic parking/removal readback survive a crash without following links,
 * inventing cleanup, or deleting a replacement at the original pathname.
 */
export async function retireNativeApplication(core,{root,lease,outcome,confirm}={}){
 need(/^[a-f0-9]{64}$/.test(lease??'')&&/^[a-f0-9]{64}$/.test(outcome??'')&&typeof confirm==='function','binding');
 await privatePath(root,{directory:true});const state=join(root,'pool-state');await privatePath(state,{directory:true});
 await privatePath(join(root,'jobs'),{directory:true});
 const original=join(root,'jobs',lease),parked=join(root,'jobs','retired-'+lease),path=join(state,'cleanup-'+lease+'.json');
 const seal=value=>({...value,identity:core.contentIdentity(value)});
 const syncJobs=async()=>{const directory=await fs.open(join(root,'jobs'),'r');try{await directory.sync();}finally{await directory.close();}};
 const save=async value=>{const next=seal(value);await writePrivate(path,next);return next;};
 return maintenanceLock(state,async assertLease=>{
  const fresh=async()=>{assertLease();need((await confirm())===true,'custody-or-service-unconfirmed');};
  await fresh();let record=await readPrivate(path,{missing:true,limit:16777216});
  if(record){const {identity,...body}=record;need(identity===core.contentIdentity(body)&&record.lease===lease&&record.outcome===outcome
   &&['parking-intent','parked','remove-intent','completed'].includes(record.phase),'journal');}
  else{
   await privatePath(original,{directory:true});need(await absent(parked),'parked-conflict');await unused([original,parked]);
   const s=await fs.lstat(original),snapshot=await inventory(original);
   record=await save({version:1,lease,outcome,phase:'parking-intent',inode:s.ino,device:s.dev,inventory:snapshot});
  }
  const update=async phase=>{const {identity,...body}=record;record=await save({...body,phase});};
  const check=async(directory,{partial=false}={})=>{
   await privatePath(directory,{directory:true});const s=await fs.lstat(directory);
   need(s.ino===record.inode&&s.dev===record.device,'tree-changed');const current=await inventory(directory);
   if(!partial)need(current.identity===record.inventory.identity,'tree-changed');
   else{
    need(Array.isArray(record.inventory.entries),'missing-removal-inventory');
    const originalEntries=new Map(record.inventory.entries.map(entry=>[entry[0],JSON.stringify(entry)]));
    need(current.entries.every(entry=>originalEntries.get(entry[0])===JSON.stringify(entry)),'tree-changed');
   }
  };
  if(record.phase==='completed'){
   need(await absent(original)&&await absent(parked),'completed-path-reused');return {status:'completed',lease,identity:record.identity};
  }
  if(record.phase==='parking-intent'){
   if(!await absent(original)){
    need(await absent(parked),'parked-conflict');await check(original);await unused([original,parked]);await fresh();
    await fs.rename(original,parked);await syncJobs();
   }else need(!await absent(parked),'original-missing');
   await check(parked);await update('parked');
  }
  if(record.phase==='parked'){
   need(await absent(original),'original-path-reused');await check(parked);await unused([original,parked]);await fresh();
   await update('remove-intent');
  }
  if(record.phase==='remove-intent'){
   need(await absent(original),'original-path-reused');
   if(!await absent(parked)){
    await check(parked,{partial:true});await unused([original,parked]);await fresh();
    // Both absolute targets are fixed descendants of the checked private jobs
    // directory. Symlinks are inventoried and never followed by recursive rm.
    await fs.rm(parked,{recursive:true});await syncJobs();
   }
   await fresh();need(await absent(original)&&await absent(parked),'removal-unconfirmed');
   const {entries,...summary}=record.inventory;record={...record,inventory:summary};await update('completed');
  }
  return {status:'completed',lease,identity:record.identity};
 });
}

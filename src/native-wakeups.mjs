import fs from'node:fs/promises';import{watch}from'node:fs';import{join}from'node:path';
import{ensurePrivateDirectory,readPrivate,writePrivate}from'./local-state.mjs';import{maintenanceLock}from'./upgrade.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-wakeup:'+code);};
export async function nativeWakeupInbox(core,{root}={}){
 const directory=join(root,'wakeups');await ensurePrivateDirectory(directory);
 const read=async id=>{need(/^native-fanout-[a-f0-9]{64}$/.test(id),'selector');const value=await readPrivate(join(directory,id+'.json'),{missing:true});
  if(value)need(value.version===1&&core.contentIdentity(value.value)===value.identity&&value.value.id===id,'integrity');return value;};
 return Object.freeze({
  async retain(value,{beforeSubmit}={}){
   need(typeof beforeSubmit==='function','guard');return maintenanceLock(directory,async()=>{
    const existing=await read(value.id),identity=core.contentIdentity(value);await beforeSubmit();
    if(existing)need(existing.identity===identity,'conflict');
    else await writePrivate(join(directory,value.id+'.json'),{version:1,value,identity},{exclusive:true});
    need((await read(value.id)).identity===identity,'readback');return {outcome:'retained',identity};
   });
  },
  async list(){const names=await fs.readdir(directory);need(names.length<=10000,'bound');const result=[];
   for(const name of names){if(name==='maintenance.lock')continue;need(/^native-fanout-[a-f0-9]{64}\.json$/.test(name),'inventory');
    result.push(await read(name.slice(0,-5)));}return result;
  },subscribe(notify){const watcher=watch(directory,(_event,name)=>{if(name===null||name.toString().endsWith('.json'))notify();});
   watcher.on('error',()=>notify({unavailable:true}));return()=>watcher.close();}
 });
}

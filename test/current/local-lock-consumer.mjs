import test from 'node:test';import assert from 'node:assert/strict';
import fs from 'node:fs/promises';import {join} from 'node:path';import {spawn} from 'node:child_process';
import {withCurrentCore} from '../../src/current-core.mjs';import {localLock} from '../../src/local-state.mjs';

test('installation locks survive process death without retaining a false busy flag or deleting the inode',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'locks');await fs.mkdir(root,{mode:0o700});
 const module=new URL('../../src/local-state.mjs',import.meta.url).href;
 const code='import {localLock} from '+JSON.stringify(module)+';await localLock('+JSON.stringify(root)+',"installation",async()=>{process.stdout.write("held\\n");await new Promise(()=>{});});';
 const child=spawn(process.execPath,['--input-type=module','-e',code],{stdio:['ignore','pipe','pipe'],env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:root}});
 const ended=new Promise(resolve=>child.once('close',resolve));
 try{
  await new Promise((resolve,reject)=>{let text='';const timer=setTimeout(()=>reject(Error('lock child unavailable')),10000);
   child.stdout.on('data',bytes=>{text+=bytes;if(text==='held\n'){clearTimeout(timer);resolve();}});child.once('error',reject);});
  const before=await fs.stat(join(root,'.installation.lock'));
  await assert.rejects(localLock(root,'installation',()=>assert.fail('concurrent lock')),/maintenance-lock-unavailable/);
  child.kill('SIGKILL');await ended;
  let acquired=false;for(let n=0;n<100&&!acquired;n++){
   try{await localLock(root,'installation',()=>{acquired=true;});}catch(e){if(!e.message.includes('maintenance-lock-unavailable'))throw e;await new Promise(r=>setTimeout(r,20));}}
  assert.equal(acquired,true);assert.equal((await fs.stat(join(root,'.installation.lock'))).ino,before.ino);
 }finally{child.kill('SIGKILL');await ended;}
}));

test('a dead legacy PID is reconciled under the kernel lease, while live or malformed legacy ownership remains held',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'legacy-lock');await fs.mkdir(root,{mode:0o700});const path=join(root,'.installation.lock');
 const dead=spawn(process.execPath,['-e',''],{stdio:'ignore'});await new Promise(r=>dead.once('close',r));
 await fs.writeFile(path,String(dead.pid),{mode:0o600});const inode=(await fs.stat(path)).ino;
 assert.equal(await localLock(root,'installation',()=> 'continued'),'continued');
 assert.equal(await fs.readFile(path,'utf8'),'');assert.equal((await fs.stat(path)).ino,inode);
 await fs.writeFile(path,String(process.pid));await assert.rejects(localLock(root,'installation',()=>assert.fail()),/still running/);
 assert.equal(await fs.readFile(path,'utf8'),String(process.pid));
 await fs.writeFile(path,'unconfirmed');await assert.rejects(localLock(root,'installation',()=>assert.fail()),/requires reconciliation/);
 assert.equal(await fs.readFile(path,'utf8'),'unconfirmed');
}));

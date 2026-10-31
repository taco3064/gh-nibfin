import test from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{tmpdir}from'node:os';
import{readPrivate,writePrivate}from'../../src/local-state.mjs';

test('private readers reobserve a transient publication link but never accept a persistent hardlink, public file or symlink',async t=>{
 const root=await fs.mkdtemp(join(tmpdir(),'nibfin-private-read-'));t.after(()=>fs.rm(root,{recursive:true,force:true}));
 const pending=join(root,'pending'),path=join(root,'record.json');await fs.writeFile(pending,'{"version":1}',{mode:0o600});await fs.link(pending,path);
 const publication=new Promise(resolve=>setTimeout(async()=>{await fs.unlink(pending);resolve();},2));
 assert.deepEqual(await readPrivate(path),{version:1});await publication;
 await fs.link(path,pending);await assert.rejects(readPrivate(path),/metadata|permissions/);assert.equal((await fs.stat(path)).nlink,2);
 await fs.unlink(pending);await fs.chmod(path,0o644);await assert.rejects(readPrivate(path),/metadata|permissions/);
 await fs.chmod(path,0o600);await fs.symlink(path,pending);await assert.rejects(readPrivate(pending),/link/);
});

test('concurrent atomic replacement returns whole private records and preserves publication integrity',async t=>{
 const root=await fs.mkdtemp(join(tmpdir(),'nibfin-private-snapshot-'));t.after(()=>fs.rm(root,{recursive:true,force:true}));
 const path=join(root,'record.json');await writePrivate(path,{sequence:0,body:'0'.repeat(4096)},{exclusive:true});
 await Promise.all([
  (async()=>{for(let n=1;n<=80;n++)await writePrivate(path,{sequence:n,body:String(n).repeat(4096)});})(),
  (async()=>{for(let n=0;n<160;n++){const r=await readPrivate(path);assert.equal(r.body,String(r.sequence).repeat(4096));}})()
 ]);
 assert.equal((await readPrivate(path)).sequence,80);assert.equal((await fs.stat(path)).mode&0o777,0o600);
});

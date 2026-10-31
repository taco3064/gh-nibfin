import fs from 'node:fs/promises';
import {join,resolve,dirname} from 'node:path';
import {userInfo} from 'node:os';
import {createHash,randomUUID} from 'node:crypto';
export const sha=bytes=>createHash('sha256').update(bytes).digest('hex');
// Shared by all storage adapters in this host process. A reader must not
// inspect an inode halfway through another adapter's atomic publication.
const publications=new Map();
export async function privatePath(path,{directory=false,missing=false}={}){
  if(typeof path!=='string'||resolve(path)!==path||path==='/'||/[\x00-\x1f\x7f]/.test(path))throw Error('Unsafe private path.');
  for(let p=path;p!=='/';p=dirname(p)){
    let s;try{s=await fs.lstat(p);}catch(e){if(missing&&e.code==='ENOENT')continue;throw e;}
    if(s.isSymbolicLink()||p!==path&&!s.isDirectory())throw Error('Private path contains a link.');
    if(p===path&&((directory?!s.isDirectory():!s.isFile())||s.uid!==userInfo().uid||s.mode&0o077||!directory&&s.nlink!==1))throw Object.assign(Error('Private path ownership, type or permissions mismatch.'),{code:'PRIVATE_PATH_METADATA'});
  }
  return path;
}
export async function readPrivate(path,{missing=false,limit=1048576}={}){
  for(let pending=publications.get(path);pending;pending=publications.get(path))await pending;
  // Atomic publication can expose a short-lived second link (exclusive create)
  // or unlink a reader's previous inode (replacement). Reobserve locally; never
  // accept unsafe metadata. Validate the actual descriptor as well as the path.
  for(let attempt=0;;attempt++)try{
    await privatePath(path);const h=await fs.open(path,fs.constants.O_RDONLY|fs.constants.O_NOFOLLOW|fs.constants.O_NONBLOCK);
    try{const s=await h.stat();
      if(!s.isFile()||s.uid!==userInfo().uid||s.mode&0o077||s.nlink!==1)throw Object.assign(Error('Private record descriptor metadata mismatch.'),{code:'PRIVATE_PATH_METADATA'});
      if(s.size>limit)throw Error('Oversized private record.');return JSON.parse(await h.readFile('utf8'));
    }finally{await h.close();}
  }catch(e){if(missing&&e.code==='ENOENT')return null;
    if(e.code!=='PRIVATE_PATH_METADATA'||attempt>=7)throw e;
    await new Promise(resolve=>setTimeout(resolve,1));
  }
}
export async function ensurePrivateDirectory(path){
  await privatePath(path,{directory:true,missing:true});await fs.mkdir(path,{recursive:true,mode:0o700});await privatePath(path,{directory:true});return path;
}
async function publishPrivate(path,value,{exclusive=false}={}){
  await privatePath(dirname(path),{directory:true});await privatePath(path,{missing:true});
  const pending=path+'.'+randomUUID();const h=await fs.open(pending,'wx',0o600);
  try{await h.writeFile(JSON.stringify(value,null,2)+'\n');await h.sync();}finally{await h.close();}
  try{if(exclusive){await fs.link(pending,path);await fs.unlink(pending);}else await fs.rename(pending,path);
    const d=await fs.open(dirname(path),'r');try{await d.sync();}finally{await d.close();}
  }catch(e){await fs.rm(pending,{force:true});throw e;}
}
export async function writePrivate(path,value,options={}){
  const previous=publications.get(path)??Promise.resolve();
  const next=previous.then(()=>publishPrivate(path,value,options)),settled=next.catch(()=>{});
  publications.set(path,settled);
  try{return await next;}finally{if(publications.get(path)===settled)publications.delete(path);}
}
// Persistent kernel lease: process termination releases exclusivity. Reconcile
// a legacy PID file under that lease without deleting its inode or work state.
export async function localLock(root,name,work){
  if(typeof name!=='string'||!/^[a-z][a-z0-9-]*$/.test(name))throw Error('Invalid local lock name.');
  const {maintenanceLock}=await import('./kernel-lock.mjs');
  return maintenanceLock(root,async()=>{
    const path=join(root,'.'+name+'.lock');await privatePath(path);
    const retained=await fs.readFile(path,'utf8');
    if(retained!==''){
      if(!/^[1-9][0-9]{0,9}$/.test(retained))throw Error('Legacy lock requires reconciliation.');
      const pid=Number(retained);let gone=false;
      try{process.kill(pid,0);}catch(e){if(e.code==='ESRCH')gone=true;else throw Error('Legacy lock owner is unconfirmed.');}
      if(!gone)throw Error('Legacy lock owner is still running.');
      const h=await fs.open(path,fs.constants.O_WRONLY|fs.constants.O_NOFOLLOW);
      try{const before=await fs.lstat(path),held=await h.stat();
        if(before.ino!==held.ino||before.dev!==held.dev||held.nlink!==1)throw Error('Lock identity changed.');
        await h.truncate(0);await h.sync();
      }finally{await h.close();}
    }
    return work();
  },{name:'.'+name+'.lock'});
}

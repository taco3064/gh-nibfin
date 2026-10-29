import fs from 'node:fs/promises';
import {join,resolve,dirname} from 'node:path';
import {userInfo} from 'node:os';
import {createHash,randomUUID} from 'node:crypto';
export const sha=bytes=>createHash('sha256').update(bytes).digest('hex');
export async function privatePath(path,{directory=false,missing=false}={}){
  if(typeof path!=='string'||resolve(path)!==path||path==='/'||/[\x00-\x1f\x7f]/.test(path))throw Error('Unsafe private path.');
  for(let p=path;p!=='/';p=dirname(p)){
    let s;try{s=await fs.lstat(p);}catch(e){if(missing&&e.code==='ENOENT')continue;throw e;}
    if(s.isSymbolicLink()||p!==path&&!s.isDirectory())throw Error('Private path contains a link.');
    if(p===path&&((directory?!s.isDirectory():!s.isFile())||s.uid!==userInfo().uid||s.mode&0o077||!directory&&s.nlink!==1))throw Error('Private path ownership, type or permissions mismatch.');
  }
  return path;
}
export async function readPrivate(path,{missing=false,limit=1048576}={}){
  try{await privatePath(path);const h=await fs.open(path,fs.constants.O_RDONLY|fs.constants.O_NOFOLLOW);
    try{const s=await h.stat();if(s.size>limit)throw Error('Oversized private record.');return JSON.parse(await h.readFile('utf8'));}finally{await h.close();}
  }catch(e){if(missing&&e.code==='ENOENT')return null;throw e;}
}
export async function ensurePrivateDirectory(path){
  await privatePath(path,{directory:true,missing:true});await fs.mkdir(path,{recursive:true,mode:0o700});await privatePath(path,{directory:true});return path;
}
export async function writePrivate(path,value,{exclusive=false}={}){
  await privatePath(dirname(path),{directory:true});await privatePath(path,{missing:true});
  const pending=path+'.'+randomUUID();const h=await fs.open(pending,'wx',0o600);
  try{await h.writeFile(JSON.stringify(value,null,2)+'\n');await h.sync();}finally{await h.close();}
  try{if(exclusive){await fs.link(pending,path);await fs.unlink(pending);}else await fs.rename(pending,path);
    const d=await fs.open(dirname(path),'r');try{await d.sync();}finally{await d.close();}
  }catch(e){await fs.rm(pending,{force:true});throw e;}
}
export async function localLock(root,name,work){
  await privatePath(root,{directory:true});const file=join(root,'.'+name+'.lock'),h=await fs.open(file,'wx',0o600);
  try{await h.writeFile(String(process.pid));await h.sync();return await work();}finally{await h.close();await fs.unlink(file);}
}

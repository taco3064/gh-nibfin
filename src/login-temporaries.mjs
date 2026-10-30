import fs from'node:fs/promises';import{join,resolve}from'node:path';import{userInfo}from'node:os';
import{privatePath}from'./local-state.mjs';
const need=(ok,code)=>{if(!ok)throw Error('login-temporaries:'+code);};
async function activeUsers(loginSpace){
 const root=join(loginSpace,'tmp'),ids=[];const inside=value=>value===root||value.startsWith(root+'/');
 for(const id of await fs.readdir('/proc')){
  if(!/^[0-9]+$/.test(id)||Number(id)===process.pid)continue;
  let status;try{status=await fs.readFile('/proc/'+id+'/status','utf8');}catch(e){if(e.code==='ENOENT'||e.code==='ESRCH')continue;throw e;}
  if(Number(status.match(/^Uid:\s+(\d+)/m)?.[1])!==userInfo().uid)continue;
  try{
   const args=await fs.readFile('/proc/'+id+'/cmdline','utf8'),env=(await fs.readFile('/proc/'+id+'/environ','utf8')).split('\0');
   if(args.includes(root)||env.includes('CODEX_HOME='+loginSpace)||env.some(v=>v.startsWith('PATH=')&&v.includes(root))){ids.push(id);continue;}
   const names=await fs.readdir('/proc/'+id+'/fd');
   for(const name of names){let target;try{target=await fs.readlink('/proc/'+id+'/fd/'+name);}catch(e){if(e.code==='ENOENT')continue;throw e;}if(inside(target)){ids.push(id);break;}}
  }catch(e){if(!['ENOENT','ESRCH'].includes(e.code))throw e;}
 }
 return ids;
}
/** Retire only Codex CLI's recognized disposable arg0 helpers after login exits.
 * Never traverse their links, touch auth.json, remove session data or relax the
 * backend's filesystem isolation checks. Unknown or active resources stay intact.
 */
export async function pruneCodexLoginTemporaries(loginSpace,{processes=activeUsers}={}){
 need(typeof loginSpace==='string'&&resolve(loginSpace)===loginSpace&&typeof processes==='function','configuration');
 await privatePath(loginSpace,{directory:true});const root=join(loginSpace,'tmp');
 try{const s=await fs.lstat(root);need(s.isDirectory()&&!s.isSymbolicLink()&&s.uid===userInfo().uid,'temporary-root');}
 catch(e){if(e.code==='ENOENT')return{status:'absent',removed:0};throw e;}
 const top=await fs.readdir(root);need(top.length===1&&top[0]==='arg0-linux','unknown-layout');
 const directory=join(root,'arg0-linux'),parent=await fs.lstat(directory);
 need(parent.isDirectory()&&!parent.isSymbolicLink()&&parent.uid===userInfo().uid,'helper-parent');
 const names=await fs.readdir(directory);need(names.length<=128,'helper-count');
 for(const name of names){
  need(/^codex-arg0[A-Za-z0-9]{6}$/.test(name),'helper-name');const path=join(directory,name),s=await fs.lstat(path);
  need(s.isDirectory()&&!s.isSymbolicLink()&&s.uid===userInfo().uid,'helper-directory');
  const files=(await fs.readdir(path)).sort();need(JSON.stringify(files)===JSON.stringify(['.lock','apply_patch','applypatch','codex-linux-sandbox'].sort()),'unknown-helper-data');
  for(const file of files){const s=await fs.lstat(join(path,file));need(s.uid===userInfo().uid&&s.nlink===1,'helper-ownership');
   need(file==='.lock'?s.isFile()&&s.size===0:s.isSymbolicLink(),'helper-type');}
 }
 need((await processes(loginSpace)).length===0,'active-helper');
 // Recheck immediately before retirement; the surrounding environment/host
 // maintenance owner serializes supported login operations for this account.
 need((await processes(loginSpace)).length===0,'active-helper');
 for(const name of names){const path=join(directory,name);for(const file of await fs.readdir(path))await fs.unlink(join(path,file));await fs.rmdir(path);}
 await fs.rmdir(directory);await fs.rmdir(root);return{status:'removed',removed:names.length};
}

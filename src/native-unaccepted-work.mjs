import fs from'node:fs/promises';import{join,resolve,dirname,sep}from'node:path';import{userInfo}from'node:os';
import{privatePath,sha}from'./local-state.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-unaccepted-work:'+code);};
// Only a terminated role's worktree is retained here. Service directories and
// credential refresh files are excluded; Core separately retains the session.
export async function retainUnacceptedWorkspace({directory,execution,retain}={}){
 await privatePath(directory,{directory:true});let count=0,bytes=0;const entries=[];
 async function walk(relative=''){
  for(const name of(await fs.readdir(join(directory,relative))).sort()){
   need(!/[\x00-\x1f]/.test(name)&&name!=='.'&&name!=='..','name');need(++count<=20000,'file-limit');
   const path=relative?relative+'/'+name:name,file=join(directory,path),before=await fs.lstat(file);
   need(before.uid===userInfo().uid,'owner');
   if(before.isDirectory()){entries.push({path,kind:'directory',mode:before.mode&0o777});await walk(path);}
   else{
    need(before.isFile()||before.isSymbolicLink(),'type');let data;
    if(before.isSymbolicLink())data=Buffer.from(await fs.readlink(file));
    else{need(before.nlink===1&&before.size<=16000000,'file');const h=await fs.open(file,fs.constants.O_RDONLY|fs.constants.O_NOFOLLOW);
     try{const s=await h.stat();need(s.ino===before.ino&&s.dev===before.dev&&s.size===before.size,'changed');data=await h.readFile();}finally{await h.close();}}
    bytes+=data.length;need(bytes<=48000000,'byte-limit');
    entries.push({path,kind:before.isSymbolicLink()?'symlink':'file',mode:before.mode&0o777,sha256:sha(data),data:data.toString('base64')});
   }
   const after=await fs.lstat(file);need(after.ino===before.ino&&after.dev===before.dev&&after.size===before.size&&after.mtimeMs===before.mtimeMs,'changed');
  }
 }
 await walk();const reference=await retain({kind:'native-unaccepted-workspace',status:'UNACCEPTED',execution,bytes,entries});
 need(typeof reference==='string'&&reference.length>0,'retention');return reference;
}

// Restore only into a new, empty isolated Writer workspace. These bytes remain
// UNACCEPTED and must pass Candidate, hooks, independent Accept and exact-head CI.
export async function restoreUnacceptedWorkspace({directory,snapshot,execution}={}){
 await privatePath(directory,{directory:true});need((await fs.readdir(directory)).length===0,'restore-not-empty');
 need(snapshot?.kind==='native-unaccepted-workspace'&&snapshot.status==='UNACCEPTED'&&snapshot.execution.id===execution
  &&Array.isArray(snapshot.entries)&&snapshot.entries.length<=20000,'restore-binding');
 const seen=new Map();let bytes=0;
 for(const entry of snapshot.entries){
  need(typeof entry.path==='string'&&entry.path.split('/').every(p=>p&&!['.','..'].includes(p)&&!/[\\\x00-\x1f]/.test(p))
   &&!seen.has(entry.path)&&['directory','file','symlink'].includes(entry.kind)&&Number.isInteger(entry.mode)&&(entry.mode&~0o777)===0,'restore-path');
  let data=null;if(entry.kind!=='directory'){
   data=Buffer.from(entry.data,'base64');need(data.toString('base64')===entry.data&&sha(data)===entry.sha256
    &&data.length<=16000000&&(bytes+=data.length)<=48000000,'restore-bytes');
   if(entry.kind==='symlink'){
    const target=data.toString('utf8'),resolved=resolve(dirname(join(directory,entry.path)),target);
    need(target&&!target.startsWith('/')&&resolved.startsWith(directory+sep)&&!entry.path.startsWith('.git/'),'restore-symlink');
   }
  }
  seen.set(entry.path,{...entry,data});
 }
 for(const entry of seen.values()){
  const parts=entry.path.split('/');parts.pop();let parent='';
  for(const part of parts){parent=parent?parent+'/'+part:part;need(seen.get(parent)?.kind==='directory','restore-parent');}
 }
 need(bytes===snapshot.bytes,'restore-size');
 for(const entry of [...seen.values()].sort((a,b)=>a.path.split('/').length-b.path.split('/').length||a.path.localeCompare(b.path))){
  const path=join(directory,entry.path);
  if(entry.kind==='directory')await fs.mkdir(path,{mode:0o700});
  else if(entry.kind==='symlink')await fs.symlink(entry.data.toString('utf8'),path);
  else{await fs.writeFile(path,entry.data,{flag:'wx',mode:entry.mode&0o700});await fs.chmod(path,entry.mode&0o700);}
 }
 return{status:'UNACCEPTED',execution,bytes};
}

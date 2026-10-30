import fs from'node:fs/promises';import{join}from'node:path';import{userInfo}from'node:os';
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

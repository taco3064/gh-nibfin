import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {spawn} from 'node:child_process';
import {privatePath,readPrivate,writePrivate,ensurePrivateDirectory,sha} from './local-state.mjs';
import {maintenanceLock} from './upgrade.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-work-custody:'+code);};
const hash=v=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
const names=['application/_work','work','roles','listener.log','process.json'];
const maxFiles=100000,maxBytes=1024*1024*1024,maxFileBytes=64*1024*1024;

async function docker(args,{signal}={}){
 const uid=userInfo().uid,socket='/run/user/'+uid+'/docker.sock',stat=await fs.lstat(socket);
 need(stat.isSocket()&&stat.uid===uid,'docker-socket');
 return new Promise((resolve,reject)=>{
  const child=spawn('/usr/bin/docker',['--host','unix://'+socket,...args],{signal,
   env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:userInfo().homedir,XDG_RUNTIME_DIR:'/run/user/'+uid,LANG:'C'},stdio:['ignore','pipe','pipe']});
  let output='',overflow=false;const timer=setTimeout(()=>child.kill('SIGKILL'),15000);
  child.stdout.on('data',b=>{output+=b;if(Buffer.byteLength(output)>8*1024*1024){overflow=true;child.kill('SIGKILL');}});
  child.stderr.resume();child.once('error',()=>{clearTimeout(timer);reject(Error('native-work-custody:docker-unavailable'));});
  child.once('close',code=>{clearTimeout(timer);if(code!==0||overflow)reject(Error('native-work-custody:docker-unavailable'));else resolve(output);});
 });
}

/** Observe the actual terminated service scope and rootless model containers.
 * An unknown read is never converted into termination or successful cleanup.
 * No Docker credential, model login or raw process output is published.
 */
export async function observeNativeWorkResources({root,lease,request,signal},{containers=docker,identity}={}){
 need(hash(lease),'lease');const path=join(root,'jobs',lease);let process;
 try{process=await readPrivate(join(path,'process.json'));}catch(e){
  if(e.code!=='ENOENT')throw e;
  const snapshot=await readPrivate(join(root,'original-work',lease,'snapshot.json'),{limit:32*1024*1024});
  const {identity:retained,...body}=snapshot;
  need(typeof identity==='function'&&identity(body)===retained&&snapshot.lease===lease
   &&identity(snapshot.processRecord)===snapshot.process,'retained-process');process=snapshot.processRecord;
 }
 need(process.lease===lease&&Number.isSafeInteger(process.entryPid)&&process.entryPid>0
  &&/^[0-9]+$/.test(process.entryStart??'')&&['launch-intent','running','terminated'].includes(process.phase)
  &&typeof process.controlGroup==='string'
  &&process.controlGroup.startsWith('/user.slice/user-'+userInfo().uid+'.slice/')
  &&process.controlGroup.split('/').slice(1).every(p=>/^[A-Za-z0-9_.@:-]+$/.test(p)),'process-scope');
 let scopeEmpty=false;
 try{scopeEmpty=/(?:^|\n)populated 0(?:\n|$)/.test(await fs.readFile('/sys/fs/cgroup'+process.controlGroup+'/cgroup.events','utf8'));}
 catch(e){if(e.code==='ENOENT')scopeEmpty=true;else throw e;}
 let entryEnded=false;
 try{const stat=await fs.readFile('/proc/'+process.entryPid+'/stat','utf8');entryEnded=stat.slice(stat.lastIndexOf(')')+2).split(' ')[19]!==process.entryStart;}
 catch(e){if(e.code==='ENOENT')entryEnded=true;else throw e;}
 const ids=(await containers(['ps','-aq'],{signal})).trim().split(/\s+/).filter(Boolean);
 need(ids.length<=1000&&ids.every(id=>/^[a-f0-9]{12,64}$/.test(id)),'container-inventory');
 const list=ids.length?JSON.parse(await containers(['inspect',...ids],{signal})):[];
 need(Array.isArray(list)&&list.length===ids.length&&new Set(list.map(c=>c.Id)).size===ids.length
  &&list.every(c=>typeof c.Id==='string'&&/^[a-f0-9]{64}$/.test(c.Id)&&ids.some(id=>c.Id.startsWith(id))
    &&c.State&&c.Config&&Array.isArray(c.Mounts)),'container-observation');
 const related=list.filter(c=>c.Config?.Labels?.['nibfin.lease']===lease||c.Mounts.some(m=>
  typeof m.Source==='string'&&(m.Source===path||m.Source.startsWith(path+'/')||m.Source==='/'||path.startsWith(m.Source+'/'))));
 return {lease,processesTerminated:process.phase==='terminated'&&scopeEmpty&&entryEnded
  &&related.every(c=>c.State.Running===false&&c.State.Paused===false),resourcesRemoved:related.length===0};
}

/** Fixed private storage for the Core Runner-work custody owner. Files are
 * content-addressed regular blobs; project symlinks are retained as data and
 * never followed. Exact original Git objects/conversations remain recoverable.
 */
export async function nativeWorkCustody(core,{root,configuration,store,resources=observeNativeWorkResources}={}){
 need(typeof core.createRunnerWorkCustody==='function'&&typeof store?.jobs?.read==='function','composition');
 const directory=join(root,'original-work');await ensurePrivateDirectory(directory);
 const validate=value=>{const {identity,...body}=value;need(hash(identity)&&core.contentIdentity(body)===identity,'private-integrity');return value;};
 const sealed=body=>({...body,identity:core.contentIdentity(body)});
 const location=lease=>{need(hash(lease),'lease');return join(directory,lease);};
 async function sourceInventory(lease,{capture=false,signal}={}){
  const base=join(root,'jobs',lease),target=location(lease),entries=[],roots=[];let bytes=0;
  await privatePath(base,{directory:true});
  async function walk(path,relative){
   need(!signal?.aborted&&entries.length<maxFiles&&!/[\x00-\x1f\x7f]/.test(relative),'inventory-bound');
   const before=await fs.lstat(path);need(before.uid===userInfo().uid,'source-owner');
   if(before.isDirectory()){
    entries.push({path:relative,kind:'directory',mode:before.mode&0o777});
    for(const name of (await fs.readdir(path)).sort())await walk(join(path,name),relative+'/'+name);
   }else{
    need(before.isFile()||before.isSymbolicLink(),'source-type');
    let payload;
    if(before.isSymbolicLink())payload=Buffer.from(await fs.readlink(path));
    else{
     need(before.nlink===1&&before.size<=maxFileBytes,'source-file');
     const file=await fs.open(path,fs.constants.O_RDONLY|fs.constants.O_NOFOLLOW);
     try{const opened=await file.stat();need(opened.ino===before.ino&&opened.dev===before.dev&&opened.size===before.size,'source-changed');payload=await file.readFile();}
     finally{await file.close();}
    }
    bytes+=payload.length;need(bytes<=maxBytes,'snapshot-size');const digest=sha(payload);
    entries.push({path:relative,kind:before.isSymbolicLink()?'symlink':'file',mode:before.mode&0o777,bytes:payload.length,sha256:digest});
    if(capture){
     const blob=join(target,'blobs',digest);let existing;
     try{await privatePath(blob);existing=await fs.readFile(blob);}catch(e){if(e.code!=='ENOENT')throw e;}
     if(existing)need(existing.equals(payload),'blob-conflict');
     else{const file=await fs.open(blob,'wx',0o600);try{await file.writeFile(payload);await file.sync();}finally{await file.close();}}
    }
   }
   const after=await fs.lstat(path);need(before.ino===after.ino&&before.dev===after.dev&&before.size===after.size
    &&before.mtimeNs===after.mtimeNs&&before.mtimeMs===after.mtimeMs,'source-changed');
  }
  for(const name of names){const path=join(base,name);
   for(const part of name.split('/').slice(0,-1)){
    const parent=await fs.lstat(join(base,part));need(parent.isDirectory()&&!parent.isSymbolicLink()&&parent.uid===userInfo().uid,'source-parent');
   }
   let present=true;try{await fs.lstat(path);}catch(e){if(e.code==='ENOENT')present=false;else throw e;}
   roots.push({path:name,present});if(present)await walk(path,name);
  }
  return {roots,entries,bytes};
 }
 async function verify({lease,identity,request}){
  const path=location(lease);await privatePath(path,{directory:true});
  const snapshot=validate(await readPrivate(join(path,'snapshot.json'),{limit:32*1024*1024}));
  need(snapshot.version===1&&snapshot.lease===lease&&snapshot.identity===identity
   &&core.canonicalJSON(snapshot.request)===core.canonicalJSON(request)&&Array.isArray(snapshot.entries)
   &&snapshot.entries.length<=maxFiles&&snapshot.bytes<=maxBytes,'snapshot-binding');
  const blobs=new Set();let bytes=0;
  for(const entry of snapshot.entries){
   need(typeof entry.path==='string'&&entry.path.split('/').every(p=>p&&p!=='.'&&p!=='..')
    &&['directory','file','symlink'].includes(entry.kind),'snapshot-path');
   if(entry.kind==='directory')continue;
   need(hash(entry.sha256)&&Number.isSafeInteger(entry.bytes)&&entry.bytes>=0&&entry.bytes<=maxFileBytes,'snapshot-file');
   const blob=join(path,'blobs',entry.sha256);await privatePath(blob);const payload=await fs.readFile(blob);
   need(payload.length===entry.bytes&&sha(payload)===entry.sha256,'retained-bytes');blobs.add(entry.sha256);bytes+=entry.bytes;
  }
  need(bytes===snapshot.bytes&&core.canonicalJSON((await fs.readdir(join(path,'blobs'))).sort())===core.canonicalJSON([...blobs].sort()),'snapshot-inventory');
  return {lease,identity,verified:true};
 }
 const records={
  read:async lease=>{const value=await readPrivate(join(directory,'receipt-'+lease+'.json'),{missing:true});return value===null?null:validate(value);},
  write:async({lease,expected,value})=>maintenanceLock(directory,async()=>{
   need(hash(lease),'lease');const path=join(directory,'receipt-'+lease+'.json'),before=await readPrivate(path,{missing:true});
   if((before?.identity??null)!==expected)return {status:'conflict'};validate(value);await writePrivate(path,value,{exclusive:before===null});return {status:'retained'};
  })
 };
 return core.createRunnerWorkCustody({configuration,expected:store.jobs.read,records,
  resources:{observe:q=>resources({root,...q},{identity:core.contentIdentity})},
  snapshots:{verify,async capture({lease,request,process,signal}){
   const path=location(lease);await fs.mkdir(path,{mode:0o700});await fs.mkdir(join(path,'blobs'),{mode:0o700});
   const before=await sourceInventory(lease,{capture:true,signal}),after=await sourceInventory(lease,{signal});
   need(core.canonicalJSON(before)===core.canonicalJSON(after),'source-changed');
   const processRecord=await readPrivate(join(root,'jobs',lease,'process.json'));
   need(core.contentIdentity(processRecord)===process,'process-binding');
   const snapshot=sealed({version:1,lease,request:structuredClone(request),process,processRecord,...before});
   await writePrivate(join(path,'snapshot.json'),snapshot,{exclusive:true});
   return {lease,identity:snapshot.identity};
  }}
 });
}

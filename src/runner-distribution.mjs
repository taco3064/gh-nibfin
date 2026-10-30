import fs from 'node:fs/promises';
import {createReadStream} from 'node:fs';
import {createHash} from 'node:crypto';
import {join,resolve,dirname,isAbsolute,sep} from 'node:path';
import {tmpdir,userInfo} from 'node:os';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {safePath} from './register.mjs';
export const runnerDistribution = Object.freeze({version:'2.338.2',commit:'d1eb12d4b115c301201de27d667284e1a80f2ab5',
  sha256:'baebf204307bac6193480852732f0aba2ae97b6c5597c244f4d5558390147c40',
  archive:'actions-runner-linux-x64-2.338.2.tar.gz'});
const hash = async path => {
  const h=createHash('sha256');for await(const b of createReadStream(path))h.update(b);return h.digest('hex');
};
// The authenticated official archive includes Node package launchers as links.
// Only relative links resolving to files inside that same distribution qualify;
// ordinary private-path checks continue to reject links everywhere else.
async function distributionLink(root,relative){
  const path=join(root,relative),target=await fs.readlink(path),inside=p=>p.startsWith(resolve(root)+sep);
  if(isAbsolute(target)||!inside(resolve(dirname(path),target))||!inside(await fs.realpath(path))
    ||!(await fs.stat(path)).isFile())throw Error('Official Runner link escapes its distribution.');
  return target;
}
// Verify the immutable official archive BEFORE extraction; compare every shipped
// file to the application actually invoked. No download, installer or lifecycle.
export async function verifyRunnerDistribution(directory,{signal,unregistered=false}={}) {
  let scratch;
  try {
    if(process.platform!=='linux' || process.arch!=='x64')throw Error();
    const archive=join(directory,runnerDistribution.archive);await safePath(archive,{directory:false});
    if((await fs.stat(archive)).size>500000000)throw Error();
    scratch=await fs.mkdtemp(join(tmpdir(),'nibfin-official-runner-'));
    const snapshot=join(scratch,'distribution.tgz');await fs.copyFile(archive,snapshot);
    if(await hash(snapshot)!==runnerDistribution.sha256)throw Error();
    const extracted=join(scratch,'extracted');await fs.mkdir(extracted);
    await promisify(execFile)('tar',['--extract','--gzip','--file',snapshot,'--directory',extracted,'--no-same-owner'],
      {signal,timeout:30000,maxBuffer:4096,env:{PATH:process.env.PATH,LANG:'C'}});
    await verifyRunnerTree(extracted,directory,{signal,unregistered});
    return runnerDistribution;
  } catch {throw Error(`Registration requires the pinned official Linux x64 Runner v${runnerDistribution.version} archive, matching extracted application bytes, and local tar. Preserve incompatible installations for reconciled upgrade; no registration was requested.`);}
  finally {if(scratch)await fs.rm(scratch,{recursive:true,force:true});}
}

// The caller authenticates and extracts the fixed official archive first.
// Walk ownership/types once, then compare regular file bytes in bounded native
// checksum batches. Rewalking every ancestor for every file made init take minutes.
export async function verifyRunnerTree(expectedRoot,directory,{signal,unregistered=false}={}){
  await safePath(directory);const expectedPaths=new Set([runnerDistribution.archive]),files=[];
  let count=0;
  const walk=async(relative='',depth=0)=>{
    if(depth>64)throw Error('Runner tree depth');
    for(const entry of await fs.readdir(join(expectedRoot,relative),{withFileTypes:true})){
      const name=join(relative,entry.name),actual=join(directory,name),stat=await fs.lstat(actual);
      if(++count>100000||name.length>4096||stat.uid!==userInfo().uid)throw Error('Runner tree ownership');
      expectedPaths.add(name);
      if(entry.isDirectory()){
        if(!stat.isDirectory()||(stat.mode&0o077)!==0)throw Error('Runner directory');
        await walk(name,depth+1);
      }else if(entry.isFile()){
        if(!stat.isFile()||stat.nlink!==1||(stat.mode&0o077)!==0)throw Error('Runner file');files.push(name);
      }else if(entry.isSymbolicLink()){
        if(!stat.isSymbolicLink()||stat.nlink!==1||await distributionLink(directory,name)!==await distributionLink(expectedRoot,name))throw Error('Runner link');
      }else throw Error('Runner file type');
    }
  };await walk();
  for(let i=0;i<files.length;i+=256){
    const paths=files.slice(i,i+256),options={signal,timeout:30000,maxBuffer:16777216,encoding:null,env:{PATH:'/usr/local/bin:/usr/bin:/bin',LANG:'C'}};
    const [expected,actual]=await Promise.all([expectedRoot,directory].map(cwd=>promisify(execFile)('/usr/bin/sha256sum',['--zero','--',...paths],{...options,cwd})));
    if(!expected.stdout.equals(actual.stdout))throw Error('Runner bytes');
  }
  if(unregistered){const exact=async(relative='')=>{
    for(const e of await fs.readdir(join(directory,relative),{withFileTypes:true})){
      const name=join(relative,e.name);if(!expectedPaths.has(name))throw Error('Unrecognized Runner state preserved.');
      if(e.isDirectory())await exact(name);
    }
  };await exact();}
}

// First-party pinned acquisition, called only by registration, never by --check.
// The archive is authenticated before any executable is extracted or invoked.
export async function prepareRunnerDistribution(directory,{signal,archive}={}){
  try{await fs.lstat(join(directory,'config.sh'));return await verifyRunnerDistribution(directory,{signal});}
  catch(e){if(e.code!=='ENOENT')throw e;}
  const entries=await fs.readdir(directory);
  if(entries.some(n=>n!=='unregister.json'))throw Error('Unrecognized Runner installation residue; preserve and reconcile before registration.');
  const scratch=await fs.mkdtemp(join(tmpdir(),'nibfin-runner-download-'));
  try{
    const snapshot=join(scratch,runnerDistribution.archive);
    if(archive){
      await safePath(archive,{directory:false});
      if((await fs.stat(archive)).size>500000000)throw Error('Runner archive oversized.');
      await fs.copyFile(archive,snapshot,fs.constants.COPYFILE_EXCL);
    }else{
    const {get}=await import('node:https');
    const handle=await fs.open(snapshot,'wx',0o600);let size=0;
    const fetch=async(url,hops=0)=>{
      if(hops>4)throw Error('Runner redirect limit.');
      const u=new URL(url);
      if(u.protocol!=='https:'||u.username||u.password||!['github.com','release-assets.githubusercontent.com','objects.githubusercontent.com'].includes(u.hostname))throw Error('Unsupported official Runner download origin.');
      await new Promise((resolve,reject)=>{
        const req=get(u,{signal,timeout:30000,headers:{'User-Agent':'gh-nibfin','Accept':'application/octet-stream'}},res=>{
          if([301,302,303,307,308].includes(res.statusCode)){res.resume();fetch(new URL(res.headers.location,u),hops+1).then(resolve,reject);return;}
          if(res.statusCode!==200){res.resume();reject(Error('Official Runner download denied/unavailable.'));return;}
          (async()=>{try{for await(const chunk of res){size+=chunk.length;if(size>500000000)throw Error('Runner archive oversized.');await handle.write(chunk);}resolve();}catch(e){req.destroy();reject(e);}})();
        });req.once('timeout',()=>req.destroy(Error('Runner download timeout.')));req.once('error',reject);
      });
    };
    try{await fetch(`https://github.com/actions/runner/releases/download/v${runnerDistribution.version}/${runnerDistribution.archive}`);await handle.sync();}finally{await handle.close();}
    }
    if(await hash(snapshot)!==runnerDistribution.sha256)throw Error('Official Runner archive integrity mismatch.');
    const extracted=join(scratch,'extracted');await fs.mkdir(extracted,{mode:0o700});
    await promisify(execFile)('tar',['--extract','--gzip','--file',snapshot,'--directory',extracted,'--no-same-owner'],{signal,timeout:30000,maxBuffer:4096,env:{PATH:process.env.PATH,LANG:'C'}});
    const copy=async(relative='')=>{
      for(const e of await fs.readdir(join(extracted,relative),{withFileTypes:true})){
        const name=join(relative,e.name),source=join(extracted,name),target=join(directory,name);
        if(e.isDirectory()){await fs.mkdir(target,{mode:0o700});await copy(name);}
        else if(e.isFile()){await fs.copyFile(source,target,fs.constants.COPYFILE_EXCL);const mode=(await fs.stat(source)).mode;await fs.chmod(target,mode&0o111?0o700:0o600);}
        else if(e.isSymbolicLink())await fs.symlink(await distributionLink(extracted,name),target);
        else throw Error('Unsupported official Runner file type: '+name);
      }
    };
    await copy();await fs.copyFile(snapshot,join(directory,runnerDistribution.archive),fs.constants.COPYFILE_EXCL);
    await fs.chmod(join(directory,runnerDistribution.archive),0o600);return verifyRunnerDistribution(directory,{signal});
  }finally{await fs.rm(scratch,{recursive:true,force:true});}
}

import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {spawn} from 'node:child_process';
import {privatePath} from './local-state.mjs';
const need=(ok,code)=>{if(!ok)throw Error('upgrade:'+code);};
/** Kernel-owned maintenance lease. If the coordinator dies, pipe EOF terminates
 * the lock holder and flock releases the lease. A lock file's mere existence is
 * never interpreted as an active process or deleted to force recovery. */
export async function maintenanceLock(directory,work,{name='maintenance.lock',acceptReleasedAfterWork}={}){
  need(acceptReleasedAfterWork===undefined||typeof acceptReleasedAfterWork==='function','lock-release-policy');
  need(typeof name==='string'&&/^\.?[a-z][a-z0-9-]*\.lock$/.test(name),'lock-name');
  await privatePath(directory,{directory:true});const path=join(directory,name);
  try{const h=await fs.open(path,'wx',0o600);await h.close();}catch(e){if(e.code!=='EEXIST')throw e;}
  await privatePath(path);
  const keeper="process.stdin.resume();process.stdin.once('end',()=>process.exit(0));process.stdout.write('locked\\n');";
  const child=spawn('/usr/bin/flock',['--exclusive','--nonblock',path,process.execPath,'-e',keeper],
    {env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:userInfo().homedir,LANG:'C'},stdio:['pipe','pipe','ignore']});
  child.stdin.on('error',()=>{});
  let ended=false;
  const closed=new Promise(resolve=>{child.once('error',()=>{ended=true;resolve();});child.once('close',()=>{ended=true;resolve();});});
  let acquired=false;
  try{
    await new Promise((resolve,reject)=>{
      let text='';const timer=setTimeout(()=>reject(Error('upgrade:maintenance-lock-timeout')),5000);
      child.stdout.on('data',bytes=>{text+=bytes;if(text==='locked\n'){acquired=true;clearTimeout(timer);resolve();}
        else if(text.length>32){clearTimeout(timer);reject(Error('upgrade:maintenance-lock-protocol'));}});
      closed.then(()=>{clearTimeout(timer);if(!acquired)reject(Error('upgrade:maintenance-lock-unavailable'));});
    });
    need(!ended,'maintenance-lock-lost');const result=await work(()=>need(!ended,'maintenance-lock-lost'));
    need(!ended||acceptReleasedAfterWork?.()===true,'maintenance-lock-lost');return result;
  }finally{
    child.stdin.end();if(!acquired&&!ended)child.kill('SIGTERM');await closed;
  }
}

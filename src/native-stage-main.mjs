// Fixed installed Actions entry. No repository checkout, command or capability
// module is selected by inputs; the private host reauthenticates the actual Job.
import fs from 'node:fs/promises';import {join} from 'node:path';import {userInfo} from 'node:os';import {connect} from 'node:net';
const root=join(userInfo().homedir,'.local/share/nibfin/control'),path=join(root,'control.sock');
const request={version:1,repository:process.env.NIBFIN_REPOSITORY,request:process.env.NIBFIN_REQUEST,
 runId:process.env.NIBFIN_RUN_ID,attempt:process.env.NIBFIN_RUN_ATTEMPT};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function call(operation){
 const stat=await fs.lstat(path);if(!stat.isSocket()||stat.uid!==userInfo().uid||(stat.mode&0o777)!==0o600)throw Error();
 return new Promise((resolve,reject)=>{const socket=connect(path);let bytes='';
  const fail=()=>{socket.destroy();reject(Error('Native host response unavailable'));};socket.on('error',fail);socket.setTimeout(60000,fail);
  socket.once('connect',()=>socket.end(JSON.stringify({...request,operation})+'\n'));
  socket.on('data',value=>{bytes+=value;if(Buffer.byteLength(bytes)>131072)fail();});
  socket.once('end',()=>{socket.destroy();try{resolve(JSON.parse(bytes));}catch{fail();}});
 });
}
let seen=0;
try{
 let value=await call('stage');const deadline=Date.now()+3300000;
 for(;;){
  if(value.request!==request.request||!Array.isArray(value.progress)||!Number.isSafeInteger(value.offset)||value.offset<0)throw Error();
  for(const line of value.progress.slice(Math.max(0,seen-value.offset)))if(typeof line==='string'&&line.startsWith('Nibfin execution '))process.stdout.write(line);seen=value.offset+value.progress.length;
  if(value.status!=='running'){
   const normal=value.status==='completed'&&value.conclusion==='success',pending=value.status==='waiting'&&value.conclusion==='neutral';
   if(process.env.GITHUB_OUTPUT)await fs.appendFile(process.env.GITHUB_OUTPUT,'conclusion='+(normal?'success':pending?'neutral':'failure')+'\n');
   process.exitCode=normal||pending?0:1;break;
  }
  if(Date.now()>=deadline)throw Error();await sleep(2000);value=await call('stage-status');
 }
}catch{process.stderr.write('Nibfin stage is unresolved; retained original state requires reconciliation.\n');process.exitCode=1;}

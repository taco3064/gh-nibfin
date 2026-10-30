import fs from'node:fs/promises';import{join,dirname}from'node:path';import{spawn}from'node:child_process';
import{privatePath,readPrivate,writePrivate,sha}from'./local-state.mjs';
import{maintenanceLock}from'./upgrade.mjs';
const need=(ok,code)=>{if(!ok)throw Error('member-login:'+code);};
export function interactiveCodexLogin(environment){
 return new Promise(resolve=>{
  let child,error=false;
  try{child=spawn('codex',['login'],{env:environment,stdio:'inherit',shell:false,detached:true});}
  catch{resolve({completed:false,terminated:true});return;}
  child.once('error',()=>{error=true;});
  child.once('close',code=>{
   let terminated=!Number.isSafeInteger(child.pid);
   if(!terminated)try{process.kill(-child.pid,0);}catch(e){terminated=e.code==='ESRCH';}
   resolve({completed:!error&&code===0,terminated});
  });
 });
}
/** Login runs in its own private disposable home. Publish only its auth file
 * after the owned client group terminates; CLI helper links never enter the
 * isolated member login directory. Current login/session files are preserved.
 */
export async function loginCodexMember(environment,{run=interactiveCodexLogin}={}){
 const login=environment.CODEX_HOME;await privatePath(login,{directory:true});
 const target=join(login,'auth.json'),pending=join(login,'login-pending.json');
 try{await fs.lstat(target);throw Error('member-login:login-already-present');}catch(e){if(e.code!=='ENOENT')throw e;}
 need(await readPrivate(pending,{missing:true})===null,'original-login-reconciliation-required');
 const temporary=await fs.mkdtemp(join(dirname(login),'.nibfin-codex-login-'));
 const intent={version:1,kind:'codex-login',directory:temporary};let retained=false,published=false;
 try{
  await writePrivate(pending,intent,{exclusive:true});retained=true;
  const result=await run({...environment,CODEX_HOME:temporary});
  need(result?.terminated===true,'original-login-process-unresolved');
  const source=join(temporary,'auth.json');
  if(result.completed!==true){
   let sourceExists=true;try{await fs.lstat(source);}catch(e){if(e.code==='ENOENT')sourceExists=false;else throw e;}
   if(sourceExists)throw Error('member-login:original-login-reconciliation-required');
   await fs.rm(temporary,{recursive:true});await fs.unlink(pending);retained=false;
   throw Error('member-login:login-not-completed');
  }
  await privatePath(source);const stat=await fs.stat(source);need(stat.size>0&&stat.size<=65536,'invalid-login-file');
  let value;try{value=JSON.parse(await fs.readFile(source,'utf8'));}catch{throw Error('member-login:invalid-login-file');}
  need(value?.auth_mode==='chatgpt'&&typeof value.tokens?.access_token==='string'&&value.tokens.access_token&&!value.OPENAI_API_KEY,'invalid-login-file');
  need(JSON.stringify(await readPrivate(pending))===JSON.stringify(intent),'login-intent-changed');
  // An exclusive link cannot overwrite a current login. Remove the original
  // immediately so the published credential has one owner and one hard link.
  await fs.link(source,target);published=true;await fs.unlink(source);
  await fs.rm(temporary,{recursive:true});await fs.unlink(pending);retained=false;
  return{status:'completed'};
 }finally{
  if(!retained&&!published)await fs.rm(temporary,{recursive:true,force:true});
 }
}

/** Credential metadata is a lifetime check, never account authentication or a
 * substitute for a successful provider exchange. Do not expose token values. */
export function codexLoginExpiry(value){
 try{const payload=JSON.parse(Buffer.from(value.tokens.access_token.split('.')[1],'base64url'));
  return Number.isSafeInteger(payload.exp)?payload.exp*1000:null;}catch{return null;}
}
function managedLogin(value){
 need(value?.auth_mode==='chatgpt'&&!value.OPENAI_API_KEY&&typeof value.tokens?.account_id==='string'&&value.tokens.account_id
  &&['access_token','refresh_token','id_token'].every(k=>typeof value.tokens[k]==='string'&&value.tokens[k]),'invalid-login-file');
 return value;
}

// The official app-server owns OAuth. No refresh token is sent by this module,
// passed in argv, echoed in diagnostics or interpreted as a model response.
export function refreshCodexLogin(environment){
 return new Promise(resolve=>{
  let child,buffer='',completed=false,diagnostic='refresh-unavailable',timer,hard,stopping=false;
  const stop=()=>{if(stopping)return;stopping=true;
   try{process.kill(-child.pid,'SIGTERM');}catch{}
   hard=setTimeout(()=>{try{process.kill(-child.pid,'SIGKILL');}catch{}},250);};
  try{child=spawn('codex',['--no-daemon','-c','cli_auth_credentials_store="file"','app-server','--listen','stdio://'],
   {env:environment,shell:false,detached:true,stdio:['pipe','pipe','ignore']});}
  catch{return resolve({completed:false,terminated:true,diagnostic});}
  child.stdin.on('error',()=>{});child.once('error',()=>{diagnostic='refresh-unavailable';});
  timer=setTimeout(()=>{diagnostic='refresh-timeout';stop();},60000);
  child.stdout.on('data',bytes=>{
   buffer+=bytes;if(buffer.length>1048576){diagnostic='refresh-protocol';stop();return;}
   while(buffer.includes('\n')){const at=buffer.indexOf('\n'),line=buffer.slice(0,at);buffer=buffer.slice(at+1);let v;
    try{v=JSON.parse(line);}catch{diagnostic='refresh-protocol';stop();return;}
    if(v.id===1&&v.result){child.stdin.write(JSON.stringify({method:'initialized'})+'\n');
     child.stdin.write(JSON.stringify({id:2,method:'account/read',params:{refreshToken:true}})+'\n');}
    if(v.id===1&&v.error){diagnostic='refresh-protocol';stop();}
    if(v.id===2){completed=v.result?.account?.type==='chatgpt';diagnostic=completed?'refreshed':'reauthentication-required';stop();}
   }
  });
  child.once('close',async()=>{
   clearTimeout(timer);let terminated=!Number.isSafeInteger(child.pid);
   for(let n=0;n<20&&!terminated;n++){
    try{process.kill(-child.pid,0);}catch(e){terminated=e.code==='ESRCH';}
    if(!terminated){if(n===5)try{process.kill(-child.pid,'SIGKILL');}catch{}await new Promise(r=>setTimeout(r,50));}
   }
   clearTimeout(hard);
   resolve({completed,terminated,diagnostic});
  });
  child.stdin.write(JSON.stringify({id:1,method:'initialize',params:{clientInfo:{name:'nibfin-auth-owner',version:'0.1.0'}}})+'\n');
 });
}

/** One kernel lease per member, shared by refresh and reauthentication. A role
 * uses a read-only credential only after this owner has published the refresh.
 * Unknown rotation keeps its original intent/candidate and is never replayed. */
export async function updateCodexMemberLogin(environment,{mode='refresh',run,minimumValidityMs=900000,now=Date.now}={}){
 need(['refresh','reauthenticate'].includes(mode)&&Number.isSafeInteger(minimumValidityMs)&&minimumValidityMs>0&&minimumValidityMs<=86400000,'update-options');
 run??=mode==='refresh'?refreshCodexLogin:interactiveCodexLogin;
 const login=environment.CODEX_HOME;await privatePath(login,{directory:true});
 return maintenanceLock(login,async assertLease=>{
  const target=join(login,'auth.json'),pending=join(login,'auth-update-pending.json');await privatePath(target);
  const original=await fs.readFile(target),originalSha256=sha(original);need(original.length>0&&original.length<=65536,'invalid-login-file');
  let current;try{current=managedLogin(JSON.parse(original));}catch{throw Error('member-login:invalid-login-file');}
  let prior=await readPrivate(pending,{missing:true});
  if(prior){
   need(prior.version===1&&prior.kind==='codex-login-update'&&prior.originalSha256===originalSha256,'original-login-reconciliation-required');
   need(prior.phase==='reauthentication-required'&&prior.directory===null,'original-login-reconciliation-required');
   if(mode==='refresh')return{status:'blocked',diagnostic:'reauthentication-required'};
  }
  if(mode==='refresh'&&(codexLoginExpiry(current)??0)>=now()+minimumValidityMs)return{status:'ready',updated:false};
  const temporary=await fs.mkdtemp(join(dirname(login),'.nibfin-codex-auth-'));
  const source=join(temporary,'auth.json'),intent={version:1,kind:'codex-login-update',mode,phase:'prepared',originalSha256,directory:temporary};
  let retained=false,published=false;
  try{
   if(prior)await writePrivate(pending,intent);else await writePrivate(pending,intent,{exclusive:true});retained=true;
   if(mode==='refresh')await fs.writeFile(source,original,{flag:'wx',mode:0o600});
   const result=await run({...environment,CODEX_HOME:temporary});
   need(result?.terminated===true,'original-login-process-unresolved');
   let after=null;try{await privatePath(source);const stat=await fs.stat(source);need(stat.size>0&&stat.size<=65536,'invalid-login-file');after=await fs.readFile(source);}catch(e){if(e.code!=='ENOENT')throw e;}
   need(JSON.stringify(await readPrivate(pending))===JSON.stringify(intent),'login-intent-changed');
   assertLease();need(sha(await fs.readFile(target))===originalSha256,'current-login-changed');
   if(result.completed!==true){
    // Preserve an updated credential when the remote outcome is unknown. Even
    // a failed RPC can have rotated it; a second refresh may invalidate it.
    need(after===null||after.equals(original),'original-login-reconciliation-required');
    need(result.diagnostic==='reauthentication-required','original-login-reconciliation-required');
    await fs.rm(temporary,{recursive:true});
    await writePrivate(pending,{...intent,phase:'reauthentication-required',directory:null});retained=false;
    return{status:'blocked',diagnostic:'reauthentication-required'};
   }
   let next;try{next=managedLogin(JSON.parse(after));}catch{throw Error('member-login:invalid-login-file');}
   need(next.tokens.account_id===current.tokens.account_id,'login-account-changed');
   need((codexLoginExpiry(next)??0)>=now()+minimumValidityMs,'login-lifetime-insufficient');
   const handle=await fs.open(source,'r');try{await handle.sync();}finally{await handle.close();}
   assertLease();need(sha(await fs.readFile(target))===originalSha256,'current-login-changed');
   await fs.rename(source,target);published=true;
   const directory=await fs.open(login,'r');try{await directory.sync();}finally{await directory.close();}
   await fs.rm(temporary,{recursive:true});await fs.unlink(pending);retained=false;
   return{status:'ready',updated:true};
  }finally{if(!retained&&!published)await fs.rm(temporary,{recursive:true,force:true});}
 });
}

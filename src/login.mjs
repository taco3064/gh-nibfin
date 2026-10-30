import fs from'node:fs/promises';import{join,dirname}from'node:path';import{spawn}from'node:child_process';
import{privatePath,readPrivate,writePrivate}from'./local-state.mjs';
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
  const value=JSON.parse(await fs.readFile(source,'utf8'));
  need(value.auth_mode==='chatgpt'&&typeof value.tokens?.access_token==='string'&&value.tokens.access_token&&!value.OPENAI_API_KEY,'invalid-login-file');
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

import{test}from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{tmpdir}from'node:os';
import{updateCodexMemberLogin,codexLoginExpiry}from'../../src/login.mjs';
const now=1700000000000;
const login=(account='SYNTHETIC-MEMBER',exp=now/1000-10)=>JSON.stringify({auth_mode:'chatgpt',OPENAI_API_KEY:null,
 tokens:{account_id:account,access_token:'x.'+Buffer.from(JSON.stringify({exp})).toString('base64url')+'.x',refresh_token:'SYNTHETIC-REFRESH',id_token:'SYNTHETIC-ID'}});
async function fixture(run){const root=await fs.mkdtemp(join(tmpdir(),'nibfin-auth-update-test-')),space=join(root,'login');await fs.mkdir(space,{mode:0o700});
 await fs.writeFile(join(space,'auth.json'),login(),{mode:0o600});await fs.writeFile(join(space,'original-session'),'PRESERVE ORIGINAL SESSION');
 try{await run({root,space,target:join(space,'auth.json'),pending:join(space,'auth-update-pending.json'),environment:{CODEX_HOME:space,PATH:process.env.PATH}});}
 finally{await fs.rm(root,{recursive:true,force:true});}}

test('refresh atomically publishes only same-member fresh auth after termination; preserves sessions and retires helpers',async()=>fixture(async f=>{
 let temporary;const fresh=login('SYNTHETIC-MEMBER',now/1000+3600),external=join(f.root,'owned-tool');await fs.writeFile(external,'PRESERVE TOOL');
 const r=await updateCodexMemberLogin(f.environment,{now:()=>now,run:async e=>{temporary=e.CODEX_HOME;
  assert.notEqual(temporary,f.space);assert.equal(await fs.readFile(join(temporary,'auth.json'),'utf8'),login());
  await fs.writeFile(join(temporary,'auth.json'),fresh,{mode:0o600});await fs.symlink(external,join(temporary,'helper'));
  assert.equal(await fs.readFile(f.target,'utf8'),login());return{completed:true,terminated:true};}});
 assert.deepEqual(r,{status:'ready',updated:true});assert.equal(await fs.readFile(f.target,'utf8'),fresh);
 assert.equal(await fs.readFile(join(f.space,'original-session'),'utf8'),'PRESERVE ORIGINAL SESSION');assert.equal(await fs.readFile(external,'utf8'),'PRESERVE TOOL');
 assert.equal((await fs.stat(f.target)).nlink,1);await assert.rejects(fs.stat(temporary),{code:'ENOENT'});await assert.rejects(fs.stat(f.pending),{code:'ENOENT'});
 assert.deepEqual(await updateCodexMemberLogin(f.environment,{now:()=>now,run:()=>assert.fail('Fresh login must not refresh')}),{status:'ready',updated:false});
}));

test('unknown credential rotation retains candidate and intent; refuses duplicate refresh',async()=>fixture(async f=>{
 let temporary;const fresh=login('SYNTHETIC-MEMBER',now/1000+3600);
 await assert.rejects(updateCodexMemberLogin(f.environment,{now:()=>now,run:async e=>{temporary=e.CODEX_HOME;await fs.writeFile(join(temporary,'auth.json'),fresh);return{completed:false,terminated:true};}}),/reconciliation-required/);
 assert.equal(await fs.readFile(f.target,'utf8'),login());assert.equal(await fs.readFile(join(temporary,'auth.json'),'utf8'),fresh);
 assert.equal(JSON.parse(await fs.readFile(f.pending)).directory,temporary);
 await assert.rejects(updateCodexMemberLogin(f.environment,{now:()=>now,run:()=>assert.fail('No replay')}),/reconciliation-required/);
}));

test('known reauthentication requirement blocks automatic retries; fresh explicit login replaces only the same member',async()=>fixture(async f=>{
 const r=await updateCodexMemberLogin(f.environment,{now:()=>now,run:async()=>({completed:false,terminated:true,diagnostic:'reauthentication-required'})});
 assert.equal(r.diagnostic,'reauthentication-required');assert.equal(JSON.parse(await fs.readFile(f.pending)).directory,null);
 assert.equal((await updateCodexMemberLogin(f.environment,{now:()=>now,run:()=>assert.fail('Never retry spent token')})).status,'blocked');
 const next=login('SYNTHETIC-MEMBER',now/1000+3600);
 assert.equal((await updateCodexMemberLogin(f.environment,{mode:'reauthenticate',now:()=>now,run:async e=>{
  await assert.rejects(fs.stat(join(e.CODEX_HOME,'auth.json')),{code:'ENOENT'});await fs.writeFile(join(e.CODEX_HOME,'auth.json'),next,{mode:0o600});return{completed:true,terminated:true};}})).status,'ready');
 assert.equal(await fs.readFile(f.target,'utf8'),next);await assert.rejects(fs.stat(f.pending),{code:'ENOENT'});
}));

test('concurrent login replacement and another account never overwrite current credentials',async()=>{
 for(const change of ['current','account'])await fixture(async f=>{
  const actual=login('SYNTHETIC-MEMBER',now/1000+7200);let temporary;
  await assert.rejects(updateCodexMemberLogin(f.environment,{now:()=>now,run:async e=>{temporary=e.CODEX_HOME;
   await fs.writeFile(join(temporary,'auth.json'),login(change==='account'?'OTHER-MEMBER':'SYNTHETIC-MEMBER',now/1000+3600));
   if(change==='current')await fs.writeFile(f.target,actual);return{completed:true,terminated:true};}}),change==='current'?/current-login-changed/:/account-changed/);
  assert.equal(await fs.readFile(f.target,'utf8'),change==='current'?actual:login());assert((await fs.stat(temporary)).isDirectory());
 });
});

test('unresolved process or insufficient lifetime preserves original login and prevents unsafe publication',async()=>{
 for(const terminated of [false,true])await fixture(async f=>{
  await assert.rejects(updateCodexMemberLogin(f.environment,{now:()=>now,run:async e=>{await fs.writeFile(join(e.CODEX_HOME,'auth.json'),login());return{completed:true,terminated};}}),terminated?/lifetime-insufficient/:/process-unresolved/);
  assert.equal(await fs.readFile(f.target,'utf8'),login());assert((await fs.stat(f.pending)).isFile());
 });
 assert.equal(codexLoginExpiry({tokens:{access_token:'malformed'}}),null);
});

import{test}from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{tmpdir}from'node:os';
import{loginCodexMember}from'../../src/login.mjs';
test('completed login publishes only exact auth bytes after client termination and retires its helper home',async()=>{
 const root=await fs.mkdtemp(join(tmpdir(),'nibfin-login-test-')),login=join(root,'login');await fs.mkdir(login,{mode:0o700});
 try{
  await fs.writeFile(join(login,'original-session'),'KEEP ORIGINAL');const actualTool=join(root,'tool');await fs.writeFile(actualTool,'KEEP TOOL');
  const bytes=JSON.stringify({auth_mode:'chatgpt',tokens:{access_token:'SYNTHETIC LOGIN FIXTURE'}});let temporary;
  const result=await loginCodexMember({CODEX_HOME:login},{run:async env=>{
   temporary=env.CODEX_HOME;assert.notEqual(temporary,login);await fs.writeFile(join(temporary,'auth.json'),bytes,{mode:0o600});
   await fs.mkdir(join(temporary,'tmp'));await fs.symlink(actualTool,join(temporary,'tmp','helper'));return{completed:true,terminated:true};}});
  assert.equal(result.status,'completed');assert.equal(await fs.readFile(join(login,'auth.json'),'utf8'),bytes);
  assert.equal((await fs.stat(join(login,'auth.json'))).nlink,1);assert.equal(await fs.readFile(join(login,'original-session'),'utf8'),'KEEP ORIGINAL');
  assert.equal(await fs.readFile(actualTool,'utf8'),'KEEP TOOL');await assert.rejects(fs.stat(temporary),{code:'ENOENT'});
  await assert.rejects(fs.stat(join(login,'login-pending.json')),{code:'ENOENT'});
  await assert.rejects(loginCodexMember({CODEX_HOME:login},{run:async()=>assert.fail('never replace current login')}),/login-already-present/);
 }finally{await fs.rm(root,{recursive:true,force:true});}
});
test('unresolved login retains its original private intent and prevents duplicate login',async()=>{
 const root=await fs.mkdtemp(join(tmpdir(),'nibfin-login-test-')),login=join(root,'login');await fs.mkdir(login,{mode:0o700});
 try{
  let temporary;await assert.rejects(loginCodexMember({CODEX_HOME:login},{run:async env=>{temporary=env.CODEX_HOME;return{completed:true,terminated:false};}}),/process-unresolved/);
  assert.equal(JSON.parse(await fs.readFile(join(login,'login-pending.json'))).directory,temporary);assert((await fs.stat(temporary)).isDirectory());
  await assert.rejects(loginCodexMember({CODEX_HOME:login},{run:async()=>assert.fail('must reconcile original login')}),/reconciliation-required/);
  await assert.rejects(fs.stat(join(login,'auth.json')),{code:'ENOENT'});
 }finally{await fs.rm(root,{recursive:true,force:true});}
});

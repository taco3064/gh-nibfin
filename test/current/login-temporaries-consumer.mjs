import{test}from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{tmpdir}from'node:os';import{join}from'node:path';
import{pruneCodexLoginTemporaries}from'../../src/login-temporaries.mjs';
test('retire only inactive generated login helpers while preserving auth, sessions and symlink targets',async()=>{
 const root=await fs.mkdtemp(join(tmpdir(),'nibfin-login-cleanup-'));
 try{
  const login=join(root,'login');await fs.mkdir(login,{mode:0o700});await fs.mkdir(join(login,'sessions'),{mode:0o700});
  await fs.writeFile(join(login,'auth.json'),'CURRENT LOGIN',{mode:0o600});await fs.writeFile(join(login,'sessions','original.jsonl'),'ORIGINAL CONVERSATION');
  const target=join(root,'actual-tool');await fs.writeFile(target,'KEEP TOOL');const helper=join(login,'tmp','arg0-linux','codex-arg0Ab123X');await fs.mkdir(helper,{recursive:true});
  await fs.writeFile(join(helper,'.lock'),'');for(const name of ['apply_patch','applypatch','codex-linux-sandbox'])await fs.symlink(target,join(helper,name));
  await assert.rejects(pruneCodexLoginTemporaries(login,{processes:async()=>[123]}),/active-helper/);assert.equal(await fs.readFile(join(helper,'.lock'),'utf8'),'');
  await fs.writeFile(join(helper,'unknown'),'PRESERVE');await assert.rejects(pruneCodexLoginTemporaries(login,{processes:async()=>[]}),/unknown-helper-data/);
  assert.equal(await fs.readFile(join(helper,'unknown'),'utf8'),'PRESERVE');await fs.unlink(join(helper,'unknown'));
  assert.deepEqual(await pruneCodexLoginTemporaries(login,{processes:async()=>[]}),{status:'removed',removed:1});
  assert.equal(await fs.readFile(join(login,'auth.json'),'utf8'),'CURRENT LOGIN');assert.equal(await fs.readFile(join(login,'sessions','original.jsonl'),'utf8'),'ORIGINAL CONVERSATION');
  assert.equal(await fs.readFile(target,'utf8'),'KEEP TOOL');assert.deepEqual(await pruneCodexLoginTemporaries(login),{status:'absent',removed:0});
 }finally{await fs.rm(root,{recursive:true,force:true});}
});

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {verifyRunnerTree} from '../../src/runner-distribution.mjs';

test('batched Runner tree verification compares every binary byte and rejects extra state or unsafe links',async()=>{
 const root=await fs.mkdtemp(join(tmpdir(),'nibfin-tree-check-'));
 const expected=join(root,'expected'),actual=join(root,'actual');
 try{
  for(const p of [expected,actual]){
   await fs.mkdir(join(p,'lib'),{recursive:true,mode:0o700});
   for(let i=0;i<270;i++)await fs.writeFile(join(p,'lib','file-'+i),Buffer.from([0,i%256,255]),{mode:0o600});
   await fs.symlink('lib/file-0',join(p,'launcher'));
  }
  await verifyRunnerTree(expected,actual,{unregistered:true});
  await fs.writeFile(join(actual,'lib/file-269'),Buffer.from([0,1,2]));
  await assert.rejects(verifyRunnerTree(expected,actual),/Runner bytes/);
  await fs.copyFile(join(expected,'lib/file-269'),join(actual,'lib/file-269'));
  await fs.writeFile(join(actual,'.credentials'),'original',{mode:0o600});
  await assert.rejects(verifyRunnerTree(expected,actual,{unregistered:true}),/Unrecognized Runner state/);
  assert.equal(await fs.readFile(join(actual,'.credentials'),'utf8'),'original');
  await fs.unlink(join(actual,'.credentials'));
  await fs.unlink(join(actual,'launcher'));await fs.symlink('../outside',join(actual,'launcher'));
  await assert.rejects(verifyRunnerTree(expected,actual));
  await fs.unlink(join(actual,'launcher'));await fs.symlink('lib/file-0',join(actual,'launcher'));
  await fs.chmod(join(actual,'lib/file-0'),0o644);
  await assert.rejects(verifyRunnerTree(expected,actual),/Runner file/);
 }finally{await fs.rm(root,{recursive:true,force:true});}
});

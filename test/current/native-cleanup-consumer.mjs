import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {spawn} from 'node:child_process';
import {withCurrentCore} from '../../src/current-core.mjs';
import {ensurePrivateDirectory,readPrivate} from '../../src/local-state.mjs';
import {retireNativeApplication} from '../../src/native-resource-cleanup.mjs';
async function fixture(context){
 const root=join(context.root,'cleanup-host'),lease='a'.repeat(64),outcome='b'.repeat(64),directory=join(root,'jobs',lease);
 await ensurePrivateDirectory(join(root,'pool-state'));await ensurePrivateDirectory(directory);
 await fs.writeFile(join(directory,'temporary'),'disposable application',{mode:0o600});
 return {root,lease,outcome,retiredGroup:'/user.slice/user-'+userInfo().uid+'.slice/fixture-gone.service',directory,
  parked:join(root,'jobs','retired-'+lease),journal:join(root,'pool-state','cleanup-'+lease+'.json')};
}
test('cleanup requires custody and actual unused files, preserves external symlink targets and rejects replacement work',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(context),external=join(context.root,'retained-original');await fs.writeFile(external,'UNACCEPTED ORIGINAL',{mode:0o600});
 await fs.symlink(external,join(f.directory,'original-link'));
 await assert.rejects(retireNativeApplication(core,{...f,confirm:async()=>false}),/custody-or-service-unconfirmed/);
 const file=await fs.open(join(f.directory,'temporary'),'r');
 try{await assert.rejects(retireNativeApplication(core,{...f,confirm:async()=>true}),/process-reference/);}finally{await file.close();}
 assert.equal((await retireNativeApplication(core,{...f,confirm:async()=>true})).status,'completed');
 assert.equal(await fs.readFile(external,'utf8'),'UNACCEPTED ORIGINAL');await assert.rejects(fs.stat(f.directory),{code:'ENOENT'});
 assert.equal((await readPrivate(f.journal)).inventory.entries,undefined);
 assert.equal((await retireNativeApplication(core,{...f,confirm:async()=>true})).status,'completed');
 await fs.mkdir(f.directory,{mode:0o700});await fs.writeFile(join(f.directory,'new-work'),'PRESERVE',{mode:0o600});
 await assert.rejects(retireNativeApplication(core,{...f,confirm:async()=>true}),/completed-path-reused/);
 assert.equal(await fs.readFile(join(f.directory,'new-work'),'utf8'),'PRESERVE');
}));
test('interrupted parking resumes the same inode and never treats missing work as empty or discards changed files',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(context);let calls=0;
 await assert.rejects(retireNativeApplication(core,{...f,confirm:async()=>++calls<3}),/custody-or-service-unconfirmed/);
 assert.equal((await readPrivate(f.journal)).phase,'parked');await assert.rejects(fs.stat(f.directory),{code:'ENOENT'});
 assert.equal(await fs.readFile(join(f.parked,'temporary'),'utf8'),'disposable application');
 await fs.writeFile(join(f.parked,'new-work'),'PRESERVE',{mode:0o600});
 await assert.rejects(retireNativeApplication(core,{...f,confirm:async()=>true}),/tree-changed/);
 await fs.unlink(join(f.parked,'new-work'));
 assert.equal((await retireNativeApplication(core,{...f,confirm:async()=>true})).status,'completed');
}));
test('known partial removal resumes only an unchanged subset of its retained inventory',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(context);await fs.writeFile(join(f.directory,'second'),'disposable second file',{mode:0o600});let calls=0;
 await assert.rejects(retireNativeApplication(core,{...f,confirm:async()=>++calls<4}),/custody-or-service-unconfirmed/);
 assert.equal((await readPrivate(f.journal)).phase,'remove-intent');
 await fs.unlink(join(f.parked,'temporary'));await fs.writeFile(join(f.parked,'unexpected'),'PRESERVE',{mode:0o600});
 await assert.rejects(retireNativeApplication(core,{...f,confirm:async()=>true}),/tree-changed/);
 await fs.unlink(join(f.parked,'unexpected'));
 assert.equal((await retireNativeApplication(core,{...f,confirm:async()=>true})).status,'completed');
}));
test('an unrelated non-dumpable process is not mistaken for a live task in the authenticated retired group',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(context),child=spawn('/usr/bin/python3',['-u','-c',
  "import ctypes,time; ctypes.CDLL(None).prctl(4,0); print('ready',flush=True); time.sleep(30)"],{stdio:['ignore','pipe','ignore']});
 const ended=new Promise(resolve=>{child.once('error',resolve);child.once('close',resolve);});
 try{
  await new Promise((resolve,reject)=>{child.stdout.once('data',resolve);child.once('error',reject);child.once('exit',()=>reject(Error('fixture exited')));});
  await assert.rejects(fs.readFile('/proc/'+child.pid+'/environ'),error=>['EACCES','EPERM'].includes(error.code));
  assert.equal((await retireNativeApplication(core,{...f,confirm:async()=>true})).status,'completed');
 }finally{child.kill('SIGTERM');await ended;}
}));

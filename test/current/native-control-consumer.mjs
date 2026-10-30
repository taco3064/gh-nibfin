import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {connect} from 'node:net';
import {withCurrentCore} from '../../src/current-core.mjs';
import {sha,writePrivate} from '../../src/local-state.mjs';
import {nativeControlService,nativeServiceStatus,nativeServiceName,nativeServiceUnit} from '../../src/native-control-service.mjs';
import {runNativeControl} from '../../src/native-control-process.mjs';

test('native init service uses the real private socket and kernel lease; liveness cannot authorize delivery or replace original work',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'control');await fs.mkdir(root,{mode:0o700});
 await fs.mkdir(join(root,'runtime'),{mode:0o700});await fs.writeFile(join(root,'runtime/fixture.mjs'),'// inert fixture\n',{mode:0o600});
 const installation={version:1,phase:'staged',runtimeInventory:[{path:'fixture.mjs',sha256:sha('// inert fixture\n')}]};
 await writePrivate(join(root,'native-installation.json'),installation);
 const original=Buffer.from('UNACCEPTED ORIGINAL CANDIDATE');await fs.writeFile(join(root,'retained-work'),original,{mode:0o600});
 let active=false,closing,started,readyResolve;const ready=new Promise(r=>readyResolve=r),abort=new AbortController(),calls=[];
 // Only systemd is simulated. Node 22, filesystem, IPC, daemon validation and
 // the non-root Linux kernel flock are real, without a model or credentials.
 const run=args=>{calls.push(args);
  if(args[0]==='start'){
   active=true;
   closing=runNativeControl(root,{signal:abort.signal,ready:async live=>{active=true;readyResolve(live);}});
   started=closing.catch(e=>{readyResolve({error:e});});
  }
  return {ok:true,output:args[0]==='show'?`ActiveState=${active?'active':'inactive'}\nFragmentPath=${join(root,nativeServiceName)}\nMainPID=${process.pid}\nType=notify\n`:''};
 };
 const status=async path=>{const observed=await ready;if(observed.error)throw observed.error;return nativeServiceStatus(path);};
 const service=nativeControlService({root,run,status});
 try{
  assert.equal((await service.check()).status,'blocked');assert.equal(calls.length,0);
  const result=await service.install();assert.equal(result.status,'running');
  assert.equal(result.diagnostics[0].code,'native-delivery-composition-pending');assert.equal(result.delivery,'blocked');
  assert.equal((await fs.stat(join(root,'control.sock'))).mode&0o777,0o600);
  assert.equal((await nativeServiceStatus(root)).pid,process.pid);
  await assert.rejects(runNativeControl(root,{signal:AbortSignal.abort()}),/maintenance-lock-unavailable/);
  assert.deepEqual(await fs.readFile(join(root,'retained-work')),original);
  assert.equal(await fs.readFile(join(root,'native-installation.json'),'utf8'),JSON.stringify(installation,null,2)+'\n');
  const unit=await fs.readFile(join(root,nativeServiceName));await fs.appendFile(join(root,nativeServiceName),'# user change\n');
  assert.equal((await service.check()).status,'blocked');await assert.rejects(service.install(),/preserve-unit/);
  await fs.writeFile(join(root,nativeServiceName),unit);
  const response=await new Promise(resolve=>{const socket=connect(join(root,'control.sock'));let data='';
   socket.on('error',()=>resolve(data));socket.on('data',b=>data+=b);socket.on('close',()=>resolve(data));
   socket.once('connect',()=>socket.end('{"version":1,"operation":"execute","issue":55}\n'));});
  assert.equal(response,'');
  const receipt=await fs.readFile(join(root,'control-service.json'));
  const changed=JSON.parse(receipt);changed.generation='12345678-1234-1234-1234-123456789abc';
  await writePrivate(join(root,'control-service.json'),changed);assert.equal((await service.check()).status,'blocked');
  await fs.writeFile(join(root,'control-service.json'),receipt);
 }finally{abort.abort();await closing;await started;}
 await assert.rejects(fs.stat(join(root,'control.sock')),{code:'ENOENT'});
 assert.equal((await service.check()).status,'blocked');
 // Kernel lease is released on termination; the persistent lock file is normal.
 await runNativeControl(root,{signal:AbortSignal.abort()});
 assert.deepEqual(await fs.readFile(join(root,'retained-work')),original);
}));

test('boot rejects changed runtime bytes before binding IPC or announcing readiness',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'control');await fs.mkdir(root,{mode:0o700});await fs.mkdir(join(root,'runtime'),{mode:0o700});
 await fs.writeFile(join(root,'runtime/fixture.mjs'),'altered',{mode:0o600});
 await writePrivate(join(root,'native-installation.json'),{version:1,phase:'staged',runtimeInventory:[{path:'fixture.mjs',sha256:sha('approved')}]});
 const unit=nativeServiceUnit(root);await fs.writeFile(join(root,nativeServiceName),unit,{mode:0o600});
 await writePrivate(join(root,'control-service.json'),{version:1,account:userInfo().uid,generation:'12345678-1234-1234-1234-123456789abc',unit:sha(unit)});
 let announced=false;await assert.rejects(runNativeControl(root,{signal:AbortSignal.abort(),ready:()=>{announced=true;}}),/native-service:bytes/);
 assert.equal(announced,false);await assert.rejects(fs.stat(join(root,'control.sock')),{code:'ENOENT'});
}));

test('unit renders literal paths without systemd specifier or variable expansion',()=>{
 const unit=nativeServiceUnit('/tmp/with spaces/$home%unit','/usr/local/bin/node');
 assert.ok(unit.includes('"/tmp/with spaces/$$home%%unit/runtime/client/native-control-main.mjs"'));
 assert.throws(()=>nativeServiceUnit('/tmp/bad\nExecStart=wrong'),/native-service:path/);
});


test('upgrade staging links only the owned inactive unit, checks readback and never starts it',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'link-only');await fs.mkdir(root,{mode:0o700});
 const unit=nativeServiceUnit(root);await fs.writeFile(join(root,nativeServiceName),unit,{mode:0o600});
 await writePrivate(join(root,'control-service.json'),{version:1,account:userInfo().uid,generation:'12345678-1234-1234-1234-123456789abc',unit:sha(unit)});
 let loaded=false,active=true;const calls=[];
 const run=args=>{calls.push(args[0]);if(args[0]==='link')loaded=true;
  return {ok:args[0]!=='show'||loaded,status:loaded?0:4,output:args[0]==='show'?
   'LoadState='+ (loaded?'loaded':'not-found')+'\nActiveState='+ (active?'active':'inactive')+'\nMainPID='+ (active?'123':'0')+'\nControlPID=0\nFragmentPath='+ (loaded?join(root,nativeServiceName):'')+'\n':''};};
 const service=nativeControlService({root,run});await assert.rejects(service.stageUnit(),/stage-unit-active/);
 assert.deepEqual(calls,['show']);active=false;assert.equal((await service.stageUnit()).status,'linked');
 assert.equal(calls.includes('start'),false);assert.equal(calls.filter(c=>c==='show').length,3);
}));


test('successive upgrade may stop only its authenticated empty staging daemon, never an occupied or active delivery host',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'idle-staging');await fs.mkdir(root,{mode:0o700});
 const unit=nativeServiceUnit(root),generation='12345678-1234-1234-1234-123456789abc',installation={executionPool:{version:2}};
 await fs.writeFile(join(root,nativeServiceName),unit,{mode:0o600});await writePrivate(join(root,'native-installation.json'),installation);
 await writePrivate(join(root,'control-service.json'),{version:1,account:userInfo().uid,generation,unit:sha(unit)});
 let active=true,occupied=1,delivery='blocked';const calls=[];
 const run=args=>{calls.push(args[0]);if(args[0]==='stop')active=false;return {ok:true,status:0,output:args[0]==='show'?
  'LoadState=loaded\nActiveState='+ (active?'active':'inactive')+'\nMainPID='+ (active?String(process.pid):'0')+'\nControlPID=0\nType=notify\nFragmentPath='+join(root,nativeServiceName)+'\n':''};};
 const status=async()=>({version:1,generation,pid:process.pid,installation:sha(JSON.stringify(installation)),service:'running',
  delivery,reason:'native-stage-composition-pending',runnerPool:'running',pool:{host:'running',occupied,pending:0}});
 const service=nativeControlService({root,run,status});
 await assert.rejects(service.stageUnit(),/stage-unit-active/);assert.equal(calls.includes('stop'),false);
 occupied=0;delivery='running';await assert.rejects(service.stageUnit(),/stage-unit-active/);assert.equal(calls.includes('stop'),false);
 delivery='blocked';assert.equal((await service.stageUnit()).status,'linked');assert.equal(calls.filter(c=>c==='stop').length,1);assert.equal(calls.includes('start'),false);
}));

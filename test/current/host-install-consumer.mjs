import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {existsSync,symlinkSync} from 'node:fs';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {createHash} from 'node:crypto';
import {withCurrentCore} from '../../src/current-core.mjs';
import {existingControlHost} from '../../src/host-install.mjs';
import {openFileHostStore} from '../../src/host-store.mjs';

test('actual installer accepts a systemd-created unit link only after notify readiness; wrong source, account, type and state fail closed',async()=>withCurrentCore(async(core,context)=>{
  const directory=join(context.root,'managed-host'),unitPath=join(directory,'nibfin-rescue-wakeups.service');
  const links=join(context.root,'unit-links');await fs.mkdir(links,{mode:0o700});const link=join(links,'nibfin-rescue-wakeups.service');
  const secretFile=join(context.root,'completion-secret');await fs.writeFile(secretFile,Buffer.alloc(32,91),{mode:0o600});
  const policy={version:1,repository:'example/host',repositoryId:'42',visibility:'public',ref:'main',stageWorkflow:'.github/workflows/nibfin-stage.yml',
    stageBlob:'a'.repeat(40),rescueBlob:'b'.repeat(40),actorIds:['7'],historical:[],maxRepairs:3,cooldownMs:30000,jobTimeoutMinutes:60,authority:'a'.repeat(64),expiresAt:Date.now()+60000};
  const bindingPath=join(context.root,'binding.mjs');
  const binding=`const unavailable=async()=>{throw Error('Fixture transport unavailable');};
    export const hosts=[{configuration:{policy:${JSON.stringify(policy)},provider:{request:unavailable,collection:unavailable},
      current:unavailable,authorize:unavailable,active:unavailable,capture:unavailable,deterministic:unavailable,adapter:unavailable,
      retainDiagnosis:unavailable,continueRole:unavailable,operations:unavailable,intent:unavailable,completion:unavailable},
      secretFile:${JSON.stringify(secretFile)},excludedRoots:${JSON.stringify(['agent','runner','workspace'].map(n=>join(context.root,n)))},attribute:unavailable,onError:unavailable}];`;
  await fs.writeFile(bindingPath,binding,{mode:0o600});
  const path=join(context.root,'host.json');await fs.writeFile(path,JSON.stringify({version:1,account:userInfo().username,directory,
    binding:{path:bindingPath,sha256:createHash('sha256').update(binding).digest('hex')}}),{mode:0o600});
  let active=false,type='notify',account=userInfo().username;const calls=[];
  // Only the service manager is simulated here. The public package, private
  // source copies, binding/secret checks, file store and symlinks are real.
  const run=args=>{calls.push(args);
    if(args[0]==='link'&&!existsSync(link))symlinkSync(args[1],link);
    if(args[0]==='start')active=true;
    return {ok:true,output:args[0]==='show'?`ActiveState=${active?'active':'inactive'}\nFragmentPath=${link}\nUser=${account}\nType=${type}\n`:''};
  };
  const host=existingControlHost({path,process:run}),request={core,context,repo:policy.repository};
  assert.equal((await host.check(request)).status,'blocked');assert.equal(calls.length,0);assert.equal(existsSync(directory),false);
  assert.equal((await host.install(request)).status,'ready');
  assert.ok((await fs.readFile(unitPath,'utf8')).includes('Type=notify\nNotifyAccess=all\nTimeoutStartSec=45\n'));
  const store=await openFileHostStore(core,{directory:join(directory,'state')});
  try{assert.equal((await store.read()).work.paused,true);assert.equal((await store.rescue('42').read()).revision,0);}finally{await store.close();}
  const record=await fs.readFile(join(directory,'state/store.json'));
  assert.equal((await host.install(request)).status,'ready');assert.deepEqual(await fs.readFile(join(directory,'state/store.json')),record);
  for(const changed of ['active','type','account']){
    if(changed==='active')active=false;if(changed==='type')type='simple';if(changed==='account')account='other-member';
    assert.equal((await host.check(request)).status,'blocked');active=true;type='notify';account=userInfo().username;
  }
  const other=join(links,'other.service');await fs.writeFile(other,await fs.readFile(unitPath),{mode:0o600});
  await fs.unlink(link);await fs.symlink(other,link);assert.equal((await host.check(request)).status,'blocked');
  await fs.unlink(link);await fs.symlink(unitPath,link);
  for(const name of ['host-store.mjs','kernel-lock.mjs','local-state.mjs']){
    const target=join(directory,name),original=await fs.readFile(target);await fs.appendFile(target,'\n// altered\n');
    assert.equal((await host.check(request)).status,'blocked');await fs.writeFile(target,original);
  }
  const installed=await import((await import('node:url')).pathToFileURL(join(directory,'local-state.mjs')).href);
  const adapterRoot=join(context.root,'installed-adapter-lock');await fs.mkdir(adapterRoot,{mode:0o700});
  assert.equal(await installed.localLock(adapterRoot,'installed',()=> 'actual-installed-lease'),'actual-installed-lease');
  const manifestPath=join(directory,'installation.json'),manifestBytes=await fs.readFile(manifestPath),unitBytes=await fs.readFile(unitPath);
  const olderUnit=unitBytes.toString().replace('TimeoutStartSec=45','TimeoutStartSec=44'),previous=JSON.parse(manifestBytes);
  await fs.writeFile(unitPath,olderUnit);previous.unit=createHash('sha256').update(olderUnit).digest('hex');await fs.writeFile(manifestPath,JSON.stringify(previous));
  const olderManifest=await fs.readFile(manifestPath),configuration=await fs.readFile(join(directory,'notifier.json'));
  await assert.rejects(host.install(request),/use reconciled upgrade/);
  assert.deepEqual(await fs.readFile(manifestPath),olderManifest);assert.deepEqual(await fs.readFile(join(directory,'notifier.json')),configuration);
  await fs.writeFile(unitPath,unitBytes);await fs.writeFile(manifestPath,manifestBytes);
  const launcher=join(directory,'notifier.mjs');await fs.appendFile(launcher,'\n// owner-modified\n');
  const before=await fs.readFile(launcher),starts=calls.filter(a=>a[0]==='start').length;
  await assert.rejects(host.install(request),/Modified notifier launcher preserved/);
  assert.deepEqual(await fs.readFile(launcher),before);assert.equal(calls.filter(a=>a[0]==='start').length,starts);
}));

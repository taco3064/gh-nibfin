import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {withCurrentCore} from '../../src/current-core.mjs';
import {writePrivate,readPrivate,sha} from '../../src/local-state.mjs';
import {transitionStagedNativeHost} from '../../src/native-staged-transition.mjs';
import {nativeControlService,nativeServiceUnit,nativeServiceName} from '../../src/native-control-service.mjs';

async function fixture(core,context,name){
 const root=join(context.root,name);await fs.mkdir(root,{mode:0o700});
 const pool={version:2,id:'member-default',capacity:2,targets:[{version:1,repository:'example/project',repositoryId:'42',ownerId:'7',ownerType:'User'}]};
 const previous={version:1,phase:'staged',core:'a'.repeat(64),runnerCount:2,executionPool:{version:1,source:pool.targets[0],targets:pool.targets},runtimeInventory:[]};
 const oldIdentity={packageVersion:'0.1.0',pins:Object.fromEntries(Object.keys(core.distributionIdentity().pins).map(k=>[k,'a'.repeat(64)])),components:{core:{sha256:previous.core}}};
 const relative='node_modules/@nibfin/core/src/index.mjs',bytes='export const distributionIdentity=()=>('+JSON.stringify(oldIdentity)+');';
 await fs.mkdir(join(root,'runtime/node_modules/@nibfin/core/src'),{recursive:true,mode:0o700});
 await fs.writeFile(join(root,'runtime',relative),bytes,{mode:0o600});previous.runtimeInventory.push({path:relative,sha256:sha(bytes)});
 await writePrivate(join(root,'native-installation.json'),previous);
 for(const name of ['runner-1','runner-2']){await fs.mkdir(join(root,name),{mode:0o700});await fs.writeFile(join(root,name,'fixture'),'PINNED FIXTURE',{mode:0o600});}
 const next={...previous,core:core.distributionIdentity().components.core.sha256,executionPool:pool,runtimeInventory:[{path:'next.mjs',sha256:sha('new runtime')}]};
 const calls=[];
 const args={root,previous,next,pool,control:{retire:async()=>{calls.push('retire');return{status:'retired'};}},
  verifyRunner:async(path,options)=>{assert.equal(options.unregistered,true);assert.equal(await fs.readFile(join(path,'fixture'),'utf8'),'PINNED FIXTURE');},
  observeResources:async()=>({status:'unused'}),publish:async()=>{calls.push('publish');await fs.mkdir(join(root,'runtime'),{mode:0o700});await fs.writeFile(join(root,'runtime/next.mjs'),'new runtime',{mode:0o600});}};
 return {root,pool,previous,next,args,calls};
}

test('unactivated staging switches through actual Core transition ownership and retains the original runtime',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context,'transition');
 const external=join(context.root,'unaccepted-conversation');await fs.writeFile(external,'ORIGINAL',{mode:0o600});
 assert.equal((await transitionStagedNativeHost(core,f.args)).status,'switched');
 assert.deepEqual(f.calls,['retire','publish']);
 assert.deepEqual(await readPrivate(join(f.root,'native-installation.json')),f.next);
 assert.deepEqual(await readPrivate(join(f.root,'previous-installation/native-installation.json')),f.previous);
 assert.equal(await fs.readFile(external,'utf8'),'ORIGINAL');
 const retained=await readPrivate(join(f.root,'staging-transition.json'));
 assert.equal(retained.phase,'completed');assert.equal(retained.result.status,'switched');
 assert.equal(retained.result.plan.maintenance,'preserve-pause');
 assert.deepEqual(retained.result.plan.preserve,{conversations:[],pending:[],receipts:[]});
}));

test('existing work, registration or a failed switch is held without reset or an automatic second switch',async()=>withCurrentCore(async(core,context)=>{
 for(const kind of ['work','registration','unknown']){
  const f=await fixture(core,context,kind);
  if(kind==='work')await fs.mkdir(join(f.root,'jobs'),{mode:0o700});
  if(kind==='registration')await fs.writeFile(join(f.root,'runner-1/.runner'),'original',{mode:0o600});
  if(kind==='unknown')f.args.publish=async()=>{f.calls.push('publish');throw Error('interruption');};
  await assert.rejects(transitionStagedNativeHost(core,f.args));
  assert.deepEqual(await readPrivate(join(f.root,'native-installation.json')),f.previous);
  if(kind==='unknown'){
   assert.deepEqual(f.calls,['retire','publish']);
   await assert.rejects(transitionStagedNativeHost(core,f.args),/original-transition-reconciliation-required/);
   assert.deepEqual(f.calls,['retire','publish']);
   assert.deepEqual(await readPrivate(join(f.root,'previous-installation/native-installation.json')),f.previous);
  }else assert.deepEqual(f.calls,[]);
 }
}));

test('retirement verifies inactive owned unit and reconciles a lost disable reply without stopping active work',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'retirement');await fs.mkdir(root,{mode:0o700});
 const unit=nativeServiceUnit(root),generation='12345678-1234-1234-1234-123456789abc';
 await fs.writeFile(join(root,nativeServiceName),unit,{mode:0o600});
 await writePrivate(join(root,'control-service.json'),{version:1,account:userInfo().uid,generation,unit:sha(unit)});
 let loaded=true,active=true;const calls=[];
 const run=args=>{calls.push(args[0]);if(args[0]==='disable')loaded=false;
  return{ok:loaded||args[0]!=='show',status:loaded?0:4,output:args[0]==='show'?`LoadState=${loaded?'loaded':'not-found'}\nActiveState=${active?'active':'inactive'}\nMainPID=${active?'123':'0'}\nControlPID=0\nControlGroup=\nFragmentPath=${loaded?join(root,nativeServiceName):''}\n`:''};};
 const service=nativeControlService({root,run});
 await assert.rejects(service.retire(),/retirement-active-work/);assert.deepEqual(calls,['show']);
 active=false;assert.equal((await service.retire()).status,'retired');
 const before=calls.filter(c=>c==='disable').length;
 await writePrivate(join(root,'control-retirement.json'),{version:1,generation,unit:sha(unit),phase:'intent'});
 assert.equal((await service.retire()).status,'retired');
 assert.equal(calls.filter(c=>c==='disable').length,before);
 assert.equal((await readPrivate(join(root,'control-retirement.json'))).phase,'retired');
 await fs.unlink(join(root,'control-retirement.json'));
 assert.equal((await service.retire()).status,'retired');
 assert.equal(calls.filter(c=>c==='disable').length,before);
}));

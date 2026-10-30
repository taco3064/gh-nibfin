import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {withCurrentCore} from '../../src/current-core.mjs';
import {nativePoolStore} from '../../src/native-pool-store.mjs';
import {writePrivate} from '../../src/local-state.mjs';
const configuration={version:2,id:'member-default',capacity:2,targets:Array.from({length:3},(_,i)=>({version:1,
 repository:'example/project-'+i,repositoryId:String(100+i),ownerId:'7',ownerType:'User'}))};
const request=i=>({id:'request-'+i,repository:'example/project-'+i,repositoryId:String(100+i),jobId:String(200+i),runId:String(300+i),attempt:'1'});

test('durable pool survives reopening, serializes competing projects, and never frees a slot on unknown cleanup',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'pool-test');await fs.mkdir(root,{mode:0o700});
 const store=nativePoolStore(core,{root,configuration});await store.initialize();
 const capacity=core.createRunnerPoolCapacity({configuration,records:store.capacity,observeRelease:async()=>({})});
 const result=await Promise.all([0,1,2].map(i=>capacity.reserve(request(i))));
 assert.deepEqual(result.map(r=>r.status),['reserved','reserved','waiting']);
 const reopened=nativePoolStore(core,{root,configuration});assert.equal((await reopened.status()).occupied,2);
 assert.equal((await capacity.release(request(0))).status,'retained');
 assert.equal((await reopened.initialize()).status,'retained');assert.equal((await reopened.status()).occupied,2);
 assert.equal((await fs.stat(join(root,'pool-state/ledger.json'))).mode&0o777,0o600);
 assert.equal((await fs.stat(join(root,'pool-state'))).mode&0o777,0o700);
}));

test('host journals use durable compare-and-swap and reject changed identity, aliases and links',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'pool-journal');await fs.mkdir(root,{mode:0o700});
 const store=nativePoolStore(core,{root,configuration});await store.initialize();
 const body={version:1,request:request(0),phase:'registration-intent'},value={...body,identity:core.contentIdentity(body)};
 assert.equal((await store.jobs.write({id:'request-0',expected:null,value})).status,'retained');
 assert.equal((await store.jobs.write({id:'request-0',expected:null,value})).status,'conflict');
 assert.deepEqual(await nativePoolStore(core,{root,configuration}).jobs.read('request-0'),value);
 await assert.rejects(store.jobs.read('../request-0'),/selector/);
 await assert.rejects(store.jobs.write({id:'request-0',expected:value.identity,value:{...value,phase:'released'}}),/job-integrity/);
 assert.deepEqual(await store.jobs.read('request-0'),value);
 const path=join(root,'pool-state/ledger.json'),original=await fs.readFile(path);
 await writePrivate(path,{version:1});await assert.rejects(store.status(),/ledger/);await assert.rejects(store.initialize(),/ledger/);
 await fs.writeFile(path,original);await fs.rename(path,path+'.original');await fs.symlink(path+'.original',path);
 await assert.rejects(store.status(),/link/);await fs.unlink(path);await fs.rename(path+'.original',path);
}));

test('an activated missing ledger is unavailable, not silently reinitialized by ordinary reads',async()=>withCurrentCore(async(core,context)=>{
 const root=join(context.root,'pool-missing');await fs.mkdir(root,{mode:0o700});
 const store=nativePoolStore(core,{root,configuration});await store.initialize();
 await fs.rename(join(root,'pool-state/ledger.json'),join(root,'pool-state/retained-ledger.json'));
 await assert.rejects(store.status(),{code:'ENOENT'});
 await assert.rejects(store.capacity.update(()=>null),{code:'ENOENT'});
 await assert.rejects(store.initialize(),/missing-retained-ledger/);
 assert.throws(()=>nativePoolStore(core,{root,configuration:{...configuration,version:1}}));
}));

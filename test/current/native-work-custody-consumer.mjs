import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {withCurrentCore} from '../../src/current-core.mjs';
import {nativePoolStore} from '../../src/native-pool-store.mjs';
import {nativeWorkCustody,observeNativeWorkResources} from '../../src/native-work-custody.mjs';
import {ensurePrivateDirectory,writePrivate,readPrivate} from '../../src/local-state.mjs';
const configuration={version:2,id:'member-default',capacity:2,targets:[{version:1,repository:'example/project',repositoryId:'100',ownerId:'7',ownerType:'User'}]};
async function fixture(core,context){
 const root=join(context.root,'custody-host');await ensurePrivateDirectory(root);
 const store=nativePoolStore(core,{root,configuration});await store.initialize();
 const request={id:'request-0',repository:'example/project',repositoryId:'100',jobId:'200',runId:'300',attempt:'1'},
  lease={request,slot:1,identity:core.contentIdentity({pool:core.contentIdentity(configuration),request,slot:1})};
 const body={version:1,request,lease,runnerId:'700',phase:'active'},record={...body,identity:core.contentIdentity(body)};
 await store.jobs.write({id:request.id,expected:null,value:record});
 const directory=join(root,'jobs',lease.identity),workspace=join(directory,'application/_work/project/.git');
 await ensurePrivateDirectory(workspace);await fs.writeFile(join(workspace,'HEAD'),'ref: refs/heads/original\n',{mode:0o600});
 const bytes=Buffer.from([0,1,2,255]);await fs.writeFile(join(workspace,'unaccepted-object'),bytes,{mode:0o600});
 const transcript=join(directory,'roles/accept');await ensurePrivateDirectory(transcript);
 await fs.writeFile(join(transcript,'conversation.jsonl'),'ORIGINAL CONVERSATION\n',{mode:0o600});
 const outside=join(context.root,'unrelated-model-login');await fs.writeFile(outside,'PRIVATE LOGIN MUST NOT BE FOLLOWED',{mode:0o600});
 await fs.symlink(outside,join(workspace,'external-link'));
 await fs.writeFile(join(directory,'listener.log'),'PRIVATE LISTENER OUTPUT\n',{mode:0o600});
 const child=spawnSync('/usr/bin/true');assert.equal(child.status,0);
 const process={version:1,lease:lease.identity,phase:'terminated',entryPid:child.pid,entryStart:'0',
  controlGroup:'/user.slice/user-'+userInfo().uid+'.slice/fixture-ended.service',exitCode:1};
 await writePrivate(join(directory,'process.json'),process);
 const query={lease:lease.identity,request,process:core.contentIdentity(process),job:{id:'200',run_id:'300',run_attempt:1,runner_id:'700',status:'completed',conclusion:'failure'}};
 const state={unknown:false,container:false,running:false};
 const resources=(q,options)=>observeNativeWorkResources(q,{...options,containers:async args=>{
  if(state.unknown)throw Error('unavailable');if(args[0]==='ps')return state.container?'a'.repeat(64)+'\n':'';
  return JSON.stringify([{Id:'a'.repeat(64),Config:{Labels:{}},State:{Running:state.running,Paused:false},Mounts:[{Source:directory+'/roles/accept'}]}]);
 }});
 const owner=await nativeWorkCustody(core,{root,configuration,store,resources});
 return {root,directory,workspace,bytes,query,state,owner,outside,lease};
}
test('installed Core retains real original Git bytes, conversations and symlink metadata privately before physical cleanup',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);assert.deepEqual(await f.owner.read(f.query),{status:'absent'});
 f.state.unknown=true;await assert.rejects(f.owner.retain(f.query));assert.deepEqual(await f.owner.read(f.query),{status:'absent'});
 f.state.unknown=false;const kept=await f.owner.retain(f.query);assert.equal(kept.status,'retained');
 const snapshot=await readPrivate(join(f.root,'original-work',f.lease.identity,'snapshot.json'));
 assert.ok(snapshot.entries.some(e=>e.path.endsWith('conversation.jsonl')));
 const object=snapshot.entries.find(e=>e.path.endsWith('unaccepted-object'));
 assert.deepEqual(await fs.readFile(join(f.root,'original-work',f.lease.identity,'blobs',object.sha256)),f.bytes);
 assert.ok(snapshot.entries.some(e=>e.kind==='symlink'&&e.path.endsWith('external-link')));
 for(const blob of await fs.readdir(join(f.root,'original-work',f.lease.identity,'blobs')))
  assert.ok(!(await fs.readFile(join(f.root,'original-work',f.lease.identity,'blobs',blob))).includes(Buffer.from('PRIVATE LOGIN MUST NOT BE FOLLOWED')));
 assert.equal(await fs.readFile(f.outside,'utf8'),'PRIVATE LOGIN MUST NOT BE FOLLOWED');
 assert.equal((await fs.stat(join(f.root,'original-work',f.lease.identity,'snapshot.json'))).mode&0o777,0o600);
 assert.deepEqual(await f.owner.retain(f.query),kept);
 f.state.container=true;f.state.running=true;let state=await f.owner.observe(f.query);assert.equal(state.processesTerminated,false);assert.equal(state.resourcesRemoved,false);
 f.state.running=false;state=await f.owner.observe(f.query);assert.equal(state.processesTerminated,true);assert.equal(state.resourcesRemoved,false);
 f.state.container=false;await fs.rm(f.directory,{recursive:true});
 state=await f.owner.observe(f.query);assert.equal(state.workRetained,true);assert.equal(state.processesTerminated,true);assert.equal(state.resourcesRemoved,true);
 assert.equal(await fs.readFile(f.outside,'utf8'),'PRIVATE LOGIN MUST NOT BE FOLLOWED');
 await fs.writeFile(join(f.root,'original-work',f.lease.identity,'blobs',object.sha256),'CHANGED',{mode:0o600});
 await assert.rejects(f.owner.read(f.query),/retained-bytes/);await assert.rejects(f.owner.observe(f.query),/retained-bytes/);
}));

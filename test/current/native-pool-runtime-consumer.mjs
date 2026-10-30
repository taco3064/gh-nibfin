import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {withCurrentCore} from '../../src/current-core.mjs';
import {nativeControlHost} from '../../src/native-host.mjs';
import {nativePoolStore} from '../../src/native-pool-store.mjs';
import {nativePoolRuntime} from '../../src/native-pool-runtime.mjs';
import {ensurePrivateDirectory,readPrivate,writePrivate} from '../../src/local-state.mjs';
const configuration={version:2,id:'member-default',capacity:2,targets:[{version:1,repository:'example/project',repositoryId:'100',ownerId:'7',ownerType:'User'}]};
const request={id:'request-0',repository:'example/project',repositoryId:'100',jobId:'200',runId:'300',attempt:'1'};
async function fixture(core,context){
 const root=join(context.root,'pool-runtime'),original=join(context.root,'original-work'),retained=join(context.root,'retained-work');
 await fs.writeFile(original,'UNACCEPTED ORIGINAL',{mode:0o600});
 await nativeControlHost({root,prepare:async directory=>fs.writeFile(join(directory,'fixture'),'application',{mode:0o600}),verify:async()=>{},
  service:{install:async()=>({diagnostics:[]})}}).prepare({core,context,executionPool:configuration});
 const store=nativePoolStore(core,{root,configuration}),flags={running:true,terminated:true,resources:true,closed:false,retired:false,lostCustodyReply:false},
  order=[],blocks=[],counts={registrations:0,starts:0,retains:0};let remote,custodyRecord;
 const provider=core.createGithubProvider({attempts:1,authorize:r=>r.operation==='runner-pool'&&['POST','DELETE'].includes(r.method),transport:async r=>{
  if(r.method==='POST'){
   counts.registrations++;const name=JSON.parse(r.body).name,settings={agentId:700,agentName:name,ephemeral:true,disableUpdate:true,workFolder:'_work',
    serverUrl:'https://pipelines.actions.githubusercontent.com/fixture/',gitHubUrl:'https://github.com/example/project'};
   const files=Object.fromEntries(Object.entries({'.runner':JSON.stringify(settings),'.credentials':'PRIVATE-CREDENTIAL',
    '.credentials_rsaparams':'PRIVATE-KEY'}).map(([path,value])=>[path,Buffer.from(value).toString('base64')]));
   remote={id:'700',name,ephemeral:true,busy:false};return {status:201,body:JSON.stringify({runner:remote,encoded_jit_config:Buffer.from(JSON.stringify(files)).toString('base64')})};
  }
  if(r.method==='DELETE'){remote=null;return {status:204,body:''};}
  if(r.endpoint.endsWith('/actions/jobs/200'))return {status:200,body:JSON.stringify({id:'200',run_id:'300',run_attempt:1,runner_id:'700',status:'completed',conclusion:'failure'})};
  return {status:remote?200:404,body:JSON.stringify(remote??{})};
 }});
 const custody={read:async()=>custodyRecord??{status:'absent'},retain:async({lease})=>{
  counts.retains++;await fs.copyFile(original,retained,fs.constants.COPYFILE_EXCL);
  custodyRecord={status:'retained',lease,identity:core.contentIdentity({bytes:await fs.readFile(retained,'utf8'),lease})};
  if(flags.lostCustodyReply)throw Error('lost after actual retention');return custodyRecord;
 },observe:async({lease})=>({lease,retention:custodyRecord?.identity,workRetained:!!custodyRecord
   &&await fs.readFile(original,'utf8')===await fs.readFile(retained,'utf8'),processesTerminated:flags.terminated,resourcesRemoved:flags.resources})};
 // Only official archive acquisition, GitHub, admission transport and systemd
 // observations are doubled. Core allocation, durable files, JIT validation,
 // real terminated PID proof, original-work retention and cleanup are real.
 const runtime=nativePoolRuntime(core,{root,configuration,context,provider,custody,
  acquire:async application=>{order.push('prepare');await ensurePrivateDirectory(join(application,'bin'));
   await fs.writeFile(join(application,'bin/Runner.Worker'),'WORKER',{mode:0o700});},
  admit:async()=>{order.push('admission');return {environment:{ACTIONS_RUNNER_HOOK_JOB_STARTED:'/fixture/hook',NIBFIN_RUNNER_JOB_POLICY:'/fixture/policy'},record:{version:1}};},
  serviceFactory:({lease})=>({start:async()=>{order.push('start');counts.starts++;return {status:'started',lease};},
   state:async()=>({status:flags.running?'active':'inactive',pid:flags.running?12345:0,invocation:'a'.repeat(32)}),
   absent:async()=>flags.retired,retire:async()=>{flags.retired=true;return {status:'retired',lease};}})});
 const capacity=core.createRunnerPoolCapacity({configuration,records:store.capacity,observeRelease:runtime.observeRelease});
 const source={inspect:async()=>({authorized:true,launchAuthorized:!flags.closed,identity:core.contentIdentity(request),memberId:'7',request,issue:'55',stage:'deliver',status:'queued'}),
  blocked:async value=>{blocks.push(value);return {status:'retained'};}};
 const make=()=>core.createRunnerPoolHost({configuration,memberId:'7',capacity,records:store.jobs,source,provider,runtime});
 async function finish(){
  flags.running=false;const record=await store.jobs.read(request.id),process=spawnSync('/usr/bin/true');assert.equal(process.status,0);
  await writePrivate(join(root,'jobs',record.lease.identity,'process.json'),{version:1,lease:record.lease.identity,phase:'terminated',
   invocation:'a'.repeat(32),entryPid:process.pid,entryStart:'0',controlGroup:'/user.slice/user-'+userInfo().uid+'.slice/fixture-gone.service',
   configuration:record.configuration,admission:record.admission,exitCode:0});return record;
 }
 return {make,root,original,retained,store,flags,order,blocks,counts,finish};
}
test('installed runtime retains failed Job evidence and original work before service/tree cleanup and capacity reuse',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);assert.equal((await f.make().advance(request.id)).status,'running');assert.deepEqual(f.order,['prepare','admission','start']);
 const active=await f.store.jobs.read(request.id),directory=join(f.root,'jobs',active.lease.identity);
 assert.ok(!JSON.stringify(active).includes('PRIVATE-CREDENTIAL'));assert.equal((await fs.stat(join(directory,'application/.credentials'))).mode&0o777,0o600);
 await f.finish();f.flags.resources=false;f.flags.terminated=false;
 assert.equal((await f.make().advance(request.id)).status,'unresolved');assert.equal(f.counts.retains,0);
 f.flags.terminated=true;
 assert.equal((await f.make().advance(request.id)).status,'unresolved');assert.equal((await f.store.status()).occupied,1);
 assert.equal(f.counts.retains,1);assert.equal(await fs.readFile(f.retained,'utf8'),'UNACCEPTED ORIGINAL');
 f.flags.resources=true;const held=await fs.open(join(directory,'application/.credentials'),'r');
 try{assert.equal((await f.make().advance(request.id)).status,'unresolved');assert.equal((await f.store.status()).occupied,1);}
 finally{await held.close();}
 const unexpected=join(f.root,'nibfin-runner-1.service');await fs.writeFile(unexpected,'PRESERVE REPLACEMENT',{mode:0o600});
 assert.equal((await f.make().advance(request.id)).status,'unresolved');assert.equal((await f.store.status()).occupied,1);
 assert.equal(await fs.readFile(unexpected,'utf8'),'PRESERVE REPLACEMENT');await fs.unlink(unexpected);
 f.flags.closed=true;assert.equal((await f.make().advance(request.id)).status,'released');assert.equal((await f.store.status()).occupied,0);
 assert.equal(f.counts.registrations,1);assert.equal(f.counts.starts,1);assert.equal(f.counts.retains,1);
 await assert.rejects(fs.stat(directory),{code:'ENOENT'});assert.equal(await fs.readFile(f.original,'utf8'),'UNACCEPTED ORIGINAL');
 const result=await readPrivate(join(f.root,'pool-state','outcome-'+active.lease.identity+'.json'));
 assert.equal(result.job.conclusion,'failure');assert.ok(f.blocks.some(b=>b.diagnostic==='native-pool-runtime:cleanup-process-reference'));
}));
test('lost custody acknowledgement reads the same retained work and does not capture or launch a second time',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);await f.make().advance(request.id);await f.finish();f.flags.lostCustodyReply=true;
 assert.equal((await f.make().advance(request.id)).status,'unresolved');assert.equal(f.counts.retains,1);
 f.flags.lostCustodyReply=false;assert.equal((await f.make().advance(request.id)).status,'released');
 assert.equal(f.counts.retains,1);assert.equal(f.counts.starts,1);assert.equal((await f.store.status()).occupied,0);
 assert.equal(await fs.readFile(f.original,'utf8'),'UNACCEPTED ORIGINAL');
}));

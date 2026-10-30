import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {withCurrentCore,currentPin} from '../../src/current-core.mjs';
import {nativePoolCoordinator} from '../../src/native-pool-coordinator.mjs';
import {nativePoolStore} from '../../src/native-pool-store.mjs';
import {ensurePrivateDirectory,writePrivate,readPrivate} from '../../src/local-state.mjs';
import {nativePoolSourceFixture} from '../../tooling/native-pool-source-fixture.mjs';
async function until(predicate){const end=Date.now()+20000;while(Date.now()<end){if(await predicate())return;await new Promise(r=>setTimeout(r,20));}assert.fail('Coordinator did not reach the expected retained state');}

test('maintained coordinator consumes a real authenticated intake, retains original work through Core and releases only after cleanup',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativePoolSourceFixture(core,context),root=join(context.root,'coordinator');await ensurePrivateDirectory(root);
 const store=nativePoolStore(core,{root,configuration:f.configuration});await store.initialize();
 const counts={registrations:0,starts:0,removals:0},state={running:true,retired:false};let remote;
 // GitHub/systemd/archive/admission and Docker observations are explicit doubles.
 // Core, intake authentication, kernel lease, JIT/private files, process proof,
 // original byte retention, physical cleanup and capacity release are real.
 const provider=core.createGithubProvider({attempts:1,authorize:r=>r.operation==='runner-pool',transport:async r=>{
  if(r.method==='POST'){
   counts.registrations++;const name=JSON.parse(r.body).name,settings={agentId:700,agentName:name,ephemeral:true,disableUpdate:true,
    workFolder:'_work',serverUrl:'https://pipelines.actions.githubusercontent.com/fixture/',gitHubUrl:'https://github.com/example/project'};
   const files=Object.fromEntries(Object.entries({'.runner':JSON.stringify(settings),'.credentials':'PRIVATE', '.credentials_rsaparams':'PRIVATE'}).map(([k,v])=>[k,Buffer.from(v).toString('base64')]));
   remote={id:'700',name,ephemeral:true,busy:false};return {status:201,body:JSON.stringify({runner:remote,encoded_jit_config:Buffer.from(JSON.stringify(files)).toString('base64')})};
  }
  if(r.method==='DELETE'){counts.removals++;remote=null;return {status:204,body:''};}
  if(r.endpoint==='/repos/example/project/actions/runners/700')return {status:remote?200:404,body:JSON.stringify(remote??{})};
  const data=f.routes.get(r.endpoint);return {status:data?200:404,body:JSON.stringify(data??{})};
 }});
 const abort=new AbortController(),progress=[];
 const coordinator=await nativePoolCoordinator(core,{root,record:{core:currentPin.coreSha256,executionPool:f.configuration},context,provider,
  installationRoot:f.installationRoot,signal:abort.signal,onProgress:r=>progress.push(r),
  observeResources:async({lease})=>({lease,processesTerminated:!state.running,resourcesRemoved:!state.running}),
  runtimeOptions:{acquire:async path=>{await ensurePrivateDirectory(join(path,'bin'));await fs.writeFile(join(path,'bin/Runner.Worker'),'FIXTURE',{mode:0o700});},
   admit:async()=>({environment:{ACTIONS_RUNNER_HOOK_JOB_STARTED:'/fixture/hook',NIBFIN_RUNNER_JOB_POLICY:'/fixture/policy'},record:{version:1}}),
   serviceFactory:({lease})=>({start:async()=>{counts.starts++;return {status:'started',lease};},
    state:async()=>({status:state.running?'active':'inactive',pid:state.running?12345:0,invocation:'a'.repeat(32)}),
    absent:async()=>state.retired,retire:async()=>{state.retired=true;return {status:'retired',lease};}})}});
 try{
  assert.equal((await coordinator.status()).occupied,0);assert.equal(counts.registrations,0);
  const hint={repository:'example/project',runId:'300',jobId:'200'};
  assert.equal((await coordinator.enqueue(hint)).status,'retained');
  await until(async()=> (await store.jobs.read('request-0'))?.phase==='active');
  await coordinator.enqueue(hint);assert.equal(counts.starts,1);assert.equal(counts.registrations,1);
  const record=await store.jobs.read('request-0'),directory=join(root,'jobs',record.lease.identity);
  await ensurePrivateDirectory(join(directory,'application/_work/project/.git'));
  await fs.writeFile(join(directory,'application/_work/project/.git/ORIGINAL'),'UNACCEPTED ORIGINAL',{mode:0o600});
  await fs.writeFile(join(directory,'listener.log'),'PRIVATE LISTENER',{mode:0o600});
  const child=spawnSync('/usr/bin/true');assert.equal(child.status,0);
  const job=f.routes.get('/repos/example/project/actions/jobs/200');Object.assign(job,{status:'completed',runner_id:'700',conclusion:'failure'});
  state.running=false;
  await writePrivate(join(directory,'process.json'),{version:1,lease:record.lease.identity,phase:'terminated',invocation:'a'.repeat(32),
   entryPid:child.pid,entryStart:'0',controlGroup:'/user.slice/user-'+userInfo().uid+'.slice/fixture-ended.service',
   configuration:record.configuration,admission:record.admission,exitCode:1});
  await until(async()=> (await store.jobs.read('request-0'))?.phase==='released');
  assert.equal((await coordinator.status()).occupied,0);assert.equal(counts.starts,1);assert.equal(counts.registrations,1);assert.equal(counts.removals,1);
  await assert.rejects(fs.stat(directory),{code:'ENOENT'});
  const snapshot=await readPrivate(join(root,'original-work',record.lease.identity,'snapshot.json')),
   original=snapshot.entries.find(e=>e.path.endsWith('/ORIGINAL'));
  assert.equal(await fs.readFile(join(root,'original-work',record.lease.identity,'blobs',original.sha256),'utf8'),'UNACCEPTED ORIGINAL');
  const receipt=await readPrivate(join(root,'original-work','receipt-'+record.lease.identity+'.json'));
  assert.equal(receipt.job.conclusion,'failure');assert.ok(progress.some(r=>r.status==='released'));
 }finally{abort.abort();await coordinator.close();}
}));

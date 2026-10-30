import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {withCurrentCore} from '../../src/current-core.mjs';
import {nativeControlHost} from '../../src/native-host.mjs';
import {nativePoolStore} from '../../src/native-pool-store.mjs';
import {nativeJitConfiguration,nativeRunnerEnvironment} from '../../src/native-jit.mjs';
import {nativeRunnerService,nativeRunnerUnit} from '../../src/native-runner-service.mjs';
import {runNativeRunner} from '../../src/native-runner-process.mjs';
import {ensurePrivateDirectory,writePrivate,sha} from '../../src/local-state.mjs';
const pool={version:2,id:'member-default',capacity:2,targets:[{version:1,repository:'example/project',repositoryId:'100',ownerId:'7',ownerType:'User'}]};
const request={id:'request-0',repository:'example/project',repositoryId:'100',jobId:'200',runId:'300',attempt:'1'};
const encode=files=>Buffer.from(JSON.stringify(files)).toString('base64');
function jit(record){
 const settings={agentId:700,agentName:record.runnerName,ephemeral:true,disableUpdate:true,workFolder:'_work',
  serverUrl:'https://pipelines.actions.githubusercontent.com/fixture/',gitHubUrl:'https://github.com/example/project'};
 const files=Object.fromEntries(Object.entries({'.runner':JSON.stringify(settings),'.credentials':'PRIVATE-JIT-CREDENTIAL',
  '.credentials_rsaparams':'PRIVATE-JIT-KEY'}).map(([path,value])=>[path,Buffer.from(value).toString('base64')]));
 return {settings,files,encoded:encode(files)};
}
const sealed=(core,value)=>({...value,identity:core.contentIdentity(value)});
async function fixture(core,context,{hang=false,badExecutable=false}={}){
 const root=join(context.root,'runner-host'),worker='PINNED WORKER FIXTURE';
 // Real private filesystem, installed Core and child process. Official Runner
 // acquisition/admission and systemd are explicit doubles; no GitHub credentials
 // or actual Runner registration are used by these process-contract tests.
 const prepare=async directory=>fs.writeFile(join(directory,'fixture'),'application',{mode:0o600});
 const verify=async()=>{};
 await nativeControlHost({root,prepare,verify,service:{install:async()=>({diagnostics:[]})}}).prepare({core,context,executionPool:pool});
 const store=nativePoolStore(core,{root,configuration:pool});
 const capacity=core.createRunnerPoolCapacity({configuration:pool,records:store.capacity,observeRelease:async()=>({})});
 const {lease}=await capacity.reserve(request),directory=join(root,'jobs',lease.identity),application=join(directory,'application');
 await ensurePrivateDirectory(join(application,'bin'));
 let record={version:1,request,memberId:'7',lease,phase:'launch-intent',runnerId:'700',runnerName:'nibfin-'+lease.identity.slice(0,40)};
 const configuration=jit(record),environment={ACTIONS_RUNNER_HOOK_JOB_STARTED:join(context.root,'fixture-hook.sh'),NIBFIN_RUNNER_JOB_POLICY:join(context.root,'fixture-policy.json')};
 const prepared=sealed(core,{version:1,lease:lease.identity,workerSha256:sha(worker)}),
  config=sealed(core,{version:1,lease:lease.identity,encoded:configuration.encoded}),
  admission=sealed(core,{version:1,lease:lease.identity,instance:'fixture-instance',environment});
 await writePrivate(join(directory,'prepared.json'),prepared);await writePrivate(join(directory,'configuration.json'),config);
 await writePrivate(join(directory,'admission.json'),admission);
 for(const [name,bytes]of Object.entries(nativeJitConfiguration(configuration.encoded,record).files))
  await fs.writeFile(join(application,name),bytes,{mode:0o600});
 await fs.writeFile(join(application,'bin/Runner.Worker'),worker,{mode:0o700});
 const listener=badExecutable?'#!/does-not-exist\n':`#!/usr/bin/python3
import json, os, re, sys, time
with open('.credentials', encoding='utf8') as credential:
 present = credential.read() == 'PRIVATE-JIT-CREDENTIAL'
payload = {'pid': os.getpid(), 'argv': sys.argv[1:], 'credentialPresent': present,
 'secretEnvironment': [key for key in os.environ if re.search('TOKEN|SECRET|KEY|JITCONFIG|CODEX_HOME|NODE_OPTIONS', key)]}
fd = os.open(${JSON.stringify(join(directory,'child.json'))}, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as output:
 json.dump(payload, output)
sys.stdout.write('PRIVATE-LISTENER-LOG'); sys.stdout.flush()
${hang?'time.sleep(600)':''}
`;
 await fs.writeFile(join(application,'bin/Runner.Listener'),listener,{mode:0o700});
 record=sealed(core,{...record,prepared:prepared.identity,configuration:config.identity,admission:admission.identity});
 await store.jobs.write({id:request.id,expected:null,value:record});
 let admitted=0;
 const options={invocation:'a'.repeat(32),verify,admissionCheck:async(instance,env)=>{
  admitted++;assert.equal(instance.registration,application);assert.deepEqual(env,environment);
 },serviceState:async()=>({pid:process.pid,status:'active',invocation:'a'.repeat(32),controlGroup:'/user.slice/user-'+userInfo().uid+'.slice/fixture.service'})};
 return {root,directory,application,record,store,options,configuration,admitted:()=>admitted};
}

test('JIT decoder rejects traversal, foreign Runner identity, updates, non-ephemeral settings and external servers',()=>{
 const record={runnerId:'700',runnerName:'runner-name',request},valid=jit(record);
 assert.equal(nativeJitConfiguration(valid.encoded,record).settings.agentId,700);
 for(const change of [s=>{s.agentId=701;},s=>{s.disableUpdate=false;},s=>{s.ephemeral=false;},
  s=>{s.gitHubUrl='https://github.com/example/foreign';},s=>{s.serverUrl='https://attacker.example/';},s=>{s.serverUrl='';}]){
  const settings=structuredClone(valid.settings);change(settings);
  assert.throws(()=>nativeJitConfiguration(encode({...valid.files,'.runner':Buffer.from(JSON.stringify(settings)).toString('base64')}),record));
 }
 assert.throws(()=>nativeJitConfiguration(encode({...valid.files,'../credentials':valid.files['.credentials']}),record),/native-jit:files/);
 const environment=nativeRunnerEnvironment({ACTIONS_RUNNER_HOOK_JOB_STARTED:'/private/hook.sh',NIBFIN_RUNNER_JOB_POLICY:'/private/policy.json'});
 assert.ok(!Object.keys(environment).some(k=>/TOKEN|SECRET|KEY|JITCONFIG|CODEX_HOME|NODE_OPTIONS/.test(k)));
 assert.throws(()=>nativeRunnerEnvironment({...environment,GH_TOKEN:'forbidden'}),/admission-environment/);
});

test('real child uses exact private JIT files without credentials in argv/environment; termination cannot release capacity',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context),result=await runNativeRunner(f.root,request.id,f.options);
 assert.equal(result.status,'terminated');assert.equal(f.admitted(),1);
 const child=JSON.parse(await fs.readFile(join(f.directory,'child.json')));
 assert.deepEqual(child.argv,['run']);assert.deepEqual(child.secretEnvironment,[]);assert.equal(child.credentialPresent,true);
 assert.equal((await fs.stat(join(f.directory,'listener.log'))).mode&0o777,0o600);
 assert.equal(await fs.readFile(join(f.directory,'listener.log'),'utf8'),'PRIVATE-LISTENER-LOG');
 assert.equal(JSON.parse(await fs.readFile(join(f.directory,'process.json'))).phase,'terminated');
 assert.equal((await f.store.status()).occupied,1);assert.equal((await f.store.jobs.read(request.id)).phase,'launch-intent');
 await assert.rejects(runNativeRunner(f.root,request.id,f.options),/original-launch-retained/);
 assert.deepEqual(JSON.parse(await fs.readFile(join(f.directory,'child.json'))),child);
}));

test('wrong JIT bytes or service binding prevents launch, and a missing executable remains unconfirmed',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);const path=join(f.application,'.credentials'),original=await fs.readFile(path);
 await fs.writeFile(path,'changed');await assert.rejects(runNativeRunner(f.root,request.id,f.options),/configuration-bytes/);
 await fs.writeFile(path,original);
 await assert.rejects(runNativeRunner(f.root,request.id,{...f.options,serviceState:async()=>({pid:process.pid+1,status:'active',invocation:'a'.repeat(32)})}),/actual-service/);
 await fs.writeFile(join(f.application,'bin/Runner.Listener'),'#!/does-not-exist\n',{mode:0o700});
 const result=await runNativeRunner(f.root,request.id,f.options);assert.equal(result.status,'unconfirmed');
 assert.equal((await f.store.status()).occupied,1);
 assert.equal(JSON.parse(await fs.readFile(join(f.directory,'process.json'))).phase,'unconfirmed');
}));

test('cancellation terminates the actual child while preserving the original slot, credentials and process receipt',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context,{hang:true}),controller=new AbortController();
 const running=runNativeRunner(f.root,request.id,{...f.options,signal:controller.signal});let child;
 try{
  const deadline=Date.now()+5000;
  while(Date.now()<deadline){try{child=JSON.parse(await fs.readFile(join(f.directory,'child.json')));break;}catch{}
   await new Promise(resolve=>setTimeout(resolve,20));}
  assert.ok(child);controller.abort();assert.equal((await running).status,'terminated');
  assert.throws(()=>process.kill(child.pid,0),{code:'ESRCH'});
  assert.equal((await f.store.status()).occupied,1);
  assert.equal(await fs.readFile(join(f.application,'.credentials'),'utf8'),'PRIVATE-JIT-CREDENTIAL');
 }finally{controller.abort();await running.catch(()=>{});}
}));

test('service launch is on demand without enable or restart; a lost start reply is observed, never replayed',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context),calls=[];let active=false;
 const service=nativeRunnerService({root:f.root,selector:request.id,slot:1,lease:f.record.lease.identity,run:args=>{
  calls.push(args);if(args[0]==='start'){active=true;return {ok:false};}
  return {ok:true,output:args[0]==='show'?`ActiveState=${active?'active':'inactive'}\nMainPID=12345\nInvocationID=${'a'.repeat(32)}\nControlGroup=/user.slice/user-${userInfo().uid}.slice/fixture.service\nFragmentPath=${join(f.root,'nibfin-runner-1.service')}\n`:''};
 }});
 await assert.rejects(service.start(),/start-unconfirmed/);assert.equal((await service.state()).status,'active');
 await assert.rejects(service.start(),{code:'EEXIST'});assert.equal(calls.filter(args=>args[0]==='start').length,1);
 assert.ok(!calls.some(args=>['enable','restart'].includes(args[0])));
 const unit=nativeRunnerUnit('/tmp/$literal%unit','request-1',2);
 assert.ok(unit.includes('$$literal%%unit'));assert.ok(unit.includes('Restart=no'));assert.ok(!unit.includes('JITCONFIG'));
}));

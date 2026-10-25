import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir,userInfo} from 'node:os';
import {registerInstance,registrationOptions,registrationProcess} from '../src/register.mjs';
import {withRegistrationCore} from '../src/core.mjs';
import {main} from '../src/cli.mjs';
const secret = 'SECRET_SENTINEL';
// All config/service executables are verifier-owned fixtures for the
// official argument contract, fixture revision 1 on Node 22. No official Runner
// application version, real service or live GitHub registration is executed.
const core = await withRegistrationCore(value => value);
const response = data => ({status:200,headers:{},body:JSON.stringify(data)});
const page = (key,values,total = values.length) => ({[key]:values,total_count:total});
const repo = (name = 'example/a', ownerType = 'User') => ({id:name.endsWith('/b')?'52':'51',full_name:name,
  private:false,visibility:'public',owner:{id:'7',type:ownerType}});
const runner = (id = '9007199254740993', patch = {}) => ({id,name:'fixture',os:'linux',status:'offline',busy:false,
  labels:['self-hosted','linux','X64','nibfin-user-8'].map(name => ({name,type:name.startsWith('nibfin')?'custom':'read-only'})),...patch});
async function fixture(consume, patch = {}) {
  const root = await fs.mkdtemp(join(tmpdir(),'registration fixture ; '));
  try {
    const base = join(root,'demo'), loginSpace = join(base,'login'), calls = [], processes = [];
    await fs.mkdir(loginSpace,{recursive:true,mode:0o700});
    const record = {schemaVersion:1,name:'demo',agent:'codex',loginSpace,githubId:'8',
      account:{uid:userInfo().uid,username:userInfo().username},instances:[]};
    for (let i = 1; i <= 2; i++) {
      const p = join(base,`instance-${i}`), instance = {id:'local-'+i,loginSpace,status:'unregistered',serviceStatus:'not-installed'};
      for (const leaf of ['workspace','registration','service']) {instance[leaf] = join(p,leaf); await fs.mkdir(instance[leaf],{recursive:true,mode:0o700});}
      record.instances.push(instance);
      await fs.writeFile(join(instance.registration,'config.sh'),`#!/usr/bin/env node
const fs=require('node:fs');
const args=process.argv.slice(2), get=k=>args[args.indexOf(k)+1];
fs.writeFileSync('fixture-invocation.json',JSON.stringify(args.map((s,i)=>args[i-1]==='--token'?'REDACTED':s)),{mode:0o600});
fs.writeFileSync('.runner',JSON.stringify({agentId:'9007199254740993',agentName:get('--name'),gitHubUrl:get('--url'),workFolder:get('--work')}),{mode:0o600});
fs.writeFileSync('.credentials','${secret}',{mode:0o600});
console.log('${secret}'); console.error('${secret}');
`,{mode:0o700});
      await fs.mkdir(join(instance.registration,'bin'));
      await fs.writeFile(join(instance.registration,'bin/runsvc.sh'),'#!/bin/sh\nexit 0\n',{mode:0o700});
    }
    await fs.writeFile(join(loginSpace,'agent-login'),'preserved',{mode:0o600});
    await fs.writeFile(join(base,'environment.json'),JSON.stringify(record),{mode:0o600});
    const original = await fs.readFile(join(base,'environment.json'));
    const routes = {
      '/repos/example/a':repo(),'/repos/example/b':repo('example/b','Organization'),
      '/orgs/example':{id:'7',login:'example',type:'Organization'},
      '/repos/example/a/actions/runners?per_page=100':page('runners',[runner()]),
      '/repos/example/b/actions/runners?per_page=100':page('runners',[runner()]),
      '/orgs/example/actions/runners?per_page=100&page=1':page('runners',[runner()]),
      '/orgs/example/actions/runner-groups?per_page=100&page=1':page('runner_groups',[{id:'3',name:'build ; $(false)'}]),
      '/orgs/example/actions/runner-groups/3/runners?per_page=100&page=1':page('runners',[runner()]),...patch.routes};
    const bin=join(root,'fixture bin');await fs.mkdir(bin);
    await fs.writeFile(join(bin,'systemctl'),`#!/usr/bin/env node\nconst fs=require('node:fs');\nif(process.env.GH_TOKEN || process.env.GITHUB_TOKEN) process.exit(9);\nfs.appendFileSync('fixture-service.jsonl',JSON.stringify(process.argv.slice(2))+'\\n');\nconsole.error('${secret}');`,{mode:0o700});
    const dependencies = {root,core,run:async (cmd,args,env) => {
      assert.equal(env.GH_TOKEN,undefined);assert.equal(env.GITHUB_TOKEN,undefined);
      return {ok:true,identity:'8'};
    },transport:async request => {
      calls.push(request);
      assert.equal(request.identity,'local-stored-login');
      if (request.endpoint.endsWith('/registration-token')) return patch.tokenResponse ?? response({token:secret,expires_at:new Date(Date.now()+3600000).toISOString()});
      const route = routes[request.endpoint];
      if (route === undefined) throw Error('Unexpected '+request.endpoint);
      return typeof route === 'function' ? route(request) : route.status ? route : response(route);
    },process:async (cmd,args,options) => {
      processes.push({cmd,args:[...args],...options});
      assert.equal(options.env.GH_TOKEN,undefined);assert.equal(options.env.GITHUB_TOKEN,undefined);
      return registrationProcess(cmd,args,{...options,env:{...options.env,PATH:bin+':'+options.env.PATH}});
    }};
    const state = {root,base,record,original,calls,processes,routes,dependencies,
      register:(args = ['--repo','example/a']) => registerInstance(['--runner','demo',...args],dependencies),
      binding:async (i = 0) => JSON.parse(await fs.readFile(join(record.instances[i].registration,'binding.json'),'utf8'))};
    await consume(state);
    assert.deepEqual(await fs.readFile(join(base,'environment.json')),original);
    assert.equal(await fs.readFile(join(loginSpace,'agent-login'),'utf8'),'preserved');
    for (const instance of record.instances) {
      assert.equal((await fs.readdir(instance.service)).includes('registration.lock'),false);
      for (const name of ['binding.json','binding.json.pending']) {
        try {assert.ok(!(await fs.readFile(join(instance.registration,name),'utf8')).includes(secret));} catch (error) {if(error.code!=='ENOENT') throw error;}
      }
    }
  } finally {await fs.rm(root,{recursive:true,force:true});}
}
const rejected = async (f, pattern) => {
  await assert.rejects(f,error => {assert.ok(!error.message.includes(secret));return pattern ? pattern.test(error.message) : true;});
};
test('AC1 strict grammar has no I/O on rejection; target/group/method options',async () => {
  for (const args of [[],['--runner','demo'],['--repo','example/a'],['--runner','demo','--repo','example/a','--org','example'],
    ['--runner','demo','--repo','example/a','--group','x'],['--runner','demo','--org','example','--method','auto'],
    ['--runner','demo','--org','example','--method','weird'],['--runner','demo','--org','example','extra'],
    ['--runner','demo','--org','example','--org','example'],['--runner','demo','--bad','x'],['--runner','--org','example'],
    ['--runner','demo','--org','example','--group','x\n']]) {
    assert.throws(()=>registrationOptions(args));
    await rejected(()=>registerInstance(args,{core:new Proxy({}, {get(){assert.fail('No core access');}})}));
  }
  assert.equal(registrationOptions(['--runner','demo','--org','example','--method','manual']).name,'demo');
});
test('AC1/3/5 two original instances bind to separate public repos with exact process boundaries', () => fixture(async f => {
  process.env.GH_TOKEN=secret;process.env.GITHUB_TOKEN=secret;
  try {
    const a = await f.register(), b = await f.register(['--repo','example/b']);
    assert.equal(a.instanceId,'local-1');assert.equal(b.instanceId,'local-2');
    assert.equal(a.runnerId,'9007199254740993');assert.equal(a.label,'nibfin-user-8');
    assert.equal(a.status,'offline');assert.equal(a.busy,false);assert.equal(a.service.status,'active');
    assert.equal((await fs.readdir(f.base)).filter(s=>s.startsWith('instance-')).length,2);
    const configs = f.processes.filter(p=>p.cmd.endsWith('config.sh'));
    assert.equal(configs.length,2);assert.notEqual(configs[0].cwd,configs[1].cwd);
    for (const [i,p] of configs.entries()) {
      assert.ok(p.args.includes(secret));assert.ok(!p.args.includes('--replace'));
      assert.equal(p.args[p.args.indexOf('--work')+1],f.record.instances[i].workspace);
      assert.equal(p.args[p.args.indexOf('--labels')+1],'nibfin-user-8');
      assert.ok(!p.args.includes('--runnergroup'));
      const unit = await fs.readFile(join(f.record.instances[i].service,`nibfin-demo-local-${i+1}.service`),'utf8');
      assert.ok(unit.includes(f.record.instances[i].registration));assert.match(unit,/UnsetEnvironment=GH_TOKEN GITHUB_TOKEN/);
      assert.equal((await fs.stat(join(f.record.instances[i].registration,'binding.json'))).mode & 0o777,0o600);
    }
    assert.ok(!f.calls.some(c=>c.endpoint.startsWith('/orgs/')));
    await rejected(()=>f.register(),/No remaining/);
  } finally {delete process.env.GH_TOKEN;delete process.env.GITHUB_TOKEN;}
}));
test('AC2 Org-owned repo and Org group registration are separate; metacharacters remain one argument', () => fixture(async f => {
  const result = await f.register(['--org','example','--group','build ; $(false)']);
  assert.equal(result.registration.id,'7');assert.equal(result.groupId,'3');
  const config = f.processes.find(p=>p.cmd.endsWith('config.sh'));
  assert.equal(config.args.at(-1),'build ; $(false)');assert.ok(config.args.includes('https://github.com/example'));
  assert.ok(!f.calls.some(c=>c.endpoint.startsWith('/repos/')));
  assert.equal(result.repositoryId,undefined);
}));
test('AC2 target/member/auth/endpoint management failures stop configuration and sanitize',async () => {
  for (const status of [401,403,404,429,500]) await fixture(async f => {
    f.routes['/repos/example/a']={status,headers:{},body:secret};
    await rejected(()=>f.register(),/GitHub/);
    assert.equal(f.processes.filter(p=>p.cmd.endsWith('config.sh')).length,0);
    assert.ok(!f.calls.some(c=>c.method==='POST'));
  });
  for (const target of [{...repo(),private:true,visibility:'private'},{...repo(),full_name:'elsewhere/a'}, {...repo(),owner:{id:'7',type:'Alien'}}]) await fixture(async f=> {
    f.routes['/repos/example/a']=target; await rejected(()=>f.register(),/Target/);assert.equal(f.calls.length,1);
  });
  for (const run of [async()=>({ok:false,http:401}),async()=>({ok:true,identity:'9'})]) await fixture(async f=>{
    f.dependencies.run=run;await rejected(()=>f.register());assert.equal(f.calls.length,0);
  });
  for(const status of [401,403,404,429]) await fixture(async f=>{
    await rejected(()=>f.register(),/GitHub/);assert.ok(!(await f.binding()).disposition.includes('registered'));
    assert.ok(!f.processes.some(p=>p.cmd.endsWith('config.sh')));
    await rejected(()=>f.register(),/reconciliation/);
  },{tokenResponse:{status,headers:{},body:secret}});
});
test('AC3 invalid/expired short-lived token responses cannot configure',async () => {
  for(const data of [{token:secret,expires_at:'2000-01-01'},{token:'bad\n'+secret,expires_at:new Date(Date.now()+10000).toISOString()},
    {expires_at:new Date(Date.now()+10000).toISOString()},{token:secret,expires_at:'never'}]) await fixture(async f=> {
    await rejected(()=>f.register(),/Malformed or expired/);assert.equal((await f.binding()).disposition,'unknown');
    assert.ok(!f.processes.some(p=>p.cmd.endsWith('config.sh')));
  },{tokenResponse:response(data)});
});
test('AC3 real process nonzero/timeout/cancellation discard output and retain unknown binding',async () => {
  for (const mode of ['exit','timeout','cancelled']) await fixture(async f => {
    const script = join(f.record.instances[0].registration,'config.sh');
    await fs.writeFile(script,`#!/usr/bin/env node\nconsole.error('${secret}');${mode==='exit'?'process.exit(1);':'setInterval(()=>{},1000);'}`,{mode:0o700});
    f.dependencies.timeoutMs=250;
    if(mode==='cancelled') {const c=new AbortController();f.dependencies.signal=c.signal;const run=f.dependencies.process;f.dependencies.process=(cmd,...args)=>{if(cmd.endsWith('config.sh')) setTimeout(()=>c.abort(),50);return run(cmd,...args);};}
    const start=Date.now();await rejected(()=>f.register());assert.ok(Date.now()-start < 3000);
    assert.equal((await f.binding()).disposition,'unknown');
    await rejected(()=>f.register(),/reconciliation/);
  });
});
test('AC3/5 prerequisites, unsafe paths, malformed records and pending states reject before API effects',async () => {
  const mutations = [async f=>fs.rm(join(f.record.instances[0].registration,'config.sh')),
    async f=>{await fs.rm(f.record.instances[0].workspace,{recursive:true});await fs.symlink('/tmp',f.record.instances[0].workspace);},
    async f=>{f.record.instances[1].id=f.record.instances[0].id;await fs.writeFile(join(f.base,'environment.json'),JSON.stringify(f.record));},
    async f=>fs.writeFile(join(f.record.instances[0].registration,'.runner'),'{}'),
    async f=>{f.dependencies.account={...userInfo(),uid:userInfo().uid+1};},
    async f=>{f.dependencies.process=async()=>({ok:false});}];
  for(const mutate of mutations) {
    // Record corruption is intentional in this case, so restore it before fixture invariants.
    await fixture(async f=>{await mutate(f);await rejected(()=>f.register());assert.ok(!f.calls.some(c=>c.method==='POST'));
      await fs.writeFile(join(f.base,'environment.json'),f.original);});
  }
});
test('AC4 real N15 readback rejects replaced IDs, labels/platform, incomplete and inaccessible inventories',async () => {
  for(const data of [page('runners',[]),page('runners',[runner('99')]),page('runners',[runner(undefined,{os:'windows'})]),
    page('runners',[runner(undefined,{labels:[{name:'nibfin-user-9'}]})]),page('runners',[runner()],2),
    {status:403,headers:{},body:secret}, {status:200,headers:{link:'<https://api.github.com/repos/example/a/actions/runners?per_page=100&page=2>; rel="next"'},body:JSON.stringify(page('runners',[runner()],2))}]) await fixture(async f=> {
    f.routes['/repos/example/a/actions/runners?per_page=100']=data;
    await rejected(()=>f.register(),/readback/);assert.equal((await f.binding()).disposition,'unknown');
    assert.ok(!f.processes.some(p=>p.args.includes('start')));
  });
  await fixture(async f=>{
    f.routes['/repos/example/a/actions/runners?per_page=100']=page('runners',[runner(undefined,{status:'online',busy:true})]);
    const result=await f.register();assert.equal(result.status,'online');assert.equal(result.busy,true);
  });
});
test('AC4 malformed local ID and configuration scope cannot publish success',async () => {
  for(const change of [s=>({...s,agentId:'01'}),s=>({...s,agentId:undefined}),s=>({...s,gitHubUrl:'https://github.com/elsewhere/x'}),s=>({...s,workFolder:'/tmp'})]) await fixture(async f=>{
    const original=f.dependencies.process;f.dependencies.process=async(...args)=>{
      const result=await original(...args);
      if(args[0].endsWith('config.sh')) {const path=join(f.record.instances[0].registration,'.runner');await fs.writeFile(path,JSON.stringify(change(JSON.parse(await fs.readFile(path,'utf8')))));}
      return result;
    };
    await rejected(()=>f.register(),/identity/);assert.equal((await f.binding()).disposition,'unknown');
  });
});
test('AC5 service and publication failure preserve completed other instance; retry never reconfigures',async () => {
  for(const mode of ['service','publication']) await fixture(async f=>{
    const first=await f.register();const original=f.dependencies.process;
    if(mode==='service') f.dependencies.process=async(cmd,args,opts)=>args.includes('start')?{ok:false}:original(cmd,args,opts);
    else {let n=0; f.dependencies.publish=async value=>{
      if(n++) throw Error(secret);
      await fs.writeFile(join(f.record.instances[1].registration,'binding.json'),JSON.stringify(value),{mode:0o600});
    };}
    await rejected(()=>f.register(['--repo','example/b']));
    assert.deepEqual(await f.binding(),first);
    const before=f.processes.filter(p=>p.cmd.endsWith('config.sh')).length;
    delete f.dependencies.publish;
    await rejected(()=>f.register(['--repo','example/b']));
    assert.equal(f.processes.filter(p=>p.cmd.endsWith('config.sh')).length,before);
  });
});
test('AC1 CLI dispatch rejects invalid registration with nonzero status',async()=>{
  assert.equal(await main(['runner','register','--runner','demo','--method','auto']),2);
});
test('AC4 selected instance matches through the production loader; same-target known instances reconcile together',()=>fixture(async f=>{
  delete f.dependencies.core;
  const a=await f.register();
  const second=join(f.record.instances[1].registration,'config.sh');
  await fs.writeFile(second,(await fs.readFile(second,'utf8')).replace("agentId:'9007199254740993'","agentId:'9007199254740994'"));
  f.routes['/repos/example/a/actions/runners?per_page=100']=page('runners',[runner(),runner('9007199254740994')]);
  const b=await f.register();assert.notEqual(a.runnerId,b.runnerId);assert.equal(b.instanceId,'local-2');
}));
test('AC4 Org complete pagination/group membership and late failures remain bounded without repo context',async()=>{
  for(const good of [true,false]) await fixture(async f=>{
    const path='/orgs/example/actions/runners';
    f.routes[path+'?per_page=100&page=1']={status:200,headers:{link:`<https://api.github.com${path}?per_page=100&page=2>; rel="next", <https://api.github.com${path}?per_page=100&page=2>; rel="last"`},body:JSON.stringify(page('runners',[runner('22')],2))};
    f.routes[path+'?per_page=100&page=2']=good?{status:200,headers:{link:`<https://api.github.com${path}?per_page=100&page=1>; rel="prev"`},body:JSON.stringify(page('runners',[runner()],2))}:{status:403,headers:{},body:secret};
    if(good) assert.equal((await f.register(['--org','example','--group','build ; $(false)'])).groupId,'3');
    else {await rejected(()=>f.register(['--org','example']));assert.equal((await f.binding()).disposition,'unknown');}
    assert.ok(!f.calls.some(c=>c.endpoint.startsWith('/repos/')));
  });
  for(const change of [f=>{f.routes['/orgs/example/actions/runner-groups/3/runners?per_page=100&page=1']=page('runners',[]);},
    f=>{f.routes['/orgs/example/actions/runners?per_page=100&page=1']=page('runners',[runner(undefined,{labels:[...runner().labels,{name:'nibfin-user-9'}]})]);}]) await fixture(async f=>{
    change(f);await rejected(()=>f.register(['--org','example','--group','build ; $(false)']));assert.equal((await f.binding()).disposition,'unknown');
  });
});
test('AC4 known bindings from another fixed-Agent environment participate in complete N15 readback',()=>fixture(async f=>{
  await f.register();
  const moved=join(f.root,'other');await fs.rename(f.base,moved);
  const other=JSON.parse(f.original);other.name='other';
  const replace=value=>typeof value==='string'?value.replace(f.base,moved):value;
  other.loginSpace=replace(other.loginSpace);
  for(const item of other.instances) {item.id='other-'+item.id;for(const key of ['loginSpace','workspace','registration','service']) item[key]=replace(item[key]);}
  const bindingPath=join(other.instances[0].registration,'binding.json'), binding=JSON.parse(await fs.readFile(bindingPath,'utf8'));
  binding.instanceId=other.instances[0].id;await fs.writeFile(bindingPath,JSON.stringify(binding));
  await fs.writeFile(join(moved,'environment.json'),JSON.stringify(other));
  // Move the remaining unregistered instance into a new complete G03 descriptor.
  await fs.mkdir(f.base,{mode:0o700});await fs.mkdir(f.record.loginSpace,{mode:0o700});await fs.writeFile(join(f.record.loginSpace,'agent-login'),'preserved');
  for(const item of f.record.instances) await fs.cp(join(moved,'instance-2'),join(f.base,item.id==='local-1'?'instance-1':'instance-2'),{recursive:true});
  await fs.writeFile(join(f.base,'environment.json'),f.original);
  const path=join(f.record.instances[0].registration,'config.sh');await fs.writeFile(path,(await fs.readFile(path,'utf8')).replace("agentId:'9007199254740993'","agentId:'9007199254740994'"));
  f.routes['/repos/example/a/actions/runners?per_page=100']=page('runners',[runner(),runner('9007199254740994')]);
  assert.equal((await f.register()).runnerId,'9007199254740994');
}));
test('AC4 literal numeric large local/API IDs survive parsing; malformed private configuration stays sanitized',async()=>{
  for(const malformed of [false,true]) await fixture(async f=>{
    const run=f.dependencies.process;f.dependencies.process=async(...args)=>{
      const result=await run(...args);
      if(args[0].endsWith('config.sh')) {
        const path=join(f.record.instances[0].registration,'.runner'), text=await fs.readFile(path,'utf8');
        await fs.writeFile(path,malformed?'{'+secret:text.replace('"9007199254740993"','9007199254740993'));
      }
      return result;
    };
    if(malformed) {await rejected(()=>f.register());assert.equal((await f.binding()).disposition,'unknown');}
    else {
      f.routes['/repos/example/a/actions/runners?per_page=100']={status:200,headers:{},body:JSON.stringify(page('runners',[runner()])).replace('"9007199254740993"','9007199254740993')};
      assert.equal((await f.register()).runnerId,'9007199254740993');
    }
  });
});

// Automatic fixtures invoke the installed N17 public entry and maintained
// producer protection, then real disposable config/systemctl subprocesses.
const {withAutomaticCore,verifyAutomaticCore} = await import('../src/core.mjs');
const {createHash} = await import('node:crypto');
const {pathToFileURL} = await import('node:url');
const {validateSource,automaticBinding} = await import('../src/automatic.mjs');
const {binding:validateLifecycleBinding} = await import('../src/unregister.mjs');
const {autoCore,zipFixture} = await withAutomaticCore(async (autoCore,{root})=>{
  const source=await fs.readFile(new URL('../docs/handoffs/n17/custody-fixture.mjs',import.meta.url),'utf8');
  const path=join(root,'fixture.mjs');
  await fs.writeFile(path,source.replace("'../src/index.mjs'",JSON.stringify(join(root,'consumer.mjs'))));
  return {autoCore,zipFixture:(await import(pathToFileURL(path).href)).zipFixture};
});
const workflowText='run-name: ${{ inputs.requestId }}\n# verifier-owned protected integration fixture';
const approved={contract:'n17-dispatch-v1',repository:'example/a',repositoryId:'51',workflowId:'71',
  path:'.github/workflows/register.yml',ref:'main',headSha:'a'.repeat(40),
  workflowSha256:createHash('sha256').update(workflowText).digest('hex')};
async function autoFixture(action,patch={}) {
  return fixture(async f=>{
    let run,encrypted,hostResult,posts=0,polls=0,clock=Date.now();const memberCalls=[];
    const c={...approved,...patch.configuration};
    const old=f.dependencies.transport;
    Object.assign(f.dependencies,{core:autoCore,distributionVerifier:async()=>{},configuration:async()=>c,confirm:async()=>patch.confirm!==false,
      wait:async()=>{clock+=1001;},now:()=>clock,waitMs:5000,timeoutMs:patch.timeoutMs??30000,
      archive:async()=>{if(patch.downloadFailure)throw Error(secret);return {status:200,bytes:patch.zip?patch.zip(encrypted):zipFixture([['response.json',encrypted]])};},
      transport:async request=>{
        memberCalls.push(request);
        if (request.endpoint.includes('/actions/runners')) assert.fail('No member Runner management call allowed');
        const prefix='/repos/example/a';let data;
        if(request.endpoint===prefix) return response({...repo(),permissions:{push:true},...patch.target});
        if(request.endpoint===prefix+'/actions/workflows/71') data={id:'71',path:c.path,state:'active'};
        else if(request.endpoint===prefix+'/git/ref/heads/main') data={object:{sha:c.headSha,type:'commit'}};
        else if(request.endpoint.startsWith(prefix+'/contents/')) data={encoding:'base64',content:Buffer.from(workflowText).toString('base64')};
        else if(request.endpoint.endsWith('/dispatches')) {
          posts++;assert.equal(request.method,'POST');const body=JSON.parse(request.body);
          assert.deepEqual(Object.keys(body),['ref','inputs']);assert.equal(body.ref,'main');assert.equal(body.inputs.level,'repository');
          if(patch.dispatchLoss) throw Error(secret);
          run={id:'91',run_attempt:1,workflow_id:'71',path:c.path,head_sha:c.headSha,head_branch:'main',event:'workflow_dispatch',
            display_title:body.inputs.requestId,actor:{id:'8'},triggering_actor:{id:'8'},repository:{id:'51',full_name:'example/a'},status:'completed',conclusion:'success',...patch.run};
          const host=autoCore.createRegistrationHost({authenticate:async()=>({authenticated:true,event:'workflow_dispatch',
            requestId:body.inputs.requestId,requester:{id:'8',login:'member'},repository:'example/a',repositoryId:'51',runId:'91',attempt:'1',level:'repository',publicKey:body.inputs.publicKey}),
            policy:async()=>({enabled:patch.policy!==false,repository:'example/a',repositoryId:'51',capabilities:{'administration:write':true}}),
            transport:async r=>{
              let d;
              if(r.method==='POST') d={token:secret,expires_at:new Date(Date.now()+(patch.shortExpiry?1000:3600000)).toISOString()};
              else if(r.endpoint===prefix)d={...repo(),owner:{id:'7',type:'User',login:'example'}};
              else if(r.endpoint==='/users/member')d={id:'8',login:'member'};
              else d={permission:patch.permission??'write',user:{id:'8',login:'member'}};
              return response(d);
            },channel:{publish:async({bytes})=>{encrypted=bytes;return {outcome:'delivered',ref:{}};}}});
          hostResult=await host.issue('protected-fixture-handle');
          if(hostResult.outcome!=='delivered'){run.conclusion='failure';}
          if(patch.envelope && encrypted)encrypted=patch.envelope(encrypted);
          return {status:204,headers:{},body:''};
        } else if(request.endpoint.includes('/workflows/71/runs?')) {
          polls++;data=page('workflow_runs',polls<=(patch.delayed??0)?[]:patch.ambiguous?[run,run]:run?[run]:[]);
        } else if(request.endpoint===prefix+'/actions/runs/91') data=patch.readRun?patch.readRun(run):run;
        else if(request.endpoint.endsWith('/artifacts?per_page=100')) {
          const bytes=patch.zip?patch.zip(encrypted):zipFixture([['response.json',encrypted]]);
          data=page('artifacts',patch.noArtifact?[]:[{id:'92',name:`registration-${run.display_title}-1`,expired:false,
            expires_at:'2030-01-01T00:00:00Z',workflow_run:{id:'91'},size_in_bytes:bytes.length,...patch.artifact}]);
        } else return old(request);
        return response(data);
      }});
    for(const instance of f.record.instances) {
      const path=join(instance.registration,'config.sh');let s=await fs.readFile(path,'utf8');
      s=s.replace("fs.writeFileSync('.credentials','"+secret+"'", "fs.writeFileSync('.credentials',JSON.stringify({scheme:'OAuth',data:{clientId:'fixture'}})");
      s+="\nfs.writeFileSync('.credentials_rsaparams',JSON.stringify({fixture:'private-key-material'}),{mode:0o600});";
      if(instance.id==='local-2')s=s.replace("agentId:'9007199254740993'","agentId:'9007199254740994'");
      if(patch.config)s=patch.config(s);
      await fs.writeFile(path,s,{mode:0o700});
    }
    const proc=f.dependencies.process;
    f.dependencies.process=async(cmd,args,opts)=>{
      if(patch.service && args.includes(patch.service))return {ok:false,reason:'timeout'};
      if(patch.cancelConfig && cmd.endsWith('config.sh')) {const c=new AbortController(),timer=setTimeout(()=>c.abort(),50);
        try{return await proc(cmd,args,{...opts,signal:c.signal});}finally{clearTimeout(timer);}}
      return proc(cmd,args,opts);
    };
    if(patch.remote403)f.dependencies.observeRemote=async()=>{throw Error('403 '+secret);};
    const register=()=>f.register(['--repo','example/a','--method','auto']);
    await action({...f,register,memberCalls,posts:()=>posts,host:()=>hostResult});
  });
}
test('auto: installed protected response and real process completion for non-admin members, separate unknown remote',async()=>{
  for(const permission of ['write','maintain','admin']) await autoFixture(async f=>{
    const result=await f.register();assert.ok(automaticBinding(result));assert.equal(result.runnerId,'9007199254740993');
    assert.equal(result.service.status,'active');assert.equal(result.status,null);assert.equal(result.busy,null);
    assert.equal(result.remote.state,'unknown');assert.equal(f.posts(),1);assert.equal(f.host().outcome,'delivered');
    assert.equal(validateLifecycleBinding(result,f.record.instances[0],f.record,autoCore,userInfo()).runnerId,result.runnerId);
    assert.ok(!(JSON.stringify(await f.binding())).includes(secret));
  },{permission,remote403:true,delayed:1});
});
test('auto: two requests use exactly the two original slots, cancellation and no-slot preserve count',async()=>{
  await autoFixture(async f=>{await f.register();await f.register();assert.equal(f.posts(),2);await rejected(f.register,/No remaining/);});
  await autoFixture(async f=>{assert.deepEqual(await f.register(),{cancelled:true});assert.equal(f.posts(),0);}, {confirm:false});
});
test('auto: denied eligibility and protected policy, unavailable installation and dispatch uncertainty cannot configure or redispatch',async()=>{
  for(const patch of [{permission:'read'},{permission:'triage'},{policy:false},{target:{permissions:{pull:true}}},
    {target:{private:true}},{target:{id:'999'}},{configuration:{workflowSha256:'b'.repeat(64)}},{dispatchLoss:true}]) {
    await autoFixture(async f=>{await rejected(f.register);assert.equal(f.processes.filter(p=>p.cmd.endsWith('config.sh')).length,0);
      if(f.posts()){const posts=f.posts();await rejected(f.register);assert.equal(f.posts(),posts);}},patch);
  }
});
test('auto: authentic source and exact artifact provenance reject wrong bindings and ambiguous runs',async()=>{
  const patches=[{ambiguous:true},{readRun:r=>({...r,id:'93'})},{run:{status:'in_progress',conclusion:null}},{shortExpiry:true,delayed:2},{run:{workflow_id:'72'}},{run:{path:'.github/workflows/evil.yml'}},{run:{head_sha:'b'.repeat(40)}},
    {run:{head_branch:'other'}},{run:{event:'push'}},{run:{actor:{id:'9'}}},{run:{triggering_actor:{id:'9'}}},
    {run:{run_attempt:2}},{run:{repository:{id:'52'}}},{run:{conclusion:'failure'}},{run:{conclusion:'cancelled'}},
    {run:{display_title:'unrelated'}},{artifact:{workflow_run:{id:'92'}}},{artifact:{expired:true}},{noArtifact:true},{downloadFailure:true},
    {zip:b=>zipFixture([['response.json',b],['extra','x']])},{zip:b=>zipFixture([['../response.json',b]])},
    {envelope:b=>{const e=JSON.parse(b);e.context.requesterId='9';return Buffer.from(JSON.stringify(e));}}];
  for(const patch of patches)await autoFixture(async f=>{await rejected(f.register);assert.equal(f.posts(),1);
    assert.equal(f.processes.filter(p=>p.cmd.endsWith('config.sh')).length,0);await rejected(f.register);assert.equal(f.posts(),1);},patch);
});
test('auto: false-success settings/credentials and uncertain service effects preserve identity and block repeat requests',async()=>{
  for(const patch of [{config:s=>s+'\nprocess.exit(1);'},
    {config:s=>s+'\nsetInterval(()=>{},1000);',timeoutMs:150}, {config:s=>s+'\nsetInterval(()=>{},1000);',cancelConfig:true}, {config:s=>s.replace("fs.writeFileSync('.runner'","false && fs.writeFileSync('.runner'")},
    {config:s=>s.replace("agentId:'9007199254740993'","agentId:'0'")},{config:s=>s.replace("gitHubUrl:get('--url')","gitHubUrl:'https://github.com/wrong/target'")},
    {config:s=>s.replace("agentName:get('--name')","agentName:'stale'")},{config:s=>s.replace("workFolder:get('--work')","workFolder:'/tmp/wrong'")},
    {config:s=>s.replace("scheme:'OAuth'","scheme:'malformed'")},
    {config:s=>s.replace("fs.writeFileSync('.credentials_rsaparams'","false && fs.writeFileSync('.credentials_rsaparams'")},...['link','enable','start','is-active'].map(service=>({service}))]) {
    await autoFixture(async f=>{await rejected(f.register);assert.equal((await f.binding()).disposition,'unknown');
      await rejected(f.register);assert.equal(f.posts(),1);
      if(patch.service){assert.equal((await f.binding()).configuration.state,'completed');assert.notEqual((await f.binding()).service.status,'active');}},patch);
  }
});
test('auto: N17 provenance/archive changes reject before installing, actual exports retained',async()=>{
  const directory=await fs.mkdtemp(join(tmpdir(),'n17 tamper '));
  try {for(const name of ['provenance.json','nibfin-core.tgz'])await fs.copyFile(new URL('../docs/handoffs/n17/'+name,import.meta.url),join(directory,name));
    for(const name of ['provenance.json','nibfin-core.tgz']) {
      const original=await fs.readFile(join(directory,name));await fs.writeFile(join(directory,name),Buffer.concat([original,Buffer.from('x')]));
      await assert.rejects(()=>verifyAutomaticCore(pathToFileURL(directory+'/')));await fs.writeFile(join(directory,name),original);
    }
  } finally {await fs.rm(directory,{recursive:true,force:true});}
});

test('auto: list and safe lifecycle consumers retain unknown remote without inventing idle or authorizing deletion',async()=>{
  const {listEnvironments}=await import('../src/list.mjs');
  const {unregisterInstance}=await import('../src/unregister.mjs');
  const {removeEnvironment}=await import('../src/remove.mjs');
  await autoFixture(async f=>{
    await f.register();
    const listed=await listEnvironments(['--runner','demo'],{...f.dependencies,service:async()=>({state:'active'})});
    const i=listed.environments[0].instances[0];assert.equal(i.registration.runnerId,'9007199254740993');
    assert.equal(i.local.state,'active');assert.equal(i.remote.state,'unknown');assert.equal(i.remote.busy,null);
    const result=await unregisterInstance(['--runner','demo','--repo','example/a'],{...f.dependencies,termination:async()=>({})});
    assert.equal(result.complete,false);assert.equal(result.remote,'unknown');assert.equal(f.posts(),1);
    assert.ok((await fs.readdir(f.record.instances[0].registration)).includes('.runner'));
  });
  await autoFixture(async f=>{
    await f.register();const result=await removeEnvironment(['--runner','demo','--yes'],{...f.dependencies,termination:async()=>({})});
    assert.equal(result.complete,false);assert.equal(f.posts(),1);
  });
});
test('auto: missing/tampered official distribution is a prerequisite failure before any request',async()=>{
  const {verifyRunnerDistribution}=await import('../src/runner-distribution.mjs');
  await autoFixture(async f=>{
    f.dependencies.distributionVerifier=verifyRunnerDistribution;
    await rejected(f.register,/pinned official/);assert.equal(f.posts(),0);
  });
});
test('auto: CLI command output reports local success and remote unknown without secrets',async()=>{
  await autoFixture(async f=>{
    let out='',err='';const stdout=process.stdout.write,stderr=process.stderr.write;
    process.stdout.write=s=>{out+=s;return true;};process.stderr.write=s=>{err+=s;return true;};
    try{assert.equal(await main(['runner','register','--runner','demo','--repo','example/a','--method','auto'],f.dependencies),0);}
    finally{process.stdout.write=stdout;process.stderr.write=stderr;}
    assert.match(out,/configuration completed; remote unknown/);assert.match(out,/service active/);
    assert.doesNotMatch(out+err,/SECRET_SENTINEL|PRIVATE KEY|https:.*[?]sig=/);
  });
});

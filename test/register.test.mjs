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

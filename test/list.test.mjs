import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir,userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {listEnvironments,listOptions,observeService} from '../src/list.mjs';
import {withRegistrationCore} from '../src/core.mjs';
import {main} from '../src/cli.mjs';
const core=await withRegistrationCore(c=>c), account=userInfo(), secret='PRIVATE_SENTINEL';
const response=data=>({status:200,headers:{},body:JSON.stringify(data)});
const runner=(id='9007199254740993',patch={})=>({id,name:'fixture',os:'linux',status:'offline',busy:false,
  labels:['self-hosted','linux','nibfin-user-8'].map(name=>({name,type:name.startsWith('nibfin')?'custom':'read-only'})),...patch});
const page=(runners,total=runners.length)=>({runners,total_count:total});
async function fixture(consume) {
  const root=await fs.mkdtemp(join(tmpdir(),'list fixture ')), records=[], calls=[];
  try {
    for(const [n,agent] of [['zeta','gemini'],['alpha','codex'],['middle','claude']]) {
      const base=join(root,n),loginSpace=join(base,'login');await fs.mkdir(loginSpace,{recursive:true,mode:0o700});
      const record={schemaVersion:1,name:n,agent,githubId:'8',loginSpace,account:{uid:account.uid,username:account.username},instances:[],secret};
      for(let i=1;i<=2;i++) {
        const item={id:n+'-'+i,loginSpace,status:'unregistered',serviceStatus:'not-installed'};
        for(const leaf of ['workspace','registration','service']) {item[leaf]=join(base,'instance-'+i,leaf);await fs.mkdir(item[leaf],{recursive:true,mode:0o700});}
        record.instances.push(item);
      }
      await fs.writeFile(join(base,'environment.json'),JSON.stringify(record),{mode:0o600});
      await fs.writeFile(join(loginSpace,'private-login'),secret,{mode:0o000});records.push(record);
    }
    const binding=(record=records[1],i=0,patch={})=>({schemaVersion:1,instanceId:record.instances[i].id,memberId:'8',
      registration:{level:'repository',id:'51'},target:{id:'51',owner:'example',repository:'a',ownerId:'7',ownerType:'User'},
      runnerId:'9007199254740993',label:'nibfin-user-8',platform:'linux',disposition:'registered',status:'online',busy:true,
      observedAt:'2000-01-01T00:00:00.000Z',service:{account:account.username,unit:`nibfin-${record.name}-${record.instances[i].id}.service`},secret,...patch});
    const write=async (b,record=records[1],i=0)=>fs.writeFile(join(record.instances[i].registration,'binding.json'),JSON.stringify(b),{mode:0o600});
    const routes={'/repos/example/a':{id:'51',full_name:'example/a',visibility:'public',private:false,owner:{id:'7',type:'User'}},
      '/repos/example/a/actions/runners?per_page=100':page([runner()])};
    const deps={root,core,run:async (cmd,args,env)=>{assert.equal(env.GH_TOKEN,undefined);assert.equal(env.NODE_OPTIONS,undefined);return {ok:true,identity:'8'};},
      service:async(unit,env)=>{assert.equal(env.GITHUB_TOKEN,undefined);return {state:'active',reason:null};},
      transport:async r=>{calls.push(r);assert.equal(r.method,'GET');const data=routes[r.endpoint];if(!data)throw Error(secret);return typeof data==='function'?data(r):response(data);}};
    await consume({root,records,binding,write,routes,deps,calls,list:(args=[])=>listEnvironments(args,deps)});
  } finally {await fs.rm(root,{recursive:true,force:true});}
}
async function output(args,deps) {
  let stdout='',stderr='';const out=process.stdout.write,err=process.stderr.write;
  process.stdout.write=s=>{stdout+=s;return true;};process.stderr.write=s=>{stderr+=s;return true;};
  let code;try {code=await main(['runner','list',...args],deps);}finally{process.stdout.write=out;process.stderr.write=err;}
  assert.ok(!(stdout+stderr).includes(secret));assert.equal(stderr,'');return {stdout,code};
}
const first=r=>r.environments.find(e=>e.name==='alpha').instances[0];
test('AC1/2 actual text and single-object JSON, sorted environments and named filters',()=>fixture(async f=>{
  await f.write(f.binding());
  for(const args of [[],['--runner','alpha'],['--json'],['--runner','alpha','--json']]) {
    const o=await output(args,f.deps);assert.equal(o.code,0);
    if(args.includes('--json')) {const r=JSON.parse(o.stdout);assert.equal(r.schemaVersion,1);assert.equal(first(r).registration.runnerId,'9007199254740993');
      assert.deepEqual(first(r).registration.registration,{level:'repository',id:'51'});
      assert.equal(first(r).local.state,'active');assert.equal(first(r).remote.status,'offline');assert.equal(first(r).remote.busy,false);
      assert.deepEqual(r.environments.map(e=>e.name),args.includes('--runner')?['alpha']:['alpha','middle','zeta']);
      if(!args.includes('--runner'))assert.deepEqual(r.environments.map(e=>e.agent),['codex','claude','gemini']);
    } else {assert.match(o.stdout,/alpha: codex/);assert.match(o.stdout,/9007199254740993/);assert.match(o.stdout,/offline/);}
  }
  const o=await output(['--runner','absent','--json'],f.deps);assert.equal(o.code,2);assert.equal(JSON.parse(o.stdout).complete,false);
}));
test('AC1 rejects malformed arguments before touching dependencies; empty storage',async()=>{
  for(const args of [['--json','--json'],['--runner'],['--runner','--json'],['--runner','a','--runner','b'],['--other'],['extra'],['--runner','../x']]) {
    assert.throws(()=>listOptions(args));const r=await listEnvironments(args,new Proxy({},{get(){assert.fail('No I/O');}}));assert.equal(r.complete,false);
    assert.equal((await output([...args,'--json'],{})).code,2);
  }
  const r=await listEnvironments([],{root:'/tmp/nonexistent-nibfin-list-792845'});assert.deepEqual(r.environments,[]);assert.equal(r.complete,true);
});
test('AC3 fresh service and remote states are independent; stored remote never confirms Online',()=>fixture(async f=>{
  await f.write(f.binding());f.routes['/repos/example/a/actions/runners?per_page=100']=page([runner(undefined,{status:'online',busy:true})]);
  f.deps.service=async()=>({state:'inactive',reason:null});let r=await f.list();assert.equal(first(r).local.state,'inactive');assert.equal(first(r).remote.status,'online');assert.equal(first(r).remote.busy,true);
  f.deps.run=async()=>({ok:false});r=await f.list();assert.equal(first(r).remote.status,null);assert.equal(first(r).remote.busy,null);assert.equal(r.complete,true);
  f.deps.service=async()=>({state:'active',reason:null});r=await f.list();assert.equal(first(r).local.state,'active');assert.equal(first(r).remote.state,'unknown');
}));
test('AC4 pinned Core GET fixtures: pages, absent/replaced IDs, labels, malformed and denied',()=>fixture(async f=>{
  await f.write(f.binding());
  const endpoint='/repos/example/a/actions/runners?per_page=100';
  for(const data of [page([]),page([runner('2')]),page([runner(undefined,{labels:[{name:'nibfin-user-9'}]})]),page([runner(undefined,{busy:null})]),page([runner()],2)]) {
    f.routes[endpoint]=data;const r=await f.list();assert.equal(first(r).remote.status,null);assert.equal(first(r).remote.busy,null);assert.equal(r.complete,true);
  }
  for(const status of [401,403,404,500]) {f.routes[endpoint]=()=>({status,headers:{},body:secret});assert.equal(first(await f.list()).remote.state,'unknown');}
  f.routes[endpoint]=()=>{throw Error(secret);};assert.equal(first(await f.list()).remote.status,null);
  f.routes[endpoint]=()=>({status:200,headers:{link:'<https://api.github.com/repos/example/a/actions/runners?per_page=100&page=2>; rel="next"'},body:JSON.stringify(page([runner()],2))});
  f.routes[endpoint+'&page=2']=page([runner('92',{labels:[{name:'self-hosted',type:'read-only'},{name:'linux',type:'read-only'},{name:'nibfin-user-9',type:'custom'}]})],2);
  assert.equal(first(await f.list()).remote.status,'offline');
  f.routes['/repos/example/a'].owner.type='Organization';await f.write(f.binding(undefined,0,{target:{id:'51',owner:'example',repository:'a',ownerId:'7',ownerType:'Organization'}}));
  assert.equal(first(await f.list()).remote.status,'offline');
}));
test('AC4 organization registration and group membership; mismatch stays unknown',()=>fixture(async f=>{
  await f.write(f.binding(undefined,0,{registration:{level:'organization',organization:'example',id:'7'},target:{id:'7',owner:'example'},groupId:'3'}));
  Object.assign(f.routes,{'/orgs/example':{id:'7',login:'example',type:'Organization'},
    '/orgs/example/actions/runners?per_page=100&page=1':page([runner()]),
    '/orgs/example/actions/runner-groups?per_page=100&page=1':{runner_groups:[{id:'3',name:'build'}],total_count:1},
    '/orgs/example/actions/runner-groups/3/runners?per_page=100&page=1':page([runner()])});
  const listed=first(await f.list());
  assert.equal(listed.remote.status,'offline');assert.deepEqual(listed.registration.registration,{level:'organization',id:'7',organization:'example'});
  f.routes['/orgs/example/actions/runner-groups/3/runners?per_page=100&page=1']=page([]);
  assert.equal(first(await f.list()).remote.status,null);
}));
test('AC5 corrupt/pending/conflicting metadata and incomplete creation never report complete',()=>fixture(async f=>{
  for(const disposition of ['pending','unknown']) {await f.write(f.binding(undefined,0,{disposition}));const r=await f.list();assert.equal(first(r).registration.disposition,disposition);assert.equal(first(r).remote.status,null);}
  const path=join(f.records[1].instances[0].registration,'binding.json');
  for(const value of ['null','false','0','[]','{}']) {await fs.writeFile(path,value);assert.equal((await f.list()).complete,false);}
  await fs.writeFile(path,'{');assert.equal((await f.list()).complete,false);
  await fs.unlink(path);f.records[1].instances[0].status='registered';await fs.writeFile(join(f.root,'alpha','environment.json'),JSON.stringify(f.records[1]));assert.equal((await f.list()).complete,false);
  await fs.mkdir(join(f.root,'unfinished'),{mode:0o700});assert.equal((await f.list()).complete,false);
}));
test('AC6 strict paths, unit/account validation, redaction and unchanged bytes',()=>fixture(async f=>{
  const b=f.binding();await f.write(b);const path=join(f.root,'alpha','environment.json'),original=await fs.readFile(path),bindingPath=join(f.records[1].instances[0].registration,'binding.json'),bytes=await fs.readFile(bindingPath);
  process.env.GH_TOKEN=secret;process.env.GITHUB_TOKEN=secret;process.env.NODE_OPTIONS=secret;
  try {await output(['--json'],f.deps);} finally{delete process.env.GH_TOKEN;delete process.env.GITHUB_TOKEN;delete process.env.NODE_OPTIONS;}
  assert.deepEqual(await fs.readFile(path),original);assert.deepEqual(await fs.readFile(bindingPath),bytes);
  await f.write({...b,service:{...b.service,unit:'$(false)'}});assert.equal((await f.list()).complete,false);
  await f.write({...b,service:{...b.service,account:'different'}});assert.equal(first(await f.list()).local.reason,'account_mismatch');
  await fs.unlink(bindingPath);await fs.symlink('/etc/passwd',bindingPath);assert.equal((await f.list()).complete,false);
}));
test('AC3 executable service fixtures: active/inactive/not found/timeout/missing, without output disclosure',async()=>{
  const root=await fs.mkdtemp(join(tmpdir(),'service fixture '));
  try {
    const tool=join(root,'systemctl'),env={PATH:root+':'+process.env.PATH,LANG:'C'};
    for(const [load,state,expected] of [['loaded','active','active'],['loaded','inactive','inactive'],['not-found','inactive','not-installed']]) {
      await fs.writeFile(tool,`#!/bin/sh\necho LoadState=${load}\necho ActiveState=${state}\necho ${secret} >&2\n`,{mode:0o700});
      assert.equal(observeService('fixture.service',env).state,expected);
    }
    await fs.writeFile(tool,'#!/bin/sh\nexec sleep 3\n',{mode:0o700});assert.equal(observeService('fixture.service',env,20).reason,'timeout');
    assert.equal(observeService('fixture.service',{PATH:'/nonexistent'},20).state,'unknown');
  }finally{await fs.rm(root,{recursive:true,force:true});}
});
test('AC4/6 real pinned gh process transport and actual CLI child output stay read-only and secret-free',()=>fixture(async f=>{
  await f.write(f.binding());
  const bin=join(f.root,'tools');await fs.mkdir(bin,{mode:0o700});
  // The extension's dependency seam is trusted test configuration, not a public flag.
  const script=join(bin,'gh');
  const routes=f.routes;
  await fs.writeFile(script,`#!${process.execPath}
const args=process.argv.slice(2);
if(process.env.GH_TOKEN || process.env.GITHUB_TOKEN || process.env.NODE_OPTIONS || process.env.OPENAI_API_KEY)process.exit(9);
console.error('${secret}');
if(args[0]==='--version' || args[0]==='auth')process.exit(0);
if(args[0]!=='api')process.exit(8);
if(args.includes('--jq')){console.log('8');process.exit(0);}
if(args[args.indexOf('--method')+1]!=='GET')process.exit(7);
const data=${JSON.stringify(routes)}[args.at(-1).replace('https://api.github.com','')];
if(!data)process.exit(6);
console.log('HTTP/2 200 OK\\nContent-Type: application/json\\n\\n'+JSON.stringify(data));
`,{mode:0o700});
  const bridge=join(f.root,'command.mjs');
  await fs.writeFile(bridge,`import {main} from ${JSON.stringify(new URL('../src/cli.mjs',import.meta.url).href)};\nprocess.exitCode=await main(process.argv.slice(2),{root:${JSON.stringify(f.root)},core:undefined});\n`);
  // Put executable state outside the enumerated storage by explicitly selecting alpha.
  const command=spawnSync(process.execPath,[bridge,'runner','list','--runner','alpha','--json'],{
    env:{PATH:bin+':'+process.env.PATH,HOME:'/nonexistent',GH_TOKEN:secret,GITHUB_TOKEN:secret,OPENAI_API_KEY:secret},encoding:'utf8',timeout:15000});
  assert.ifError(command.error);assert.equal(command.status,0,command.stderr);assert.equal(command.stderr,'');
  assert.ok(!(command.stdout+command.stderr).includes(secret));const r=JSON.parse(command.stdout);
  assert.equal(first(r).remote.status,'offline');assert.equal(first(r).remote.busy,false);
}));
test('AC2/4 multiple registrations are independently observed, including exact numeric wire IDs',()=>fixture(async f=>{
  await f.write(f.binding());await f.write(f.binding(undefined,1,{runnerId:'92'}),f.records[1],1);
  f.routes['/repos/example/a/actions/runners?per_page=100']=page([runner(),runner('92',{status:'online',busy:false})]);
  const r=await f.list(['--runner','alpha']);assert.deepEqual(r.environments[0].instances.map(i=>i.remote.status),['offline','online']);
  f.routes['/repos/example/a/actions/runners?per_page=100']=()=>({status:200,headers:{},body:JSON.stringify(page([runner(),runner('92')])).replace('"9007199254740993"','9007199254740993')});
  assert.equal(first(await f.list()).remote.status,'offline');
  f.routes['/repos/example/a/actions/runners?per_page=100']=page([runner()]);
  const partial=await f.list();assert.equal(first(partial).remote.status,'offline');assert.equal(partial.environments[0].instances[1].remote.status,null);
  assert.ok(partial.environments[0].instances[1].remote.diagnostics.some(d=>d.code==='missing_or_replaced_id'));
}));
test('AC5 bounded timeout, wrong member/scope and unsupported service contexts remain explicit',()=>fixture(async f=>{
  await f.write(f.binding());f.deps.remoteTimeoutMs=20;f.deps.transport=()=>new Promise(resolve=>setTimeout(()=>resolve(response({})),60));
  assert.equal(first(await f.list()).remote.status,null);
  f.deps.transport=async r=>response(f.routes[r.endpoint]);f.deps.run=async()=>({ok:true,identity:'9'});
  assert.equal(first(await f.list()).remote.reason,'member_mismatch');
  f.deps.run=async()=>({ok:true,identity:'8'});f.routes['/repos/example/a'].id='52';assert.equal(first(await f.list()).remote.state,'unknown');
  await f.write(f.binding(undefined,0,{platform:'windows'}));assert.equal(first(await f.list()).local.reason,'unsupported_service');
}));
test('AC5/6 duplicate identities, unreadable metadata and path escapes fail local enumeration',()=>fixture(async f=>{
  const r=f.records[1],path=join(f.root,'alpha','environment.json');
  r.instances[1].id=r.instances[0].id;await fs.writeFile(path,JSON.stringify(r));assert.equal((await f.list()).complete,false);
  r.instances[1].id='alpha-2';r.instances[0].workspace='/etc';await fs.writeFile(path,JSON.stringify(r));assert.equal((await f.list()).complete,false);
  await fs.chmod(path,0o000);assert.equal((await f.list()).complete,false);await fs.chmod(path,0o600);
  await fs.writeFile(path,'{');assert.equal((await f.list()).complete,false);
}));
test('AC5/6 registration-level output allowlists reject inapplicable private metadata in both CLI formats',()=>fixture(async f=>{
  const path=join(f.records[1].instances[0].registration,'binding.json');
  for(const organization of [secret,{private:secret},[secret],null,false,0,'']) {
    await f.write(f.binding(undefined,0,{registration:{level:'repository',id:'51',organization}}));
    const original=await fs.readFile(path);
    let probes=0,authentications=0;
    f.deps.service=async()=>{probes++;return {state:'active',reason:null};};
    f.deps.run=async()=>{authentications++;return {ok:true,identity:'8'};};
    for(const args of [['--runner','alpha'],['--runner','alpha','--json']]) {
      const o=await output(args,f.deps);assert.equal(o.code,2);
      if(args.includes('--json')) {
        const r=JSON.parse(o.stdout);assert.equal(r.complete,false);
        assert.deepEqual(first(r).registration,{disposition:'unknown'});
        assert.equal(first(r).remote.status,null);assert.equal(first(r).remote.busy,null);
        assert.ok(r.diagnostics.some(d=>d.code==='invalid_instance_state'));
      } else {assert.match(o.stdout,/incomplete/);assert.match(o.stdout,/invalid_instance_state/);}
    }
    assert.equal(probes,0);assert.equal(authentications,0);
    assert.deepEqual(await fs.readFile(path),original);
  }
  for(const level of ['repository','organization']) {
    const registration=level==='repository' ? {level,id:'51',unexpected:{private:secret}} : {level,id:'7',organization:'example',unexpected:{private:secret}};
    await f.write(f.binding(undefined,0,{registration,target:level==='repository' ? f.binding().target : {id:'7',owner:'example'}}));
    for(const args of [[],['--json']])assert.equal((await output(args,f.deps)).code,2);
  }
}));

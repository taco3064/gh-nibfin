import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir,userInfo} from 'node:os';
import {withRegistrationCore} from '../src/core.mjs';
import {unregisterInstance,unregisterOptions,workObservation,terminationObservation} from '../src/unregister.mjs';
import {registerInstance,registrationProcess} from '../src/register.mjs';
import {main} from '../src/cli.mjs';
const core=await withRegistrationCore(c=>c), secret='UNREGISTER_SECRET_SENTINEL';
const response=data=>({status:200,headers:{},body:JSON.stringify(data)});
async function fixture(run) {
  const root=await fs.mkdtemp(join(tmpdir(),'unregister fixture '));
  try {
    const account=userInfo(), base=join(root,'demo'),loginSpace=join(base,'login');
    await fs.mkdir(loginSpace,{recursive:true,mode:0o700});
    const record={schemaVersion:1,name:'demo',agent:'codex',githubId:'8',account:{uid:account.uid,username:account.username},loginSpace,instances:[]};
    const bindings=[];
    for(let n=1;n<=3;n++) {
      const i={id:'local-'+n,loginSpace,status:'unregistered',serviceStatus:'not-installed'};
      for(const leaf of ['registration','service','workspace']) {i[leaf]=join(base,'instance-'+n,leaf);await fs.mkdir(i[leaf],{recursive:true,mode:0o700});}
      record.instances.push(i);
      const b={schemaVersion:1,instanceId:i.id,memberId:'8',runnerId:n===1?'9007199254740993':String(90+n),label:'nibfin-user-8',platform:'linux',disposition:'registered',status:'offline',busy:false,
        registration:{level:'repository',id:'51'},target:{id:'51',owner:'example',repository:n===3?'b':'a',ownerId:'7',ownerType:'User'},
        runnerName:'nibfin-demo-'+n,service:{account:account.username,unit:`nibfin-demo-${i.id}.service`,status:'active'}};
      if(n===3) {b.registration.id='52';b.target.id='52';}
      bindings.push(b);
      await fs.writeFile(join(i.registration,'binding.json'),JSON.stringify(b),{mode:0o600});
      for(const leaf of ['.runner','.runner_migrated','.credentials','.credentials_migrated','.credentials_rsaparams','.credential_store','.service','runsvc.sh']) await fs.writeFile(join(i.registration,leaf),secret,{mode:0o600});
      await fs.writeFile(join(i.registration,'config.sh'),'prepared application',{mode:0o700});
      await fs.writeFile(join(i.service,b.service.unit),`[Unit]\nDescription=Nibfin instance runner\n[Service]\nWorkingDirectory="${i.registration}"\nExecStart="${i.registration}/runsvc.sh"\nRestart=on-failure\n[Install]\nWantedBy=default.target\n`,{mode:0o600});
    }
    await fs.writeFile(join(loginSpace,'agent-login'),secret,{mode:0o600});
    const descriptor=JSON.stringify(record);await fs.writeFile(join(base,'environment.json'),descriptor,{mode:0o600});
    let present=true, busy=false, stopped=false, disabled=false;
    const calls=[],processes=[],selected=record.instances[0];
    const f={root,record,bindings,calls,processes,
      get present(){return present;},set busy(v){busy=v;},
      dependencies:{core,root,run:async()=>({ok:true,identity:'8'}),transport:async r=>{
        calls.push(r);assert.equal(r.identity,'local-stored-login');
        if(r.method==='DELETE') {present=false;return {status:204,headers:{},body:''};}
        if(r.endpoint.includes('/actions/runners?')) return response({total_count:present?1:0,runners:present?[{id:bindings[0].runnerId,name:bindings[0].runnerName,os:'linux',status:'offline',busy,labels:[{name:'nibfin-user-8'}]}]:[]});
        return response(bindings[0].registration.level==='organization'?{id:'7',login:'example',type:'Organization'}:
          {id:'51',full_name:'example/a',private:false,visibility:'public',owner:{id:'7',type:bindings[0].target.ownerType}});
      },process:async(cmd,args,options)=>{
        assert.equal(options.env.GH_TOKEN,undefined);assert.equal(options.env.GITHUB_TOKEN,undefined);
        processes.push(args);if(args.includes('stop')) stopped=true;if(args.includes('disable')) disabled=true;return {ok:true};
      },termination:()=>({LoadState:'loaded',FragmentPath:join(selected.service,bindings[0].service.unit),ActiveState:stopped?'inactive':'active',MainPID:stopped?'0':'123',TasksCurrent:stopped?'0':'2',UnitFileState:disabled?'disabled':'enabled',DropInPaths:'',KillMode:'control-group',NeedDaemonReload:'no',TriggeredBy:'',PropagatesStopTo:''})},
      unregister:(extra=['--instance','local-1'])=>unregisterInstance(['--runner','demo',...(bindings[0].registration.level==='organization'?['--org','example']:['--repo','example/a']),...extra],f.dependencies),
      state:async()=>JSON.parse(await fs.readFile(join(selected.registration,'unregister.json'))),
      writeBinding:async()=>fs.writeFile(join(selected.registration,'binding.json'),JSON.stringify(bindings[0]),{mode:0o600})};
    const neighbors=await Promise.all(record.instances.slice(1).map(i=>fs.readFile(join(i.registration,'binding.json'))));
    await run(f);
    assert.equal(await fs.readFile(join(base,'environment.json'),'utf8'),descriptor);
    assert.equal(await fs.readFile(join(loginSpace,'agent-login'),'utf8'),secret);
    for(const i of record.instances) assert.equal(await fs.readFile(join(i.registration,'config.sh'),'utf8'),'prepared application');
    for(const [n,i] of record.instances.slice(1).entries()) {
      assert.deepEqual(await fs.readFile(join(i.registration,'binding.json')),neighbors[n]);
      assert.equal(await fs.readFile(join(i.registration,'.credentials'),'utf8'),secret);
    }
  } finally {await fs.rm(root,{recursive:true,force:true});}
}
test('AC1 strict options, coordinates, ambiguity and wrong selection never mutate',async()=>{
  for(const args of [[],['--runner','demo'],['--runner','demo','--org','x','--repo','a/b'],['--runner','demo','--org','x','--org','x'],['--runner','demo','--org','x','--bad','x'],['--runner','demo','--org','x','--instance','../x']]) assert.throws(()=>unregisterOptions(args));
  await fixture(async f=>{
    for(const extra of [[],['--instance','missing']]) await assert.rejects(()=>f.unregister(extra));
    for(const target of ['a/b/c','../x','a/..','-a/b']) await assert.rejects(()=>unregisterInstance(['--runner','demo','--repo',target],f.dependencies));
    assert.equal(f.calls.length,0);assert.equal(f.processes.length,0);
    await fs.rm(join(f.record.instances[1].registration,'binding.json'));
    // Unique target selection is allowed without an instance.
    assert.equal((await f.unregister([])).complete,true);
    await fs.writeFile(join(f.record.instances[1].registration,'binding.json'),JSON.stringify(f.bindings[1]));
  });
});
test('AC2/5/7 exact official removal, large IDs, preservation, repeat and safe reuse reservation',()=>fixture(async f=>{
  process.env.GH_TOKEN=secret;process.env.GITHUB_TOKEN=secret;
  try {
    const r=await f.unregister();assert.equal(r.complete,true);assert.equal(r.remote,'absent');assert.equal(r.local,'detached');
    assert.equal((await f.unregister()).already,true);assert.equal((await f.state()).complete,true);
    assert.deepEqual(f.calls.filter(r=>r.method==='DELETE').map(r=>r.endpoint),['/repos/example/a/actions/runners/9007199254740993']);
    assert.equal(await fs.readFile(join(f.record.instances[0].registration,'config.sh'),'utf8'),'prepared application');
    assert.equal((await fs.readdir(f.record.instances[0].registration)).includes('.credentials'),false);
    assert.ok(f.processes.every(a=>!a.some(v=>v.includes('local-2') || v.includes('local-3'))));
    // Registration reaches the prepared-application check, rather than residual or capacity rejection.
    await assert.rejects(()=>registerInstance(['--runner','demo','--repo','example/a'],{...f.dependencies,run:async()=>({ok:true,identity:'8'})}),/application/);
  } finally {delete process.env.GH_TOKEN;delete process.env.GITHUB_TOKEN;}
}));
test('AC2/6 Org scope distinct from Org-owned repository; repo selector never stops shared Org service',async()=>{
  for(const org of [true,false]) await fixture(async f=>{
    f.bindings[0].target.ownerType='Organization';
    if(org) {f.bindings[0].registration={level:'organization',id:'7',organization:'example'};f.bindings[0].target={id:'7',owner:'example'};}
    await f.writeBinding();
    if(org) {await assert.rejects(()=>unregisterInstance(['--runner','demo','--repo','example/a','--instance','local-1'],f.dependencies));assert.equal(f.processes.length,0);}
    assert.equal((await f.unregister()).complete,true);
    assert.equal(f.calls.find(r=>r.method==='DELETE').endpoint,org?'/orgs/example/actions/runners/9007199254740993':'/repos/example/a/actions/runners/9007199254740993');
  });
});
test('AC2/4 read failures allow safe local exit but cannot prove absence or permit reuse',async()=>{
  for(const status of [401,403,404,500,'transport','malformed','incomplete','mismatch']) await fixture(async f=>{
    const transport=f.dependencies.transport;
    f.dependencies.transport=async r=>{
      if(status==='transport') throw Error(secret);
      if(status==='malformed') return {status:200,headers:{},body:secret};
      if(status==='incomplete' && r.endpoint.includes('/actions/runners?')) return response({total_count:2,runners:[]});
      if(status==='mismatch' && !r.endpoint.includes('/actions/')) return response({id:'99',full_name:'elsewhere/a'});
      return typeof status==='number'?{status,headers:{},body:secret}:transport(r);
    };
    const r=await f.unregister();assert.equal(r.complete,false);assert.equal(r.remote,'unknown');assert.equal(r.service,'terminated');
    assert.equal(f.calls.filter(c=>c.method==='DELETE').length,0);
    assert.ok(!JSON.stringify(await f.state()).includes(secret));
    assert.ok(await fs.readFile(join(f.record.instances[0].registration,'binding.json')));
    await assert.rejects(()=>registerInstance(['--runner','demo','--repo','example/a'],{...f.dependencies,run:async()=>({ok:true,identity:'8'})}),/reconciliation/);
  });
});
test('AC3 termination acknowledgement, unsafe units, active work and unknown writes retain credentials',async()=>{
  for(const mode of ['stop-failure','running','missing-manager','cancelled','busy','work','write','unit']) await fixture(async f=>{
    if(mode==='stop-failure'||mode==='cancelled') f.dependencies.process=async()=>({ok:false,reason:mode});
    if(mode==='running') {const original=f.dependencies.termination;f.dependencies.termination=()=>({...original(),TasksCurrent:'1'});}
    if(mode==='missing-manager') f.dependencies.termination=()=>({});
    if(mode==='unit') {const original=f.dependencies.termination;f.dependencies.termination=()=>({...original(),FragmentPath:'/elsewhere'});}
    if(mode==='busy') f.busy=true;
    if(mode==='work') await fs.writeFile(join(f.record.instances[0].workspace,'active'),'unknown');
    if(mode==='write') await fs.writeFile(join(f.record.instances[0].service,'write-intent.json'),'{}');
    const r=await f.unregister();assert.equal(r.complete,false);assert.equal(r.local,'retained');
    assert.equal(await fs.readFile(join(f.record.instances[0].registration,'.credentials'),'utf8'),secret);
    assert.equal(f.calls.filter(c=>c.method==='DELETE').length,0);
  });
});
test('AC4 uncertain DELETE is durably read back on reentry, never blindly replayed',async()=>{
  for(const lost of [true,false]) await fixture(async f=>{
    const transport=f.dependencies.transport;let deletes=0;
    f.dependencies.transport=async r=>{
      if(r.method==='DELETE') {deletes++;if(lost) await transport(r);throw Error(secret);}
      return transport(r);
    };
    const first=await f.unregister();assert.equal(first.complete,lost);
    if(!lost) {
      assert.equal(first.remote,'present');assert.equal(first.local,'detached');
      // Model the real user-manager state after source cleanup.
      f.dependencies.termination=()=>({LoadState:'not-found',ActiveState:'inactive',MainPID:'0'});
      const again=await f.unregister();assert.equal(again.complete,false);assert.equal(again.remote,'present');
    }
    assert.equal(deletes,1);
  });
});
test('AC4 lock contention, publication and unsafe cleanup failures do not claim completion',async()=>{
  for(const mode of ['lock','publish','cleanup','reload']) await fixture(async f=>{
    const i=f.record.instances[0];
    if(mode==='lock') await fs.mkdir(join(i.service,'registration.lock'));
    if(mode==='publish') f.dependencies.publish=async()=>{throw Error(secret);};
    if(mode==='cleanup') {await fs.rm(join(i.registration,'.credentials'));await fs.symlink('/nonexistent',join(i.registration,'.credentials'));}
    if(mode==='reload') {const p=f.dependencies.process;f.dependencies.process=(c,a,o)=>a.includes('daemon-reload')?{ok:false}:p(c,a,o);}
    const r=await f.unregister();assert.equal(r.complete,false);
    if(['lock','publish'].includes(mode)) {assert.equal(f.calls.length,0);assert.equal(f.processes.length,0);}
    assert.ok(!JSON.stringify(r).includes(secret));
  });
});
test('AC7 CLI emits only safe separate dispositions and nonzero for incomplete results',()=>fixture(async f=>{
  const out=[],err=[],stdout=process.stdout.write,stderr=process.stderr.write;
  process.stdout.write=s=>{out.push(s);return true;};process.stderr.write=s=>{err.push(s);return true;};
  try {
    const process=f.dependencies.process;f.dependencies.process=async()=>({ok:false});
    assert.equal(await main(['runner','unregister','--runner','demo','--repo','example/a','--instance','local-1'],f.dependencies),2);
    f.dependencies.process=process;
    assert.equal(await main(['runner','unregister','--runner','demo','--repo','example/a','--instance','local-1'],f.dependencies),0);
    assert.equal(await main(['runner','unregister','--bad',secret],f.dependencies),2);
    assert.match(out.join(''),/local=detached; service=terminated; remote=absent/);
    assert.ok(!(out.join('')+err.join('')).includes(secret));
  } finally {process.stdout.write=stdout;process.stderr.write=stderr;}
}));
test('AC3 production observations reject unavailable manager and Worker history',()=>fixture(async f=>{
  const i=f.record.instances[0];assert.equal(await workObservation(i),'idle');
  await fs.mkdir(join(i.registration,'_diag'),{mode:0o700});await fs.writeFile(join(i.registration,'_diag','Worker_run.log'),'private');
  assert.equal(await workObservation(i),'unknown');
  assert.deepEqual(terminationObservation('nonexistent.service',{PATH:'/nonexistent'},100),{});
}));
test('AC3 real process timeout/cancellation suppress output',async()=>{
  for(const cancelled of [false,true]) {
    const c=new AbortController();if(cancelled)c.abort();
    const r=await registrationProcess(process.execPath,['-e',`console.log('${secret}');setInterval(()=>{},1000)`],{env:{PATH:process.env.PATH},timeoutMs:100,signal:c.signal});
    assert.equal(r.ok,false);assert.equal(r.reason,cancelled?'cancelled':'timeout');assert.ok(!JSON.stringify(r).includes(secret));
  }
});
test('AC2/4 submitted, denied, server, malformed and cancelled writes never imply absence; recovery reads first',async()=>{
  for(const mode of ['submitted',403,500,'malformed','cancelled']) await fixture(async f=>{
    const transport=f.dependencies.transport;let deletes=0;
    const controller=new AbortController();f.dependencies.signal=controller.signal;
    f.dependencies.transport=async r=>{
      if(r.method==='DELETE') {deletes++;
        if(mode==='cancelled') controller.abort();
        return {status:typeof mode==='number'?mode:mode==='submitted'?204:200,headers:{},body:mode==='submitted'?'':secret};
      }
      return transport(r);
    };
    const r=await f.unregister();assert.equal(r.complete,false);assert.equal(r.attempted,true);assert.equal(deletes,1);
    assert.notEqual(r.remote,'absent');assert.equal(r.local,'detached');assert.equal((await f.state()).identity.runnerId,'9007199254740993');
    delete f.dependencies.signal;
    f.dependencies.termination=()=>({LoadState:'not-found',ActiveState:'inactive',MainPID:'0'});
    const reads=[];
    f.dependencies.transport=async request=>{
      reads.push(request.method);assert.notEqual(request.method,'DELETE');
      return request.endpoint.includes('/actions/runners?')?response({total_count:0,runners:[]}):transport(request);
    };
    const recovered=await f.unregister();assert.equal(recovered.complete,true);assert.ok(reads.length>=2);assert.equal(deletes,1);
  });
});
test('AC2 malformed runner identity and inaccessible readback cannot authorize deletion',async()=>{
  for(const change of [r=>({...r,name:'replaced'}),r=>({...r,busy:undefined}),r=>({...r,labels:[]}),r=>({...r,os:'windows'})]) await fixture(async f=>{
    const transport=f.dependencies.transport;
    f.dependencies.transport=async request=>{
      const r=await transport(request);
      if(request.endpoint.includes('/actions/runners?')) {const d=JSON.parse(r.body);d.runners=d.runners.map(change);return response(d);}
      return r;
    };
    assert.equal((await f.unregister()).complete,false);assert.equal(f.calls.filter(r=>r.method==='DELETE').length,0);
    assert.equal(await fs.readFile(join(f.record.instances[0].registration,'.credentials'),'utf8'),secret);
  });
});
test('AC5 production register reuses the cleaned existing instance without increasing capacity',()=>fixture(async f=>{
  assert.equal((await f.unregister()).complete,true);
  const i=f.record.instances[0];await fs.mkdir(join(i.registration,'bin'),{mode:0o700});
  await fs.writeFile(join(i.registration,'bin/runsvc.sh'),'#!/bin/sh\nexit 0\n',{mode:0o700});
  const runner=b=>({id:b.runnerId,name:b.runnerName,os:'linux',status:'offline',busy:false,
    labels:['self-hosted','linux','nibfin-user-8'].map(name=>({name,type:name.startsWith('nibfin')?'custom':'read-only'}))});
  const d={...f.dependencies,run:async()=>({ok:true,identity:'8'}),transport:async request=>{
    if(request.endpoint.endsWith('/registration-token')) return response({token:'fixture_token',expires_at:new Date(Date.now()+60000).toISOString()});
    if(request.endpoint.includes('/actions/runners?')) return response({total_count:2,runners:[runner({...f.bindings[0],runnerId:'100'}),runner(f.bindings[1])]});
    return response({id:'51',full_name:'example/a',private:false,visibility:'public',owner:{id:'7',type:'User'}});
  },process:async(command,args)=>{
    if(command.endsWith('/config.sh')) {
      await fs.writeFile(join(i.registration,'.runner'),JSON.stringify({agentId:'100',agentName:args[args.indexOf('--name')+1],gitHubUrl:'https://github.com/example/a',workFolder:i.workspace}),{mode:0o600});
      await fs.writeFile(join(i.registration,'.credentials'),'fixture-new',{mode:0o600});
    }
    return {ok:true};
  }};
  const r=await registerInstance(['--runner','demo','--repo','example/a'],d);
  assert.equal(r.instanceId,i.id);assert.equal(r.runnerId,'100');assert.equal(f.record.instances.length,3);
  assert.equal(JSON.parse(await fs.readFile(join(i.registration,'binding.json'))).disposition,'registered');
}));
test('AC4 simultaneous unregister and in-flight registration share local exclusion',()=>fixture(async f=>{
  const original=f.dependencies.process;let checked=false;
  f.dependencies.process=async(...args)=>{
    if(!checked) {
      checked=true;
      await assert.rejects(()=>registerInstance(['--runner','demo','--repo','example/a'],{...f.dependencies,run:async()=>({ok:true,identity:'8'})}),/reconciliation/);
      const competing=await f.unregister();assert.equal(competing.complete,false);assert.equal(competing.local,'retained');
    }
    return original(...args);
  };
  assert.equal((await f.unregister()).complete,true);assert.equal(f.calls.filter(r=>r.method==='DELETE').length,1);
}));
test('AC2/3 large numeric API ID and systemd empty-cgroup observation remain exact',()=>fixture(async f=>{
  const transport=f.dependencies.transport;
  f.dependencies.transport=async r=>{const response=await transport(r);return {...response,body:response.body.replace('"9007199254740993"','9007199254740993')};};
  const observe=f.dependencies.termination;
  f.dependencies.termination=()=>({...observe(),TasksCurrent:'[not set]',ControlGroup:''});
  assert.equal((await f.unregister()).complete,true);assert.equal(f.calls.find(r=>r.method==='DELETE').endpoint,'/repos/example/a/actions/runners/9007199254740993');
}));
test('AC3 service source and manager dependencies cannot widen stop/disable effects',async()=>{
  for(const mode of ['Also','dropin','trigger','reload','killmode']) await fixture(async f=>{
    if(mode==='Also') await fs.appendFile(join(f.record.instances[0].service,f.bindings[0].service.unit),'Also=neighbor.service\n');
    else {
      const original=f.dependencies.termination;
      f.dependencies.termination=()=>({...original(),...(mode==='dropin'?{DropInPaths:'/foreign.conf'}:
        mode==='trigger'?{TriggeredBy:'neighbor.timer'}:mode==='reload'?{NeedDaemonReload:'yes'}:{KillMode:'process'})});
    }
    const r=await f.unregister();assert.equal(r.complete,false);assert.equal(f.processes.length,0);assert.equal(f.calls.length,0);
  });
});
test('AC4 final state publication failure and interrupted pending state retain reconciliation evidence',async()=>{
  for(const pending of [false,true]) await fixture(async f=>{
    const i=f.record.instances[0],path=join(i.registration,'unregister.json');
    if(pending) await fs.writeFile(path+'.pending','interrupted',{mode:0o600});
    else f.dependencies.publish=async(p,value)=>{
      if(value.complete) throw Error(secret);
      await fs.writeFile(p,JSON.stringify(value),{mode:0o600});
    };
    const r=await f.unregister();assert.equal(r.complete,false);
    assert.equal(JSON.parse(await fs.readFile(join(i.registration,'binding.json'))).runnerId,'9007199254740993');
    if(pending) {assert.equal(f.processes.length,0);assert.equal(f.calls.length,0);}
    else {assert.equal(r.remote,'absent');assert.equal((await f.state()).complete,false);}
  });
});
test('AC1 live bindings take precedence over completed historical unregister records',()=>fixture(async f=>{
  assert.equal((await f.unregister()).complete,true);
  const second=f.record.instances[1], original=await fs.readFile(join(second.registration,'binding.json'));
  const r=await f.unregister([]);assert.equal(r.instanceId,'local-2');assert.equal(r.complete,false);
  await fs.writeFile(join(second.registration,'binding.json'),original);
}));
test('AC4/7 missing or changed stored-login identity still permits independently safe local exit',async()=>{
  for(const run of [async()=>({ok:false,http:401}),async()=>({ok:true,identity:'9'})]) await fixture(async f=>{
    f.dependencies.run=run;
    const r=await f.unregister();assert.equal(r.complete,false);assert.equal(r.local,'detached');assert.equal(r.remote,'unknown');assert.equal(f.calls.length,0);
  });
});
test('AC3/8 already-stopped service and production pinned loader retain the same safety checks',()=>fixture(async f=>{
  const observe=f.dependencies.termination;
  f.dependencies.termination=()=>({...observe(),ActiveState:'inactive',MainPID:'0',TasksCurrent:'0'});
  delete f.dependencies.core;
  assert.equal((await f.unregister()).complete,true);
  assert.ok(f.processes.some(args=>args.includes('disable')));
}));
test('AC4/7 contradictory or tainted residuals cannot fabricate completion or echo secrets',async()=>{
  for(const mode of ['contradictory','tainted','pending']) await fixture(async f=>{
    assert.equal((await f.unregister()).complete,true);
    const path=join(f.record.instances[0].registration,'unregister.json'),value=await f.state();
    if(mode==='pending') await fs.writeFile(path+'.pending','interrupted',{mode:0o600});
    else {value.local=mode==='contradictory'?'retained':secret;value.extra=secret;await fs.writeFile(path,JSON.stringify(value));}
    const r=await f.unregister();assert.equal(r.complete,false);assert.ok(!JSON.stringify(r).includes(secret));
    const calls=f.calls.length;
    await assert.rejects(()=>registerInstance(['--runner','demo','--repo','example/a'],f.dependencies));
    assert.equal(f.calls.length,calls);
  });
});

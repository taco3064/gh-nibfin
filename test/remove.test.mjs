import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir,userInfo} from 'node:os';
import {withRegistrationCore} from '../src/core.mjs';
import {removeEnvironment,removeOptions} from '../src/remove.mjs';
import {registerInstance,withEnvironmentLock,registrationProcess} from '../src/register.mjs';
import {unregisterInstance} from '../src/unregister.mjs';
import {main} from '../src/cli.mjs';
import {listEnvironments} from '../src/list.mjs';
const core=await withRegistrationCore(c=>c),secret='REMOVE_PRIVATE_SENTINEL';
const response=data=>({status:200,headers:{},body:JSON.stringify(data)});
async function fixture(action,{registered=true}={}) {
  const root=await fs.mkdtemp(join(tmpdir(),'remove-'));
  const account=userInfo(),base=join(root,'demo'),loginSpace=join(base,'login');
  try {
    await fs.mkdir(loginSpace,{recursive:true,mode:0o700});
    const record={schemaVersion:1,name:'demo',agent:'codex',githubId:'8',account:{uid:account.uid,username:account.username},loginSpace,instances:[]};
    const bindings=[],present=new Set(),stopped=new Set(),disabled=new Set(),calls=[],processes=[];
    for(let n=1;n<=3;n++) {
      const i={id:'local-'+n,loginSpace,status:'unregistered'};
      for(const leaf of ['registration','service','workspace']) {i[leaf]=join(base,`instance-${n}`,leaf);await fs.mkdir(i[leaf],{recursive:true,mode:0o700});}
      record.instances.push(i);
      const b={schemaVersion:1,instanceId:i.id,memberId:'8',runnerId:n===1?'9007199254740993':String(n+90),label:'nibfin-user-8',platform:'linux',disposition:'registered',status:'offline',busy:false,
        runnerName:`nibfin-demo-${n}`,registration:n===3?{level:'organization',id:'7',organization:'example'}:{level:'repository',id:String(50+n)},
        target:n===3?{id:'7',owner:'example'}:{id:String(50+n),owner:'example',repository:n===1?'a':'b',ownerId:'7',ownerType:n===1?'User':'Organization'},
        service:{account:account.username,unit:`nibfin-demo-${i.id}.service`,status:'active'}};
      bindings.push(b);
      if(registered) {
        present.add(b.runnerId);
        await fs.writeFile(join(i.registration,'binding.json'),JSON.stringify(b),{mode:0o600});
        await fs.writeFile(join(i.service,b.service.unit),`[Unit]\nDescription=Nibfin instance runner\n[Service]\nWorkingDirectory="${i.registration}"\nExecStart="${i.registration}/runsvc.sh"\nRestart=on-failure\n[Install]\nWantedBy=default.target\n`,{mode:0o600});
      }
      for(const leaf of ['.credentials','runsvc.sh','config.sh']) await fs.writeFile(join(i.registration,leaf),secret,{mode:0o600});
    }
    await fs.writeFile(join(loginSpace,'agent'),secret,{mode:0o600});
    await fs.writeFile(join(base,'environment.json'),JSON.stringify(record),{mode:0o600});
    const sibling=join(root,'neighbor');await fs.mkdir(sibling,{mode:0o700});await fs.writeFile(join(sibling,'login'),secret,{mode:0o600});
    const f={root,base,record,bindings,present,calls,processes,dependencies:{root,core,
      run:async()=>({ok:true,identity:'8'}),
      transport:async r=>{
        calls.push(r);assert.equal(r.identity,'local-stored-login');
        const b=bindings.find(b=>r.endpoint.startsWith(b.registration.level==='repository'?`/repos/example/${b.target.repository}`:'/orgs/example'));
        assert.ok(b,'only selected target');
        if(r.method==='DELETE') {present.delete(b.runnerId);return {status:204,headers:{},body:''};}
        if(r.endpoint.includes('/actions/runners?')) return response({total_count:present.has(b.runnerId)?1:0,runners:present.has(b.runnerId)?
          [{id:b.runnerId,name:b.runnerName,os:'linux',status:'offline',busy:false,labels:[{name:b.label}]}]:[]});
        return response(b.registration.level==='repository'?{id:b.target.id,full_name:`example/${b.target.repository}`,private:false,visibility:'public',owner:{id:'7',type:b.target.ownerType}}:
          {id:'7',login:'example',type:'Organization'});
      },
      process:async(command,args,o)=>{processes.push(args);assert.equal(command,'systemctl');assert.equal(o.env.GH_TOKEN,undefined);assert.equal(o.env.GITHUB_TOKEN,undefined);
        if(args.includes('stop')) stopped.add(args.at(-1));if(args.includes('disable')) disabled.add(args.at(-1));return {ok:true};},
      termination:unit=>{
        const n=bindings.findIndex(b=>b.service.unit===unit),i=record.instances[n];
        return registered?{LoadState:'loaded',ActiveState:stopped.has(unit)?'inactive':'active',MainPID:stopped.has(unit)?'0':'123',TasksCurrent:stopped.has(unit)?'0':'1',
          FragmentPath:join(i.service,unit),UnitFileState:disabled.has(unit)?'disabled':'enabled',DropInPaths:'',KillMode:'control-group',NeedDaemonReload:'no',TriggeredBy:'',PropagatesStopTo:'',BoundBy:'',ConsistsOf:'',UpheldBy:''}:
          {LoadState:'not-found',ActiveState:'inactive',MainPID:'0',TasksCurrent:'0'};
      }},remove:args=>removeEnvironment(args ?? ['--runner','demo','--yes'],f.dependencies),
      state:async()=>JSON.parse(await fs.readFile(join(root,'.removals/demo.json')))};
    await action(f);
    assert.equal(await fs.readFile(join(sibling,'login'),'utf8'),secret);
  } finally {await fs.rm(root,{recursive:true,force:true});}
}
test('AC1 strict selection, confirmation, cancellation and unknown names have no effects',async()=>{
  for(const args of [[],['--runner'],['--runner','../demo'],['--runner','demo\n'],['--runner','demo','--yes','--yes'],['--runner','demo','--runner','demo'],['--runner','demo','x'],['--runner','demo','--bad']]) assert.throws(()=>removeOptions(args));
  assert.deepEqual(removeOptions(['--yes','--runner','demo']),{name:'demo',yes:true});
  await fixture(async f=>{
    await assert.rejects(()=>f.remove(['--runner','missing','--yes']));
    let prompt;f.dependencies.confirm=async text=>{prompt=text;return false;};
    assert.equal((await f.remove(['--runner','demo'])).cancelled,true);
    assert.match(prompt,/ALL instances/);assert.match(prompt,/entire shared Organization/);assert.match(prompt,/9007199254740993/);
    assert.equal(f.calls.length,0);assert.equal(f.processes.length,0);
    delete f.dependencies.confirm;
    assert.equal((await f.remove(['--runner','demo'])).cancelled,true);
    assert.ok(await fs.stat(f.base));
  });
});
test('AC2/4/7 confirmed environment-wide removal preserves siblings and exact remote scopes',()=>fixture(async f=>{
  f.dependencies.confirm=async()=>true;
  const r=await f.remove(['--runner','demo']);assert.equal(r.complete,true);assert.equal(r.local,'removed');
  await assert.rejects(()=>fs.stat(f.base),{code:'ENOENT'});
  assert.deepEqual(f.calls.filter(c=>c.method==='DELETE').map(c=>c.endpoint),[
    '/repos/example/a/actions/runners/9007199254740993','/repos/example/b/actions/runners/92','/orgs/example/actions/runners/93']);
  assert.equal(f.processes.filter(a=>a.includes('stop')).length,3);assert.equal(f.processes.filter(a=>a.includes('disable')).length,3);
  assert.equal((await f.state()).complete,true);assert.ok(!JSON.stringify(await f.state()).includes(secret));
  const repeated=await f.remove();assert.equal(repeated.already,true);assert.equal(repeated.complete,true);
  assert.equal(f.calls.filter(c=>c.method==='DELETE').length,3);
}));
test('AC2 never registered and partially unregistered instances remove credentials and workspaces',async()=>{
  await fixture(async f=>{delete f.dependencies.core;const r=await f.remove();assert.equal(r.complete,true);assert.equal(f.calls.length,0);},{registered:false});
  await fixture(async f=>{
    const i=f.record.instances[0],b=f.bindings[0];
    await fs.writeFile(join(i.registration,'unregister.json'),JSON.stringify({identity:b,attempted:true,complete:true,remote:'absent'}),{mode:0o600});
    await fs.rm(join(i.registration,'binding.json'));f.present.delete(b.runnerId);
    assert.equal((await f.remove()).complete,true);
  });
});
test('AC3 stop proof and service ownership failures retain resources and never broaden effects',async()=>{
  for(const mode of ['stop','tasks','pid','manager','fragment','dropin','trigger','propagates','bound','partof','upheld','kill','reload-needed','foreign-source','reload']) await fixture(async f=>{
    const observe=f.dependencies.termination,proc=f.dependencies.process;
    if(mode==='stop'||mode==='reload') f.dependencies.process=(c,a,o)=>a.includes(mode==='stop'?'stop':'daemon-reload')?{ok:false}:proc(c,a,o);
    else if(mode==='foreign-source') await fs.appendFile(join(f.record.instances[0].service,f.bindings[0].service.unit),'Also=neighbor.service\n');
    else f.dependencies.termination=unit=>({...observe(unit),...({tasks:{TasksCurrent:'2'},pid:{MainPID:'222'},manager:{LoadState:'unknown'},fragment:{FragmentPath:'/foreign'},
      dropin:{DropInPaths:'/foreign.conf'},trigger:{TriggeredBy:'neighbor.timer'},propagates:{PropagatesStopTo:'neighbor.service'},bound:{BoundBy:'neighbor.service'},partof:{ConsistsOf:'neighbor.service'},upheld:{UpheldBy:'neighbor.service'},kill:{KillMode:'process'},'reload-needed':{NeedDaemonReload:'yes'}}[mode])});
    const r=await f.remove();assert.equal(r.complete,false);assert.equal(r.local,'retained');
    assert.equal(await fs.readFile(join(f.record.loginSpace,'agent'),'utf8'),secret);
    assert.ok(r.instances.some(i=>i.service==='unknown'));assert.ok(f.processes.flat().every(s=>!s.includes('neighbor')));
  });
});
test('AC3 environment and per-instance exclusion blocks all competing lifecycle work',()=>fixture(async f=>{
  await withEnvironmentLock(f.root,'demo',async()=>{await assert.rejects(()=>f.remove(),/reconcile/);});
  const path=join(f.record.instances[2].service,'registration.lock');await fs.mkdir(path);
  assert.equal((await f.remove()).complete,false);assert.equal(f.processes.length,0);await fs.rmdir(path);
  const proc=f.dependencies.process;let checked=false;
  f.dependencies.process=async(...args)=>{
    if(!checked) {checked=true;
      await assert.rejects(()=>registerInstance(['--runner','demo','--repo','example/a'],f.dependencies),/reconciliation|reconcile/);
      const u=await unregisterInstance(['--runner','demo','--repo','example/a'],f.dependencies);assert.equal(u.complete,false);
      await assert.rejects(()=>f.remove());
    }return proc(...args);
  };
  assert.equal((await f.remove()).complete,true);
}));
test('AC3 history and unknown writes permit safe local exit while preserving safe effects facts',async()=>{
  for(const mode of ['workspace','worker','write','busy']) await fixture(async f=>{
    const i=f.record.instances[0];
    if(mode==='workspace') await fs.writeFile(join(i.workspace,'job'),secret);
    if(mode==='worker') {await fs.mkdir(join(i.registration,'_diag'),{mode:0o700});await fs.writeFile(join(i.registration,'_diag/Worker_old.log'),secret);}
    if(mode==='write') await fs.writeFile(join(i.service,'write-intent.json'),JSON.stringify({operation:'merge',repository:'example/a',issueId:123,requestId:'operation-unique-42',token:secret}),{mode:0o600});
    if(mode==='busy') {const transport=f.dependencies.transport;f.dependencies.transport=async r=>{const v=await transport(r);if(r.endpoint.includes('/actions/runners?')) {const d=JSON.parse(v.body);d.runners.forEach(r=>r.busy=true);return response(d);}return v;};}
    const r=await f.remove();assert.equal(r.local,'removed');assert.equal(r.complete,false);assert.equal(r.instances[0].effects,'unresolved');
    assert.ok(!JSON.stringify(await f.state()).includes(secret));await assert.rejects(()=>fs.stat(f.base));
  });
});
test('AC4 remote failures and missing login do not retain safe local resources or suppress other targets',async()=>{
  for(const mode of [401,403,404,'timeout','malformed','pagination','target','runner','login','changed-login']) await fixture(async f=>{
    const transport=f.dependencies.transport;
    if(mode==='login'||mode==='changed-login') f.dependencies.run=async()=>({ok:mode!=='login',identity:'9'});
    else f.dependencies.transport=async r=>{
      if(!r.endpoint.startsWith('/repos/example/a')) return transport(r);
      if(mode==='timeout') throw Error(secret);
      if(typeof mode==='number') return {status:mode,headers:{},body:secret};
      if(mode==='malformed') return {status:200,headers:{},body:secret};
      if(mode==='pagination' && r.endpoint.includes('/actions/runners?')) return response({total_count:2,runners:[]});
      if(mode==='target' && !r.endpoint.includes('/actions/')) return response({id:'99'});
      const v=await transport(r);if(mode==='runner' && r.endpoint.includes('/actions/runners?')) {const d=JSON.parse(v.body);d.runners[0].name='replaced';return response(d);}return v;
    };
    const r=await f.remove();assert.equal(r.local,'removed');assert.equal(r.complete,false);assert.equal(r.instances[0].remote,'unknown');
    assert.ok(!JSON.stringify(r).includes(secret));assert.equal(f.calls.filter(c=>c.method==='DELETE' && c.endpoint.startsWith('/repos/example/a')).length,0);
    if(!['login','changed-login'].includes(mode)) assert.equal(r.instances[1].remote,'absent');
  });
});
test('AC4 DELETE readback and uncertain response reentry never replay a write',async()=>{
  for(const removed of [true,false]) await fixture(async f=>{
    const transport=f.dependencies.transport;let deletes=0;
    f.dependencies.transport=async r=>{
      if(r.method==='DELETE') {deletes++;if(removed) await transport(r);throw Error(secret);}return transport(r);
    };
    const r=await f.remove();assert.equal(r.local,'removed');assert.equal(r.complete,removed);assert.equal(deletes,3);
    assert.equal((await f.remove()).complete,removed);assert.equal(deletes,3);
  });
  await fixture(async f=>{f.present.clear();assert.equal((await f.remove()).complete,true);assert.equal(f.calls.filter(c=>c.method==='DELETE').length,0);});
});
test('AC5 publication, filesystem, reload, interrupted cleanup and generation reuse remain truthful',async()=>{
  for(const mode of ['intent','final-publication','filesystem']) await fixture(async f=>{
    if(mode==='intent') f.dependencies.publish=async()=>{throw Error(secret);};
    if(mode==='final-publication') f.dependencies.publish=async(p,s)=>{if(s.local==='removed') throw Error(secret);await fs.writeFile(p,JSON.stringify(s),{mode:0o600});};
    if(mode==='filesystem') f.dependencies.remove=async()=>{throw Error(secret);};
    const r=await f.remove();assert.equal(r.complete,false);assert.ok(!JSON.stringify(r).includes(secret));
    if(mode==='intent') {assert.equal(f.processes.length,0);assert.ok(await fs.stat(f.base));}
    else {delete f.dependencies.publish;delete f.dependencies.remove;const recovered=await f.remove();assert.equal(recovered.complete,true);}
  });
  await fixture(async f=>{
    assert.equal((await f.remove()).complete,true);
    await fs.mkdir(f.base,{mode:0o700});await fs.writeFile(join(f.base,'new-generation'),secret,{mode:0o600});
    await assert.rejects(()=>f.remove());assert.equal(await fs.readFile(join(f.base,'new-generation'),'utf8'),secret);
  });
});
test('AC6 symlinks, escaped descriptors, account mismatches and tainted residues reject safely',async()=>{
  for(const mode of ['root','descriptor','leaf','escape','account','binding','residue']) await fixture(async f=>{
    const i=f.record.instances[0];
    if(mode==='root') {await fs.rename(f.base,f.base+'-real');await fs.symlink(f.base+'-real',f.base);}
    if(mode==='descriptor') {await fs.rename(join(f.base,'environment.json'),join(f.base,'original'));await fs.symlink(join(f.base,'original'),join(f.base,'environment.json'));}
    if(mode==='leaf') await fs.symlink(join(f.root,'neighbor/login'),join(i.workspace,'link'));
    if(mode==='escape') {f.record.instances[0].workspace=join(f.root,'neighbor');await fs.writeFile(join(f.base,'environment.json'),JSON.stringify(f.record));}
    if(mode==='account') f.dependencies.account={...userInfo(),uid:999};
    if(mode==='binding') {f.bindings[0].target.owner=secret;await fs.writeFile(join(i.registration,'binding.json'),JSON.stringify(f.bindings[0]));}
    if(mode==='residue') {await fs.mkdir(join(f.root,'.removals'),{mode:0o700});await fs.writeFile(join(f.root,'.removals/demo.json'),JSON.stringify({token:secret}),{mode:0o600});}
    await assert.rejects(()=>f.remove());assert.equal(f.processes.length,0);assert.equal(f.calls.length,0);
  });
});
test('AC7 CLI surfaces local/remote/effect dispositions and safe errors',()=>fixture(async f=>{
  const stdout=process.stdout.write,stderr=process.stderr.write,out=[];
  process.stdout.write=process.stderr.write=s=>{out.push(s);return true;};
  try {
    f.dependencies.run=async()=>({ok:false});
    assert.equal(await main(['runner','remove','--runner','demo','--yes'],f.dependencies),2);
    assert.match(out.join(''),/local=removed/);assert.match(out.join(''),/remote=unknown/);assert.match(out.join(''),/Private reconciliation record/);
    assert.equal(await main(['runner','remove','--runner',secret],f.dependencies),2);assert.ok(!out.join('').includes(secret));
  } finally {process.stdout.write=stdout;process.stderr.write=stderr;}
}));
test('AC3 real bounded process termination kills a disposable process group',async()=>{
  const root=await fs.mkdtemp(join(tmpdir(),'remove-process-')),pidfile=join(root,'pid');
  try {
    const source=`require('fs').writeFileSync(${JSON.stringify(pidfile)},String(process.pid));setInterval(()=>{},1000)`;
    const r=await registrationProcess(process.execPath,['-e',source],{env:{PATH:process.env.PATH},timeoutMs:300});assert.equal(r.reason,'timeout');
    const pid=Number(await fs.readFile(pidfile,'utf8'));assert.throws(()=>process.kill(pid,0),{code:'ESRCH'});
  } finally {await fs.rm(root,{recursive:true,force:true});}
});
test('AC3/5 failed reload resumes from fresh disabled/empty proof without retaining credentials forever',()=>fixture(async f=>{
  const proc=f.dependencies.process;
  f.dependencies.process=(c,a,o)=>a.includes('daemon-reload')?{ok:false}:proc(c,a,o);
  assert.equal((await f.remove()).local,'retained');
  f.dependencies.process=proc;
  assert.equal((await f.remove()).complete,true);
}));
test('AC5 pending intent, stale process locks and partial deletion recover without uncertain write replay',()=>fixture(async f=>{
  f.dependencies.remove=async path=>{await fs.rm(join(path,'login'),{recursive:true,force:true});throw Error(secret);};
  assert.equal((await f.remove()).complete,false);
  const path=join(f.root,'.removals/demo.json');
  await fs.rename(path,path+'.pending');
  // Obtain a real exited disposable PID rather than guessing an unused host PID.
  const pidfile=join(f.root,'dead-pid');
  await registrationProcess(process.execPath,['-e',`require('fs').writeFileSync(${JSON.stringify(pidfile)},String(process.pid))`],{env:{PATH:process.env.PATH}});
  const pid=await fs.readFile(pidfile,'utf8');await fs.writeFile(join(f.root,'.demo.lifecycle.lock'),pid,{mode:0o600});
  delete f.dependencies.remove;
  assert.equal((await f.remove()).complete,true);assert.equal(f.calls.filter(c=>c.method==='DELETE').length,3);
}));
test('AC3 dead per-instance lifecycle owners recover; live or unowned locks remain protected',()=>fixture(async f=>{
  const p=join(f.record.instances[0].service,'registration.lock');await fs.mkdir(p,{mode:0o700});
  await fs.writeFile(join(p,'lifecycle-owner.json'),JSON.stringify({pid:process.pid}),{mode:0o600});
  assert.equal((await f.remove()).complete,false);assert.equal(f.processes.length,0);
  const pidfile=join(f.root,'dead-pid');
  await registrationProcess(process.execPath,['-e',`require('fs').writeFileSync(${JSON.stringify(pidfile)},String(process.pid))`],{env:{PATH:process.env.PATH}});
  await fs.writeFile(join(p,'lifecycle-owner.json'),JSON.stringify({pid:Number(await fs.readFile(pidfile,'utf8'))}));
  assert.equal((await f.remove()).complete,true);
}));
test('AC2/5 a failed registration reservation without Runner ID allows local exit and retains minimal identity',()=>fixture(async f=>{
  const i=f.record.instances[0],b=structuredClone(f.bindings[0]);
  b.disposition='unknown';delete b.runnerId;delete b.platform;delete b.status;delete b.busy;delete b.service.unit;b.secret=secret;
  await fs.writeFile(join(i.registration,'binding.json.pending'),JSON.stringify(b),{mode:0o600});
  await fs.rm(join(i.registration,'binding.json'));
  const r=await f.remove();assert.equal(r.local,'removed');assert.equal(r.complete,false);
  assert.equal(r.instances[0].reservation.target.id,'51');assert.equal(r.instances[0].remote,'unknown');
  assert.ok(!JSON.stringify(await f.state()).includes(secret));
  assert.equal(f.calls.filter(c=>c.method==='DELETE').length,2);
  assert.equal((await f.remove()).complete,false);
}));
test('AC5 canonical residue validation precedes quarantine deletion, and reentry still requires confirmation',()=>fixture(async f=>{
  f.dependencies.remove=async()=>{throw Error(secret);};await f.remove();delete f.dependencies.remove;
  const tomb=join(f.root,'.removals/demo.resources');
  f.dependencies.confirm=async()=>false;
  assert.equal((await f.remove(['--runner','demo'])).cancelled,true);assert.ok(await fs.stat(tomb));
  const path=join(f.root,'.removals/demo.json'),s=await f.state();s.instances[0].identity.extra=secret;
  await fs.writeFile(path,JSON.stringify(s));await assert.rejects(()=>f.remove());assert.ok(await fs.stat(tomb));
}));
test('AC5 name reuse during withdrawal never cleans locks or resources of the new generation',()=>fixture(async f=>{
  const sentinel=join(f.record.instances[0].service,'registration.lock/remove-owner.json');
  f.dependencies.remove=async(path,options)=>{
    await fs.rm(path,options);
    await fs.mkdir(f.record.loginSpace,{recursive:true,mode:0o700});
    for(const i of f.record.instances) for(const leaf of ['registration','service','workspace']) await fs.mkdir(i[leaf],{recursive:true,mode:0o700});
    await fs.writeFile(join(f.base,'environment.json'),JSON.stringify(f.record),{mode:0o600});
    await fs.mkdir(join(f.record.instances[0].service,'registration.lock'),{mode:0o700});
    await fs.writeFile(sentinel,secret,{mode:0o600});
  };
  assert.equal((await f.remove()).complete,true);
  assert.equal(await fs.readFile(sentinel,'utf8'),secret);
  await assert.rejects(()=>f.remove());assert.equal(await fs.readFile(sentinel,'utf8'),secret);
}));

test('AC7 read-only inventory recognizes private lifecycle metadata but rejects unsafe metadata paths',async()=>{
  const root=await fs.mkdtemp(join(tmpdir(),'remove-inventory-'));
  try {
    await fs.mkdir(join(root,'.removals'),{mode:0o700});
    await fs.writeFile(join(root,'.demo.lifecycle.lock'),String(process.pid),{mode:0o600});
    const r=await listEnvironments([],{root,core});assert.equal(r.complete,true);assert.deepEqual(r.environments,[]);
    await fs.rmdir(join(root,'.removals'));await fs.symlink('/nonexistent',join(root,'.removals'));
    assert.equal((await listEnvironments([],{root,core})).complete,false);
  } finally {await fs.rm(root,{recursive:true,force:true});}
});

test('AC3/5 repair: exact operation reconciliation identities survive deletion and repeated removal',()=>fixture(async f=>{
  const source=join(f.record.instances[0].service,'write-intent.json');
  await fs.writeFile(source,JSON.stringify({operation:'merge',repository:'example/a',issueId:123,requestId:'operation-unique-42',token:secret,response:{token:secret}}),{mode:0o600});
  const first=await f.remove();assert.equal(first.local,'removed');assert.equal(first.complete,false);
  await assert.rejects(()=>fs.stat(source),{code:'ENOENT'});
  const expected={source:'write-intent.json',operation:'merge',repository:'example/a',requestId:'operation-unique-42',issueId:'123'};
  assert.deepEqual((await f.state()).instances[0].operations,[expected]);
  assert.ok(!JSON.stringify(await f.state()).includes(secret));
  const deletes=f.calls.filter(c=>c.method==='DELETE').length;
  const again=await f.remove();assert.equal(again.local,'removed');assert.equal(again.complete,false);
  assert.deepEqual(again.instances[0].operations,[expected]);assert.equal(f.calls.filter(c=>c.method==='DELETE').length,deletes);
}));
test('AC3/5 repair: execution and pending write identities survive interrupted deletion without raw payloads',()=>fixture(async f=>{
  const i=f.record.instances[0];
  const first={schemaVersion:1,operation:'close',repository:'example/a',issueId:'I_kwDOVSXbns8AAAABZnFOUA',requestId:'request-1',log:secret};
  const second={operation:'merge',repository:'example/a',pullRequestNumber:42,headSha:'a'.repeat(40),requestId:'request-2',credentials:secret};
  await fs.writeFile(join(i.service,'execution.json'),JSON.stringify(first),{mode:0o600});
  await fs.writeFile(join(i.service,'write-intent.json.pending'),JSON.stringify(second),{mode:0o600});
  f.dependencies.remove=async()=>{throw Error(secret);};
  assert.equal((await f.remove()).complete,false);
  const before=(await f.state()).instances[0].operations;assert.equal(before.length,2);
  assert.equal(before[0].issueId,first.issueId);assert.equal(before[1].pullRequestNumber,'42');assert.equal(before[1].headSha,second.headSha);
  delete f.dependencies.remove;
  const after=await f.remove();assert.equal(after.local,'removed');assert.deepEqual(after.instances[0].operations,before);
  assert.ok(!JSON.stringify(await f.state()).includes(secret));
}));
test('AC5/6 repair: unrepresentable essential records remain private at their source after safe shutdown',async()=>{
  for(const raw of [{token:secret},{operation:'merge',repository:'example/a',requestId:'r',issueId:0},
    {operation:'merge',repository:'example/a',requestId:'r',issueId:1,unknownCoordinate:'essential'},secret,'x'.repeat(1048577)]) await fixture(async f=>{
    const source=join(f.record.instances[0].service,'write-intent.json'),bytes=typeof raw==='string'?raw:JSON.stringify(raw);
    await fs.writeFile(source,bytes,{mode:0o600});
    const r=await f.remove();assert.equal(r.local,'retained');assert.equal(r.complete,false);
    assert.ok(r.instances.every(i=>i.service==='terminated'));assert.deepEqual(r.instances[0].retainedSources,['write-intent.json']);
    assert.equal(await fs.readFile(source,'utf8'),bytes);assert.ok(!JSON.stringify(await f.state()).includes(secret));
    const again=await f.remove();assert.equal(again.local,'retained');assert.equal(await fs.readFile(source,'utf8'),bytes);
  });
});
test('AC5 repair: failed publication of operation coordinates never deletes their only source',()=>fixture(async f=>{
  const source=join(f.record.instances[0].service,'write-intent.json');
  await fs.writeFile(source,JSON.stringify({operation:'merge',repository:'example/a',issueId:123,requestId:'operation-unique-42'}),{mode:0o600});
  f.dependencies.publish=async(path,s)=>{if(s.instances.some(i=>i.operations?.length)) throw Error(secret);await fs.writeFile(path,JSON.stringify(s),{mode:0o600});};
  const r=await f.remove();assert.equal(r.local,'retained');assert.equal(r.complete,false);assert.match(await fs.readFile(source,'utf8'),/operation-unique-42/);
  delete f.dependencies.publish;
  assert.equal((await f.remove()).local,'removed');assert.equal((await f.state()).instances[0].operations[0].requestId,'operation-unique-42');
}));
test('AC6 repair: tainted persisted operation identities are rejected on reentry before provider effects',()=>fixture(async f=>{
  await fs.writeFile(join(f.record.instances[0].service,'write-intent.json'),JSON.stringify({operation:'merge',repository:'example/a',issueId:123,requestId:'operation-unique-42'}),{mode:0o600});
  await f.remove();const s=await f.state();s.instances[0].operations[0].extra=secret;
  await fs.writeFile(join(f.root,'.removals/demo.json'),JSON.stringify(s));const calls=f.calls.length;
  await assert.rejects(()=>f.remove());assert.equal(f.calls.length,calls);
}));
test('AC5 repair: pending publication cannot discard already captured reconciliation identities',()=>fixture(async f=>{
  await fs.writeFile(join(f.record.instances[0].service,'write-intent.json'),JSON.stringify({operation:'merge',repository:'example/a',issueId:123,requestId:'operation-unique-42'}),{mode:0o600});
  await f.remove();const older=await f.state();delete older.instances[0].operations;
  await fs.writeFile(join(f.root,'.removals/demo.json.pending'),JSON.stringify(older),{mode:0o600});
  const calls=f.calls.length;await assert.rejects(()=>f.remove());assert.equal(f.calls.length,calls);
  assert.equal((await f.state()).instances[0].operations[0].requestId,'operation-unique-42');
}));

import {test,after} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join,resolve,sep} from 'node:path';
import {tmpdir,userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {pathToFileURL} from 'node:url';
import {createEnvironment,options,processEnvironment,githubObservation,observeProcess,backendReady} from '../src/create.mjs';
import {withCurrentCore} from '../src/current-core.mjs';
import {readinessTransport} from './fixtures/readiness.mjs';
const checkout=resolve(import.meta.dirname,'..'),sentinel='credential-sentinel';
let core,context,release,opened;const ready=new Promise(r=>opened=r);
const installation=withCurrentCore(async(c,x)=>{core=c;context=x;opened();await new Promise(r=>release=r);});
installation.catch(opened);await ready;if(!core)await installation;
after(async()=>{release();await installation;});
async function scratch(run){const root=await fs.mkdtemp(join(tmpdir(),'create-proof-'));try{return await run(root);}finally{await fs.rm(root,{recursive:true,force:true});}}
function run(command,args,environment){assert.ok(['git','gh'].includes(command));assert.ok(['--version','auth','api'].includes(args[0]));assert.equal(environment.GH_TOKEN,undefined);return {ok:true,identity:command==='gh'&&args[0]==='api'?'42':undefined};}
const args=(name='sample',count)=>['--name',name,'--agent','codex',...(count===undefined?[]:['--count',String(count)])];
function deps(root,{flags={},agent='codex'}={}){
  const transport=readinessTransport(core,context,{flags,agent});
  return {root:join(root,'storage'),run,core:async()=>core,packageContext:context,configuration:{image:transport.image,socket:`/run/user/${userInfo().uid}/docker.sock`},backendProcess:transport.process,transport,
    login:async(selected,environment)=>{assert.equal(selected,agent);await fs.writeFile(join(environment.CODEX_HOME,agent==='codex'?'auth.json':agent==='claude'?'anthropic-api-key.json':'gemini-api-key.json'),JSON.stringify(agent==='codex'?{auth_mode:'chatgpt',tokens:{access_token:sentinel}}:{auth_mode:agent==='claude'?'anthropic-api-key':'gemini-api-key',api_key:sentinel}),{flag:'wx',mode:0o600});}};
}
async function absent(path){await assert.rejects(fs.stat(path),{code:'ENOENT'});}
test('explicit installed-producer Docker29/Node-image profile is persisted; unknown profiles and silent fallback are rejected',()=>scratch(async root=>{
  const profile='linux-amd64-codex-node22-docker29-v1';
  const flags={docker:'29.9.1',imageEnvironment:['PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','NODE_VERSION=22.23.3','YARN_VERSION=1.22.22']};
  const d=deps(root,{flags}),record=await createEnvironment([...args('profile',1),'--runtime-profile',profile],d);
  assert.equal(record.runtime.runtimeProfile,profile);assert.equal(record.instances.length,1);
  const before=await fs.readdir(d.root);
  await assert.rejects(()=>createEnvironment([...args('unknown',1),'--runtime-profile','linux-amd64-codex-unreviewed'],d),/verified producer catalog/);
  assert.deepEqual(await fs.readdir(d.root),before);
  await assert.rejects(()=>createEnvironment(args('implicit',1),deps(root,{flags})),/not ready/);
  const wrong=deps(root,{flags:{...flags,docker:'28.5.1'}});
  await assert.rejects(()=>createEnvironment([...args('wrong',1),'--runtime-profile',profile],wrong),/not ready/);
}));

test('executable-level installed-Core proof: unrelated cwd, spaces in checkout, default/1/4; exact persisted allocation',()=>scratch(async root=>{
  const copy=join(root,'extension with spaces');await fs.cp(checkout,copy,{recursive:true});
  const harness=join(root,'local-proof.mjs');
  await fs.writeFile(harness,`import fs from 'node:fs/promises';import {join} from 'node:path';import {userInfo} from 'node:os';import {main} from ${JSON.stringify(pathToFileURL(join(copy,'src/cli.mjs')).href)};import {withCurrentCore} from ${JSON.stringify(pathToFileURL(join(copy,'src/current-core.mjs')).href)};import {readinessTransport} from ${JSON.stringify(pathToFileURL(join(copy,'test/fixtures/readiness.mjs')).href)};await withCurrentCore(async(core,context)=>{const transport=readinessTransport(core,context);process.exitCode=await main(process.argv.slice(2),{root:${JSON.stringify(join(root,'storage'))},core:async()=>core,packageContext:context,configuration:{image:transport.image,socket:'/run/user/'+userInfo().uid+'/docker.sock'},backendProcess:transport.process,login:async(a,e)=>fs.writeFile(join(e.CODEX_HOME,'auth.json'),JSON.stringify({auth_mode:'chatgpt',tokens:{access_token:'MEMBER-SENTINEL'}}),{mode:0o600}),run:async(c,a)=>({ok:true,identity:c==='gh'&&a[0]==='api'?'42':undefined})});});`);
  for(const [name,count,expected] of [['default',undefined,2],['one',1,1],['four',4,4]]){
    const result=spawnSync(process.execPath,[harness,'runner','create',...args(name,count)],{cwd:tmpdir(),encoding:'utf8',env:{PATH:process.env.PATH,HOME:'/nonexistent'}});assert.equal(result.status,0,result.stderr);
    const base=join(root,'storage',name),record=JSON.parse(await fs.readFile(join(base,'environment.json')));
    assert.equal(record.instances.length,expected);assert.equal(record.agent,'codex');assert.equal(new Set(record.instances.map(i=>i.id)).size,expected);assert.equal((await fs.readdir(base)).length,expected+2);
    for(const i of record.instances){assert.equal(i.loginSpace,record.loginSpace);assert.equal(i.status,'unregistered');assert.equal(i.serviceStatus,'not-installed');for(const p of [i.workspace,i.registration,i.service])assert.deepEqual(await fs.readdir(p),[]);for(const k of ['runnerId','token','binding'])assert.equal(i[k],undefined);}
  }
  const records=await Promise.all(['default','one','four'].map(async n=>JSON.parse(await fs.readFile(join(root,'storage',n,'environment.json')))));assert.equal(new Set(records.map(r=>r.loginSpace)).size,3);
}));

test('strict options reject invalid counts, unsafe names, duplicate flags and operands before filesystem activity',async()=>{
  for(const v of ['0','-1','1.2','NaN','9007199254740992','129','01',''])assert.throws(()=>options(args('sample',v)),/Count|Use/);
  for(const v of ['../escape','/absolute','a/b','A','x\n','..'])assert.throws(()=>options(args(v)));
  for(const extra of [['--count'],['extra'],['--repo','a/b'],['--name','other'],['--agent','claude']])assert.throws(()=>options([...args(),...extra]));
  await assert.rejects(createEnvironment(args('sample',0),{io:new Proxy({},{get(){assert.fail('allocation before validation');}})}));
});

test('actual Core rejects aliases; readiness never launches role tasks and cleans its owned probes',()=>scratch(async root=>{
  const d=deps(root);await createEnvironment(args(),d);
  assert.equal(d.transport.calls.filter(c=>c.args[2]==='start'&&c.args.includes('--attach')).length,2);
  assert.equal(d.transport.calls.some(c=>c.args.at(-1)==='execute'),false);assert.equal(d.transport.gateways.size,0);assert.equal(d.transport.networks.size,0);
  await assert.rejects(createEnvironment(args('aliased'),{...deps(root),id:()=> 'same'}));await absent(join(d.root,'aliased'));
  for(const instances of [[{id:'a',workspace:'/x',registration:'/x',service:'/s'}],[{id:'a',workspace:'/x',registration:'/x/y',service:'/s'}]])assert.throws(()=>core.createAgentEnvironments().describe({id:'env',agent:'codex',loginSpace:'/login',instances}));
}));

test('existing names and symlink escapes preserve existing bytes and member login',()=>scratch(async root=>{
  const d=deps(root);await createEnvironment(args(),d);const file=join(d.root,'sample/environment.json'),before=await fs.readFile(file);
  await fs.writeFile(join(d.root,'sample/login/sentinel'),sentinel);
  await assert.rejects(createEnvironment(['--name','sample','--agent','claude'],{...deps(root,{agent:'claude'})}),/already exists/);
  assert.deepEqual(await fs.readFile(file),before);assert.equal(await fs.readFile(join(d.root,'sample/login/sentinel'),'utf8'),sentinel);
  const outside=join(root,'outside');await fs.mkdir(outside);await fs.writeFile(join(outside,'sentinel'),sentinel);await fs.symlink(outside,join(d.root,'linked'));
  await assert.rejects(createEnvironment(args('linked'),d),/already exists/);await assert.rejects(createEnvironment(args('escape'),{...d,root:join(d.root,'linked/child')}),/link/);assert.deepEqual(await fs.readdir(outside),['sentinel']);
}));

test('credential-free diagnostics: tools, gh login/401/403, identity and execution account',()=>scratch(async root=>{
  const environment=processEnvironment(join(root,'login'),{PATH:process.env.PATH,HOME:'/host-login',GH_TOKEN:sentinel,GITHUB_TOKEN:sentinel,CODEX_HOME:'/host',NODE_OPTIONS:sentinel});
  assert.equal(environment.HOME,userInfo().homedir);assert.equal(environment.CODEX_HOME,join(root,'login'));for(const k of ['GH_TOKEN','GITHUB_TOKEN','NODE_OPTIONS','OPENAI_API_KEY'])assert.equal(environment[k],undefined);
  for(const [command,first,http] of [['git','--version'],['gh','--version'],['gh','auth'],['gh','api',401],['gh','api',403]]){
    const result=async(c,a,e)=>c===command&&a[0]===first?{ok:false,http,raw:sentinel}:run(c,a,e);
    await assert.rejects(githubObservation({run:result,environment,account:userInfo()}),e=>!e.message.includes(sentinel)&&/tool|gh/.test(e.message));
    await assert.rejects(createEnvironment(args('failed'),{...deps(root),run:result}));await absent(join(root,'storage/failed'));
  }
  await assert.rejects(githubObservation({run,environment,account:{uid:-1}}),/account mismatch/);
  const executable=join(root,'process-tool');await fs.writeFile(executable,'#!/bin/sh\nprintf credential-sentinel >&2\nexit 1\n',{mode:0o700});
  assert.deepEqual(observeProcess(executable,[],environment),{ok:false,missing:false,http:undefined,identity:undefined});assert.equal(observeProcess(join(root,'missing'),[],environment).missing,true);
}));

test('missing login/isolation/domain, backend exceptions and partial reports fail safely with resumable member resources',()=>scratch(async root=>{
  let n=0;for(const flags of [{login:false},{security:[]},{missingDomain:'registry.npmjs.org'},{throw:'info'}]){
    const name='unavailable-'+(++n),d=deps(root,{flags});await assert.rejects(createEnvironment(args(name),d),e=>/Creation incomplete/.test(e.message)&&!e.message.includes(sentinel));
    await absent(join(d.root,name,'environment.json'));assert.match(await fs.readFile(join(d.root,name,'login/auth.json'),'utf8'),/credential-sentinel/);assert.equal(d.transport.calls.some(c=>c.args.at(-1)==='execute'),false);
  }
  for(const report of [{ready:true},{ready:false},{ready:'unknown'},{},null])assert.equal(backendReady(report),false);
  const d=deps(root);delete d.core;delete d.configuration;await assert.rejects(createEnvironment(args('unconfigured'),d),/preprovisioned/);await absent(join(d.root,'unconfigured'));
}));

test('allocation failures clean only owned resources; publication failure preserves member credentials and original intent',()=>scratch(async root=>{
  const d=deps(root);await fs.mkdir(d.root,{mode:0o700});await fs.writeFile(join(d.root,'sentinel'),sentinel);
  const io=Object.create(fs);io.mkdir=async(...values)=>{if(String(values[0]).endsWith('registration'))throw Error(sentinel);return fs.mkdir(...values);};
  await assert.rejects(createEnvironment(args(),{...d,io}),e=>!e.message.includes(sentinel));await absent(join(d.root,'sample'));
  const publish=Object.create(fs);publish.rename=async(...values)=>{if(String(values[1]).endsWith('environment.json'))throw Error(sentinel);return fs.rename(...values);};
  await assert.rejects(createEnvironment(args(),{...deps(root),io:publish}),/Creation incomplete/);await absent(join(d.root,'sample/environment.json'));assert.match(await fs.readFile(join(d.root,'sample/login/auth.json'),'utf8'),/credential-sentinel/);
  await assert.rejects(createEnvironment(args(),deps(root)),/publication is unresolved/);assert.equal(await fs.readFile(join(d.root,'sentinel'),'utf8'),sentinel);
  const broken=Object.create(fs);broken.mkdir=io.mkdir;broken.rm=async()=>{throw Error(sentinel);};await assert.rejects(createEnvironment(args('cleanup'),{...deps(root),io:broken}),/cleanup incomplete.*cleanup/);
}));

test('real diagnostic subprocesses use the execution login context; provider output is sanitized',()=>scratch(async root=>{
  const bin=join(root,'bin');await fs.mkdir(bin);const log=join(root,'observations');
  const script=`#!/bin/sh\nprintf '%s|%s|%s|%s|%s|%s\\n' "$(id -u)" "$GH_CONFIG_DIR" "$CODEX_HOME" "$GH_TOKEN" "$GITHUB_TOKEN" "$*" >> '${log}'\ncase "$*" in\n '--version') exit 0;;\n 'auth status --hostname github.com') printf credential-sentinel >&2; exit 0;;\n 'api user --jq .id') printf '42\\n'; exit 0;;\n *) exit 99;;\nesac\n`;
  for(const tool of ['git','gh'])await fs.writeFile(join(bin,tool),script,{mode:0o700});
  const d={...deps(root),run:(c,a,e)=>observeProcess(c,a,{...e,PATH:bin+':'+process.env.PATH})},record=await createEnvironment(args(),d),lines=(await fs.readFile(log,'utf8')).trim().split('\n');assert.equal(lines.length,4);
  for(const line of lines){const [uid,config,login,token,actions]=line.split('|');assert.equal(uid,String(userInfo().uid));assert.equal(config,processEnvironment(record.loginSpace).GH_CONFIG_DIR);assert.equal(login,record.loginSpace);assert.equal(token,'');assert.equal(actions,'');}
  assert.doesNotMatch(await fs.readFile(join(d.root,'sample/environment.json'),'utf8'),/credential-sentinel/);
  await fs.writeFile(join(bin,'gh'),'#!/bin/sh\nprintf "HTTP 403 credential-sentinel" >&2\nexit 1\n',{mode:0o700});await assert.rejects(createEnvironment(args('rejected'),d),e=>!e.message.includes(sentinel));await absent(join(d.root,'rejected'));
}));

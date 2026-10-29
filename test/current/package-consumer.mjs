import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {pathToFileURL} from 'node:url';
import {withCurrentCore,verifyCurrentCore,currentPin} from '../../src/current-core.mjs';

test('installed requirement adapters use protected selectors and preserve fresh native Issue content without provider fallback',async()=>{
  await withCurrentCore(async core=>{
    const target={repository:'example-org/core',repositoryId:'42',issueId:'I_101',databaseId:'61',number:'101'},calls=[];
    let body='Current Scope\n完整原文';
    const provider=core.createGithubProvider({attempts:1,transport:async r=>{
      calls.push(r);assert.equal(r.method,'GET');
      return {status:200,body:JSON.stringify(r.endpoint==='/repos/example-org/core'
        ?{id:'42',full_name:'example-org/core',private:false,visibility:'public'}
        :{id:'61',node_id:'I_101',number:101,body,repository_url:'https://api.github.com/repos/example-org/core',
          url:'https://api.github.com/repos/example-org/core/issues/101'})};
    }});
    const input={source:{kind:'github-issue',target},provider,cycle:'cycle',approvalId:'installed-owner-policy'};
    const first=await core.observeRequirementAuthority(input);assert.equal(first.outcome,'observed');
    assert.deepEqual(core.supportedRequirementSources,[{kind:'github-issue',version:1}]);
    assert.equal(first.authority.components[0].body,body);body+='\nNew live requirements';
    const next=await core.observeRequirementAuthority(input);assert.equal(next.outcome,'observed');
    assert.notEqual(first.authority.identity,next.authority.identity);assert.equal(calls.length,4);
    for(const kind of ['jira','linear','notion'])assert.equal((await core.observeRequirementAuthority({...input,source:{kind,target}})).outcome,'unavailable');
    assert.equal(calls.length,4);assert.equal(core.verifyAuthority(next.authority).ok,false);
  });
});

test('exact installed public APIs construct and report explicit unavailable member readiness; no fabricated ready flag',async()=>{
  await withCurrentCore(async(core,{root,identity})=>{
    const source=core.createInstallation({version:1,expected:currentPin,profile:'source',
      observeCapability:async({capability,version,sha256})=>{
        assert.equal(sha256,identity.pins[capability]);return {version,status:'verified',sha256};
      }});
    const sourceReport=await source.check();
    assert.equal(sourceReport.status,'ready');assert.equal(sourceReport.scope,'source-contract');
    const base=join(root,'member');await fs.mkdir(base,{mode:0o700});
    const descriptor={id:'member',agent:'codex',loginSpace:join(base,'login'),instances:[{id:'instance',
      workspace:join(base,'workspace'),registration:join(base,'registration'),service:join(base,'service')}]};
    for(const path of [descriptor.loginSpace,...Object.values(descriptor.instances[0]).slice(1)])await fs.mkdir(path,{mode:0o700});
    const environment=core.createAgentEnvironments().describe(descriptor);
    let tasks=0;
    const config={root:base,image:'sha256:'+'1'.repeat(64),socket:'/run/user/1000/docker.sock',environment,
      instanceId:'instance',task:()=>{tasks++;throw Error('No Issue/Merge task authorized');},
      process:async()=>({ok:false,output:''}),egress:{image:'sha256:'+'1'.repeat(64),node:true}};
    for(const invalid of [{...config,agent:'codex'},{...config,task:{}},{...config,process:{}},{...config,image:'latest'},{...config,socket:'/var/run/docker.sock'}])
      assert.throws(()=>core.createLinuxCodexBackend(invalid));
    const report=await core.createLinuxCodexBackend(config).readiness();
    assert.equal(report.login,'unavailable');assert.equal(report.isolation,'unsupported');assert.equal(report.ready,undefined);
    assert.ok(Array.isArray(report.evidence));assert.ok(Array.isArray(report.limitations));assert.equal(tasks,0);
  });
});

test('modified archives/manifests/compatibility, links and historical identity-only packages reject before consumption',async()=>{
  const original=new URL('../../docs/handoffs/current/',import.meta.url);
  for(const name of ['nibfin-core.tgz','release-manifest.json','compatibility.json','identity-only','link']){
    const directory=await fs.mkdtemp(join(tmpdir(),'nibfin-tamper-'));
    try{
      for(const asset of ['nibfin-core.tgz','release-manifest.json','compatibility.json'])await fs.copyFile(new URL(asset,original),join(directory,asset));
      if(name==='identity-only')await fs.copyFile(new URL('../../docs/handoffs/n01/nibfin-core.tgz',import.meta.url),join(directory,'nibfin-core.tgz'));
      else if(name==='link'){
        await fs.rename(join(directory,'nibfin-core.tgz'),join(directory,'alias'));
        await fs.symlink(join(directory,'alias'),join(directory,'nibfin-core.tgz'));
      }else await fs.appendFile(join(directory,name),'tampered');
      let consumed=false;
      await assert.rejects(withCurrentCore(()=>{consumed=true;},{directory:pathToFileURL(directory+'/')}),/verification/);
      assert.equal(consumed,false);
    }finally{await fs.rm(directory,{recursive:true,force:true});}
  }
  const verified=await verifyCurrentCore();assert.ok(verified.files.size>100);
});

test('installed selected backend verifies complete synthetic daemon/egress observations and rejects missing required domains; Core owns cleanup',async()=>{
  const {readinessTransport}=await import('../fixtures/readiness.mjs');
  await withCurrentCore(async(core,context)=>{
    const base=join(context.root,'readiness');await fs.mkdir(base,{mode:0o700});
    const descriptor={id:'member',agent:'codex',loginSpace:join(base,'login'),instances:[{id:'local',workspace:join(base,'workspace'),registration:join(base,'registration'),service:join(base,'service')}]};
    await fs.mkdir(descriptor.loginSpace,{mode:0o700});
    await fs.writeFile(join(descriptor.loginSpace,'auth.json'),JSON.stringify({auth_mode:'chatgpt',tokens:{access_token:'MEMBER-SENTINEL'}}),{mode:0o600});
    for(const flags of [{},{missingDomain:'cdn.playwright.dev'},{missingDomain:'playwright.download.prss.microsoft.com'},{missingDomain:'registry.npmjs.org'},{security:[]},{login:false}]){
      for(const path of Object.values(descriptor.instances[0]).slice(1))await fs.mkdir(path,{mode:0o700});
      const environment=core.createAgentEnvironments().describe(descriptor),transport=readinessTransport(core,context,{flags});
      const backend=core.createLinuxCodexBackend({root:base,image:transport.image,socket:'/run/user/1000/docker.sock',environment,instanceId:'local',task:()=>assert.fail('No task authorized'),process:transport.process,egress:{image:transport.image,node:true}});
      const report=await backend.readiness();
      assert.equal(report.login,Object.keys(flags).length?'unavailable':'available',backend.diagnostic());
      assert.equal((await backend.cleanup({environment:'member',instance:'local',resources:Object.values(descriptor.instances[0]).slice(1)})).status,'completed');
      assert.equal(transport.gateways.size,0);assert.equal(transport.networks.size,0);assert.equal(transport.containers.size,0);
      assert.match(await fs.readFile(join(descriptor.loginSpace,'auth.json'),'utf8'),/MEMBER-SENTINEL/);
    }
  });
});

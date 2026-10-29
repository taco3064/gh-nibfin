import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir,userInfo} from 'node:os';
import {withCurrentCore} from '../../src/current-core.mjs';
import {createEnvironment,backendReady} from '../../src/create.mjs';

test('creation reaches the installed backend; unavailable tools retain exact unregistered allocation and member login for resumption',async()=>{
  await withCurrentCore(async(core,context)=>{
    const scratch=await fs.mkdtemp(join(tmpdir(),'nibfin-create-'));
    try{
      let logins=0,processes=0;
      const root=join(scratch,'environments'),image='sha256:'+'1'.repeat(64);
      const dependencies={root,core:()=>core,packageContext:context,configuration:{image,socket:'/run/user/1000/docker.sock'},
        run:async(command,args,env)=>{assert.equal(env.GH_TOKEN,undefined);assert.equal(env.GITHUB_TOKEN,undefined);return {ok:true,identity:command==='gh'&&args[0]==='api'?'42':undefined};},
        login:async(agent,env)=>{logins++;assert.equal(agent,'codex');await fs.writeFile(join(env.CODEX_HOME,'auth.json'),JSON.stringify({auth_mode:'chatgpt',tokens:{access_token:'MEMBER-SECRET-SENTINEL'}}),{flag:'wx',mode:0o600});},
        backendProcess:async request=>{processes++;assert.equal(request.file,'/usr/bin/docker');assert.deepEqual(request.env,{PATH:'/usr/bin:/bin',LANG:'C'});return {ok:false,output:'PRIVATE-PROVIDER-SENTINEL'};}};
      const args=['--name','member','--agent','codex'];
      await assert.rejects(createEnvironment(args,dependencies),e=>/Creation incomplete/.test(e.message)&&!/SENTINEL/.test(e.message));
      assert.equal(logins,1);assert.ok(processes>0);
      const base=join(root,'member'),pending=JSON.parse(await fs.readFile(join(base,'creation.json')));
      assert.equal(pending.count,2);assert.equal(pending.instances.length,2);assert.equal(new Set(pending.instances.map(i=>i.id)).size,2);
      assert.equal(pending.uid,userInfo().uid);assert.equal(pending.githubId,'42');
      for(const instance of pending.instances)for(const path of [instance.workspace,instance.registration,instance.service])assert.deepEqual(await fs.readdir(path),[]);
      await assert.rejects(fs.stat(join(base,'environment.json')),{code:'ENOENT'});
      await assert.rejects(createEnvironment(args,dependencies),/Creation incomplete/);
      assert.equal(logins,1);assert.deepEqual(JSON.parse(await fs.readFile(join(base,'creation.json'))).instances,pending.instances);
      const before=processes;
      await assert.rejects(createEnvironment([...args,'--count','4'],dependencies),/different account\/Agent\/count\/runtime/);
      assert.equal(processes,before);
      assert.match(await fs.readFile(join(base,'login/auth.json'),'utf8'),/MEMBER-SECRET-SENTINEL/);
      assert.doesNotMatch(await fs.readFile(join(base,'creation.json'),'utf8'),/SENTINEL/);
      const unavailable={...dependencies,run:async()=>({ok:false,http:403})};
      await assert.rejects(createEnvironment(['--name','denied','--agent','codex'],unavailable),/tool/);
      await assert.rejects(fs.stat(join(root,'denied')),{code:'ENOENT'});
      await assert.rejects(createEnvironment(['--name','no-image','--agent','codex'],{...dependencies,configuration:{}}),/immutable|preprovisioned/);
      await assert.rejects(fs.stat(join(root,'no-image')),{code:'ENOENT'});
    }finally{await fs.rm(scratch,{recursive:true,force:true});}
  });
});

test('readiness accepts only the complete explicit producer report',()=>{
  const report={login:'available',tool:'available',isolation:'verified',read:'verified',write:'verified',evidence:['diagnostic:ready'],limitations:[]};
  assert.equal(backendReady(report),true);
  for(const value of [null,{}, {ready:true}, {...report,login:'unknown'}, {...report,isolation:'unsupported'}, {...report,evidence:[]}, {...report,limitations:undefined}])assert.equal(backendReady(value),false);
});

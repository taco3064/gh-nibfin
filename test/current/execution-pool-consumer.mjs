import {test} from 'node:test';
import assert from 'node:assert/strict';
import {withCurrentCore} from '../../src/current-core.mjs';
import {observeExecutionPool} from '../../src/execution-pool.mjs';
import {installationOptions,expectedInstallation} from '../../src/installation.mjs';
import {existingControlHost} from '../../src/host-install.mjs';

test('installed pool validation observes bounded public administration identities and preserves protected membership',async()=>{
  await withCurrentCore(async(core,context)=>{
    const names=['example/project','example/cli','example/action'],selection={id:'member-default',capacity:6,targets:names};
    const calls=[];let patch={};
    const provider=core.createGithubProvider({authorize:()=>assert.fail('Pool observation cannot write'),transport:async request=>{
      calls.push(request);assert.equal(request.method,'GET');
      const name=request.endpoint.replace('/repos/','');assert.ok(names.includes(name));
      return {status:200,headers:{},body:JSON.stringify({id:String(42+names.indexOf(name)),full_name:name,private:false,visibility:'public',
        owner:{id:'7',type:'User'},permissions:{admin:true},...(name===names[1]?patch:{})})};
    }});
    const input={selection,repo:names[0],provider,memberId:'7'};
    const pool=await observeExecutionPool(core,input);
    assert.equal(calls.length,3);assert.equal(pool.source,undefined);assert.equal(pool.capacity,6);assert.equal(pool.targets.length,3);
    assert.ok(Object.isFrozen(pool));assert.ok(Object.isFrozen(pool.targets));
    assert.deepEqual(await observeExecutionPool(core,{...input,selection:undefined,previous:pool}),pool);
    const desired=await expectedInstallation(core,context,{executionPool:pool,repo:names[0]});
    assert.deepEqual(JSON.parse(desired.files.get('.nibfin/runtime.json')).executionPool,pool);
    for(const alteration of [{id:'99'},{owner:{id:'8',type:'User'}},{owner:{id:'7',type:'Organization'}},{private:true},{permissions:{admin:false}}]){
      patch=alteration;await assert.rejects(observeExecutionPool(core,{...input,previous:pool}));
    }
    patch={};
    await assert.rejects(observeExecutionPool(core,{...input,previous:pool,selection:{...selection,capacity:5}}),{code:'protected-pool-transition-required'});
    const before=calls.length;
    for(const bad of [{source:names[0],targets:[names[0],names[0]]},{source:'other/project',targets:names},{source:names[0],targets:[names[0],'other/cli']}])
      await assert.rejects(observeExecutionPool(core,{...input,selection:bad}));
    assert.equal(calls.length,before);
    assert.equal(await observeExecutionPool(core,{repo:names[0]}),undefined);
    const checked=await existingControlHost().check({executionPool:pool});
    assert.equal(checked.status,'blocked');assert.equal(checked.diagnostics[0].capability,'executionPool');
  });
});

test('pool options stay within init, are finite and explicit, and cannot bypass a protected upgrade boundary',()=>{
  const args=['--repo','example/cli','--runner-count','6','--pool-repos','example/project,example/cli,example/action'];
  assert.equal(installationOptions(args).pool.targets.length,3);
  assert.equal(installationOptions(args).pool.capacity,6);assert.equal(installationOptions(args).pool.source,undefined);
  for(const invalid of [args.slice(0,4),[...args,'--execution-repo','example/project'],
    ['--repo','example/cli','--execution-repo','example/project','--pool-repos','example/project'],
    ['--repo','example/cli','--execution-repo','example/project','--pool-repos','example/project,example/cli,example/cli']])
    assert.throws(()=>installationOptions(invalid));
  assert.throws(()=>installationOptions(args,{upgrade:true}));
});

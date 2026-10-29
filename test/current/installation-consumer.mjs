import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {createHash} from 'node:crypto';
import {withCurrentCore} from '../../src/current-core.mjs';
import {initializeRepository} from '../../src/installation.mjs';
import {existingControlHost} from '../../src/host-install.mjs';
const blob=b=>createHash('sha1').update(`blob ${Buffer.byteLength(b)}\0`).update(b).digest('hex');
const response=(data,status=200)=>({status,headers:{},body:JSON.stringify(data)});

test('actual installed installation API: first/repeat init, modified bytes, explicit Owner opt-in, denied and unknown writes; check sends no mutations',async()=>{
  await withCurrentCore(async(core,context)=>{
    const root=await fs.mkdtemp(join(tmpdir(),'nibfin-init-'));
    try{
      const files=new Map(),labels=[],calls=[];let admin=true,deny=false,ambiguous=false,hostInstalls=0,hostPool;
      const host={check:async({executionPool})=>{if(hostInstalls)assert.deepEqual(executionPool,hostPool);return hostInstalls?{status:'ready'}:{status:'blocked',diagnostics:[{capability:'recovery',code:'missing-notifier'}]};},install:async({executionPool})=>{hostPool=executionPool;hostInstalls++;}};
      const transport=async r=>{
        calls.push({method:r.method,path:r.endpoint});
        const body=r.body===undefined?undefined:JSON.parse(r.body);
        const repositories=['example/project','example/cli','example/action'];
        if(repositories.some(name=>r.endpoint==='/repos/'+name)){const name=r.endpoint.slice(7);return response({id:String(42+repositories.indexOf(name)),full_name:name,private:false,visibility:'public',owner:{id:'7',type:'User'},permissions:{admin}});}
        if(r.endpoint==='/repos/example/project/labels?per_page=100')return response(labels);
        if(r.endpoint==='/repos/example/project/labels'&&r.method==='POST'){labels.push(body);return response(body,201);}
        const marker='/repos/example/project/contents/';
        assert.ok(r.endpoint.startsWith(marker),r.endpoint);const path=r.endpoint.slice(marker.length);
        if(r.method==='PUT'){
          if(deny)return response({message:'denied'},403);
          if(ambiguous)throw Error('transport lost without proving effect');
          const bytes=Buffer.from(body.content,'base64');files.set(path,bytes);return response({content:{sha:blob(bytes)}},201);
        }
        const bytes=files.get(path);return bytes?response({type:'file',encoding:'base64',size:bytes.length,content:bytes.toString('base64'),sha:blob(bytes)}):response({message:'Not Found'},404);
      };
      const deps={core,context,transport,memberId:'8',root,host},args=['--repo','example/project'];
      assert.equal((await initializeRepository([...args,'--check'],deps)).status,'blocked');
      assert.equal(calls.some(r=>r.method!=='GET'),false);assert.equal(hostInstalls,0);assert.deepEqual(await fs.readdir(root),[]);
      const poolArgs=[...args,'--execution-repo','example/project','--pool-repos','example/project,example/cli,example/action'];
      const unsupported=await initializeRepository(poolArgs,{...deps,host:existingControlHost()});
      assert.equal(unsupported.status,'blocked');assert.ok(unsupported.diagnostics.some(d=>d.code==='protected-shared-host-onboarding-incomplete'));
      assert.equal(calls.some(r=>r.method!=='GET'),false);assert.equal(hostInstalls,0);
      assert.equal((await initializeRepository([...args,'--auto-register','--execution-repo','example/project','--pool-repos','example/project,example/cli,example/action'],deps)).status,'ready');
      assert.ok(files.size>8);assert.equal(labels.length,3);assert.equal(hostInstalls,1);
      const runtime=JSON.parse(files.get('.nibfin/runtime.json'));
      assert.equal(runtime.automaticRegistration,true);assert.deepEqual(runtime.allowedActorIds,['8']);
      assert.equal(runtime.executionPool.source.repositoryId,'42');assert.equal(runtime.executionPool.targets.length,3);
      assert.deepEqual(runtime.executionPool,hostPool);
      const writes=calls.filter(r=>r.method!=='GET').length;
      assert.equal((await initializeRepository(args,deps)).status,'ready');
      assert.equal(calls.filter(r=>r.method!=='GET').length,writes);
      const before=hostInstalls;assert.equal((await initializeRepository([...args,'--check'],deps)).status,'ready');assert.equal(hostInstalls,before);
      const path='.github/workflows/nibfin-rescue.yml';files.set(path,Buffer.from('user changes\n'));
      assert.equal((await initializeRepository(args,deps)).diagnostics[0].code,'user-modification-preserved');
      assert.equal(files.get(path).toString(),'user changes\n');
      admin=false;const at=calls.length;assert.equal((await initializeRepository(args,deps)).status,'blocked');assert.equal(calls.slice(at).some(r=>r.method!=='GET'),false);
      admin=true;files.delete(path);deny=true;
      const denied=await initializeRepository(args,deps);assert.equal(denied.status,'blocked');assert.equal(denied.diagnostics[0].code,'managed-source-installation-denied');
      deny=false;ambiguous=true;const unknown=await initializeRepository(args,deps);
      assert.equal(unknown.status,'unknown');assert.equal(unknown.diagnostics[0].code,'original-write-reconciliation-required');
      assert.equal(calls.filter(r=>r.method==='PUT'&&r.path.endsWith(path)).length,2); // original first install plus one denied attempt
    }finally{await fs.rm(root,{recursive:true,force:true});}
  });
});

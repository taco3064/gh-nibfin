import {test} from 'node:test';
import assert from 'node:assert/strict';
import {withCurrentCore} from '../../src/current-core.mjs';

import {nativePoolSourceFixture as fixture} from '../../tooling/native-pool-source-fixture.mjs';

test('JIT source revalidates the project Run, Job, immutable installed workflow and live Issue without a write',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context),result=await f.source.inspect('request-0');
 assert.equal(result.authorized,true);assert.deepEqual(result.request,f.request);assert.equal(result.stage,'deliver');
 assert.equal(result.identity,f.retained.identity);
 for(const path of ['/user','/repos/example/project','/repos/example/project/actions/runs/300',
  '/repos/example/project/actions/jobs/200','/repos/example/project/issues/55'])assert.ok(f.calls.some(r=>r.endpoint===path));
 for(const path of ['.nibfin/runtime.json','.github/workflows/nibfin-stage.yml','.github/workflows/nibfin-delivery.yml'])
  assert.ok(f.calls.some(r=>r.endpoint.includes('/contents/'+path+'?ref='+f.retained.sourceCommit)));
 f.routes.get('/repos/example/project/actions/jobs/200').status='in_progress';
 assert.equal((await f.source.inspect('request-0')).identity,result.identity);
 assert.equal((await f.source.blocked({request:'request-0'})).status,'retained');
}));

test('foreign repository, unapproved actor, mismatched Job, absent request label and changed Scope all deny JIT authority',async()=>withCurrentCore(async(core,context)=>{
 const f=await fixture(core,context);
 const cases=[['/user',v=>{v.id='8';}],['/repos/example/project',v=>{v.permissions.admin=false;}],
  ['/repos/example/project/actions/runs/300',v=>{v.head_repository.id='101';}],
  ['/repos/example/project/actions/runs/300',v=>{v.triggering_actor.id='8';}],
  ['/repos/example/project/actions/runs/300',v=>{v.display_title='#55 | accept';}],
  ['/repos/example/project/actions/jobs/200',v=>{v.run_attempt=2;}],
  ['/repos/example/project/actions/jobs/200',v=>{v.labels=v.labels.filter(s=>!s.startsWith('nibfin-request-'));}],
  ['/repos/example/project/issues/55',v=>{v.pull_request={};}]];
 for(const [path,change]of cases){const original=structuredClone(f.routes.get(path));change(f.routes.get(path));
  await assert.rejects(f.source.inspect('request-0'),/native-pool-source:/);f.routes.set(path,original);}
 assert.ok(f.calls.every(r=>r.method==='GET'));
 const issue=f.routes.get('/repos/example/project/issues/55'),original=structuredClone(issue);
 for(const change of [v=>{v.body+=' changed';},v=>{v.assignees=[];},v=>{v.state='closed';}]){
  Object.assign(issue,structuredClone(original));change(issue);
  const observation=await f.source.inspect('request-0');assert.equal(observation.authorized,true);
  assert.equal(observation.launchAuthorized,false);assert.equal(observation.identity,f.retained.identity);
 }
}));

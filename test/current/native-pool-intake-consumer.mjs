import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {withCurrentCore} from '../../src/current-core.mjs';
import {nativePoolIntake} from '../../src/native-pool-intake.mjs';
import {ensurePrivateDirectory} from '../../src/local-state.mjs';
import {nativePoolSourceFixture} from '../../tooling/native-pool-source-fixture.mjs';
test('actual queued Job hints become a privately retained, independently verified intake; duplicate hints preserve identity',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativePoolSourceFixture(core,context),root=join(context.root,'intake');await ensurePrivateDirectory(root);
 const intake=await nativePoolIntake(core,{root,configuration:f.configuration,memberId:'7',provider:f.provider,
  installationRoot:f.installationRoot,onBlocked:async()=>({status:'retained'})});
 const hint={repository:'example/project',runId:'300',jobId:'200'};
 assert.equal((await intake.accept(hint)).status,'retained');const retained=await intake.read('request-0');
 assert.equal(retained.identity,f.retained.identity);assert.equal((await intake.accept(hint)).already,true);
 assert.equal((await intake.source.inspect('request-0')).authorized,true);assert.equal((await intake.list()).length,1);
 assert.equal((await fs.stat(join(root,'pool-intake/request-0.json'))).mode&0o777,0o600);
 assert.ok(f.calls.every(r=>r.method==='GET'));
 const issue=f.routes.get('/repos/example/project/issues/55');issue.body+=' revised';
 assert.equal((await intake.source.inspect('request-0')).launchAuthorized,false);
 assert.equal((await intake.read('request-0')).identity,retained.identity);
}));
test('title and forged event data cannot authorize intake; reject foreign actor, workflow bytes, member labels and duplicate selectors',async()=>withCurrentCore(async(core,context)=>{
 const f=await nativePoolSourceFixture(core,context),root=join(context.root,'intake-denials');await ensurePrivateDirectory(root);
 const intake=await nativePoolIntake(core,{root,configuration:f.configuration,memberId:'7',provider:f.provider,
  installationRoot:f.installationRoot,onBlocked:async()=>({status:'retained'})});
 const hint={repository:'example/project',runId:'300',jobId:'200'};
 for(const [path,mutate]of [
  ['/repos/example/project/actions/runs/300',r=>r.triggering_actor.id='8'],
  ['/repos/example/project/actions/runs/300',r=>r.path='.github/workflows/untrusted.yml'],
  ['/repos/example/project/actions/jobs/200',j=>j.labels.push('nibfin-request-other')],
  ['/repos/example/project/actions/jobs/200',j=>j.labels=j.labels.filter(l=>!l.startsWith('nibfin-user-'))],
  ['/repos/example/project/issues/55',i=>i.assignees=[]]]){
  const before=structuredClone(f.routes.get(path));mutate(f.routes.get(path));await assert.rejects(intake.accept(hint));
  assert.deepEqual(await intake.list(),[]);f.routes.set(path,before);
 }
 await assert.rejects(intake.accept({...hint,repository:'other/project'}),/target/);
 assert.ok(f.calls.every(r=>r.method==='GET'));
}));

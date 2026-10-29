import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {spawn} from 'node:child_process';
import {withCurrentCore,currentPin} from '../../src/current-core.mjs';
const sha=b=>createHash('sha256').update(b).digest('hex');
const blob=b=>createHash('sha1').update(`blob ${Buffer.byteLength(b)}\0`).update(b).digest('hex');

// Fresh installed Core and unchanged installed shell/Node Hook. Only authenticated
// provider reads are replaced. The Worker is a disposable Linux process double,
// never an actual Actions Runner or evidence of official binary certification.
test('installed Hook accepts Repository .runner identity and explicitly scoped Organization registrations; legacy scope still denies',async()=>{
  await withCurrentCore(async(core,{root,packageRoot,identity})=>{
    assert.equal(identity.components.core.sha256,currentPin.coreSha256);
    const installation=core.createInstallation({version:1,expected:currentPin,profile:'source',
      observeCapability:async()=>({status:'unknown'})});
    assert.equal((await installation.check()).status,'blocked');
    assert.throws(()=>core.createLinuxCodexBackend({agent:'codex',task:{role:'readiness'},process:{}}));
    for(const kind of ['repository','organization','legacy-org','wrong-actor','denied-read','shell-fallback']){
      const level=kind==='organization'||kind==='legacy-org'?kind:'repository';
      const allowed=['repository','organization'].includes(kind);
      const base=join(root,'probe-'+kind);await fs.mkdir(base,{mode:0o700});
      await fs.mkdir(join(base,'bin'),{mode:0o700});
      const executable=join(base,'bin/Runner.Worker');
      await fs.copyFile('/usr/bin/dash',executable);await fs.chmod(executable,0o700);
      const workerSha256=sha(await fs.readFile(executable));
      const repository='example-org/public-project',workflowPath='.github/workflows/nibfin-stage.yml';
      const bytes='name: Approved source\n';
      const policy=core.runnerJobPolicy({version:1,repository,repositoryId:'42',ownerId:'43',runnerId:'44',runnerName:'member',
        runnerDirectory:base,workerSha256,workflowPath,workflowSha256:sha(bytes),jobName:'execute',allowedActorIds:['45'],ref:'refs/heads/main',...(level==='organization'?{registrationLevel:'organization'}:{})});
      const local={agentId:'44',agentName:'member',gitHubUrl:level==='repository'?'https://github.com/'+repository:'https://github.com/example-org'};
      await fs.writeFile(join(base,'.runner'),JSON.stringify(local),{mode:0o600});
      const observation={context:{repository,runId:'46',attempt:1},
        run:{id:'46',run_attempt:1,event:'workflow_dispatch',status:'in_progress',head_branch:'main',path:workflowPath,head_sha:'a'.repeat(40),
          repository:{id:'42',full_name:repository,owner:{id:'43',type:'Organization'}},head_repository:{id:'42'},actor:{id:'45'},triggering_actor:{id:'45'}},
        job:{run_id:'46',status:'in_progress',name:'execute',runner_id:'44',runner_name:'member',head_sha:'a'.repeat(40),run_attempt:1},
        workflow:{type:'file',path:workflowPath,encoding:'base64',content:Buffer.from(bytes).toString('base64'),size:Buffer.byteLength(bytes),sha:blob(bytes)},
        localRunner:{id:'44',name:'member',repository:local.gitHubUrl.replace(/^https:\/\/github.com\//,'').replace(/\/$/,''),workerSha256,...(level==='organization'?{registrationLevel:'organization'}:{})}};
      if(kind==='wrong-actor')observation.run.actor.id='99';
      const expected=core.verifyRunnerJobAdmission(policy,observation);
      assert.equal(expected.allowed,!['legacy-org','wrong-actor'].includes(kind));
      if(level==='legacy-org')assert.equal(expected.code,'local-runner');
      const policyPath=join(base,'policy.json');await fs.writeFile(policyPath,JSON.stringify(policy),{mode:0o600});
      const fixture={
        ['/repos/'+repository+'/actions/runs/46']:observation.run,
        ['/repos/'+repository+'/actions/runs/46/attempts/1/jobs?per_page=100']:{total_count:1,jobs:[observation.job]},
        ['/repos/'+repository+'/contents/'+workflowPath+'?ref='+observation.run.head_sha]:observation.workflow};
      if(kind==='denied-read')delete fixture['/repos/'+repository+'/actions/runs/46'];
      const fixturePath=join(base,'provider.json');await fs.writeFile(fixturePath,JSON.stringify(fixture),{mode:0o600});
      const preload=join(base,'transport.mjs');
      await fs.writeFile(preload,`import cp from 'node:child_process';import {syncBuiltinESMExports} from 'node:module';import {readFileSync} from 'node:fs';
const fixture=JSON.parse(readFileSync(${JSON.stringify(fixturePath)},'utf8'));const original=cp.execFileSync;
cp.execFileSync=(file,args,options)=>{if(file!=='/usr/bin/gh')return original(file,args,options);if(args[0]!=='api'||!Object.hasOwn(fixture,args[1]))throw Error('Unexpected authenticated fixture read');return JSON.stringify(fixture[args[1]]);};syncBuiltinESMExports();\n`,{mode:0o600});
      const sentinel=join(base,'always-sentinel'),hook=join(packageRoot,'host/runner-job-hook.sh');
      const worker=spawn(executable,['-c','/usr/bin/bash "$1"; echo continued > "$2"','--',hook,sentinel],{
        env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:base,LANG:'C',NODE_OPTIONS:'--import='+(kind==='shell-fallback'?join(base,'missing-preload.mjs'):preload),
          NIBFIN_RUNNER_JOB_POLICY:policyPath,GITHUB_REPOSITORY:repository,GITHUB_RUN_ID:'46',GITHUB_RUN_ATTEMPT:'1'},stdio:['ignore','pipe','pipe']});
      let output='';worker.stdout.on('data',b=>output+=b);worker.stderr.on('data',b=>output+=b);
      const exited=await new Promise((resolve,reject)=>{
        const timer=setTimeout(()=>{worker.kill('SIGKILL');reject(Error('Worker did not finish within bounded fixture'));},5000);
        worker.once('error',e=>{clearTimeout(timer);reject(e);});
        worker.once('close',(code,signal)=>{clearTimeout(timer);resolve({code,signal});});
      });
      assert.doesNotMatch(output,/SECRET-SENTINEL/);
      if(allowed){
        assert.equal(exited.code,0,output);assert.equal(await fs.readFile(sentinel,'utf8'),'continued\n');
      }else{
        assert.equal(exited.signal,'SIGKILL',output);await assert.rejects(fs.readFile(sentinel),{code:'ENOENT'});
        if(level==='legacy-org')assert.match(output,/local-runner/);
        if(kind==='shell-fallback')assert.match(output,/ERR_MODULE_NOT_FOUND/);
      }
    }
  });
});

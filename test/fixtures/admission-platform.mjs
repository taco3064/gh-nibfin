import fs from 'node:fs/promises';
import {join} from 'node:path';
import assert from 'node:assert/strict';
// Platform-only service fixture for retained N15/N17 lifecycle tests. It does
// not certify admission. Current installed-package process fixtures exercise the
// real packaged aborting Hook separately; official binary integration is pending.
export function admissionPlatform(root){
  return {
    admission:async(core,context,{instance})=>{
      const directory=join(root,'platform-admission-'+instance.id);await fs.mkdir(directory,{mode:0o700});
      const hook=join(directory,'fixture-hook.sh'),policy=join(directory,'fixture-policy.json');
      await fs.writeFile(hook,'#!/bin/sh\nexit 1\n',{flag:'wx',mode:0o700});await fs.writeFile(policy,JSON.stringify({fixture:true,instance:instance.id}),{flag:'wx',mode:0o600});
      return {environment:{ACTIONS_RUNNER_HOOK_JOB_STARTED:hook,NIBFIN_RUNNER_JOB_POLICY:policy},record:{fixture:true}};
    },
    activationVerifier:async(instance,environment)=>{
      assert.equal(await fs.readFile(environment.ACTIONS_RUNNER_HOOK_JOB_STARTED,'utf8'),'#!/bin/sh\nexit 1\n');
      assert.deepEqual(JSON.parse(await fs.readFile(environment.NIBFIN_RUNNER_JOB_POLICY)),{fixture:true,instance:instance.id});
    },
    serviceEnvironment:async(unit,environment)=>{
      assert.ok(environment.ACTIONS_RUNNER_HOOK_JOB_STARTED);assert.ok(environment.NIBFIN_RUNNER_JOB_POLICY);
      return {...environment};
    }
  };
}

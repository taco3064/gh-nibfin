import {currentProducer} from './current-core.mjs';
export function managedWorkflowPlan(core,{executionPool,repository=executionPool?.source?.repository}={}){
  const pool=executionPool===undefined?null:core.validateExecutionPool(executionPool);
  if(pool&&pool.version!==2)throw Error('Legacy centralized execution pools require reconciliation; workflows must remain in their own repositories.');
  if(pool&&!pool.targets.some(t=>t.repository===repository))throw Error('Managed workflow target is outside the protected pool.');
  const plan=core.workflowInstallation({commit:currentProducer,hostRepository:'taco3064/nibfin',hostCommit:currentProducer,
    ...(pool?{executionPool:pool,repository}:{})});
  // Consumer-owned request correlation; the protected Core input and issuer
  // contracts are unchanged. This exact rendered source is verified on reads.
  const path='.github/workflows/nibfin-register.yml';
  plan.files.set(path,plan.files.get(path).replace('name: Nibfin registration request v1\n','name: Nibfin registration request v1\nrun-name: ${{ inputs.nibfin_registration }}\n'));
  return plan;
}

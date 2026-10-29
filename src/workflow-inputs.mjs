import {currentProducer} from './current-core.mjs';
export function managedWorkflowPlan(core){
  const plan=core.workflowInstallation({commit:currentProducer,hostRepository:'taco3064/nibfin',hostCommit:currentProducer});
  // Consumer-owned request correlation; the protected Core input and issuer
  // contracts are unchanged. This exact rendered source is verified on reads.
  const path='.github/workflows/nibfin-register.yml';
  plan.files.set(path,plan.files.get(path).replace('name: Nibfin registration request v1\n','name: Nibfin registration request v1\nrun-name: ${{ inputs.nibfin_registration }}\n'));
  return plan;
}

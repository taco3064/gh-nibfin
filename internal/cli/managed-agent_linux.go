package cli

import (
	"errors"
)

func setupManagedAgent(ops managedHostOperations, plan InitPlan, token []byte) error {
	if plan.Agent != nil {
		if ops.setupAgent == nil {
			return errors.New("AGENT_SETUP_UNAVAILABLE")
		}
		// This operation owns private input; it must never reuse the Codex envelope
		// for another provider. Controlled fixtures implement the same narrow seam.
		return ops.setupAgent(plan, token)
	}
	return ops.credentials(plan.Member.ID, token)
}

func checkManagedAgent(ops managedHostOperations, plan InitPlan, host *sharedHost) error {
	if plan.Agent == nil {
		return nil
	}
	if ops.readAgent == nil {
		return errors.New("INSTALLED_AGENT_READBACK_REQUIRED")
	}
	return ops.readAgent(plan, host)
}

func managedIntentAgent(plan InitPlan) (*agentBinding, error) {
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		return nil, err
	}
	intent, present, err := readManagedIntent(path)
	if err == nil && !present {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plan.Agent = intent.Plan.Agent
	if !validManagedIntent(intent, plan) ||
		plan.Agent != nil && !plan.Agent.valid() {
		return nil, errors.New("MANAGED_INSTALL_INTENT_CHANGED")
	}
	return plan.Agent, nil
}

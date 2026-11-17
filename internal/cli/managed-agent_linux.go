package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
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
	file, err := privateInstallFile(path, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("MANAGED_INSTALL_STATE_UNAVAILABLE")
	}
	defer file.Close()
	var intent managedInstallIntent
	decoder := json.NewDecoder(io.LimitReader(file, 2*1024*1024+1))
	if decoder.Decode(&intent) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("MANAGED_INSTALL_INTENT_CHANGED")
	}
	plan.Agent = intent.Plan.Agent
	if !validManagedIntent(intent, plan) ||
		plan.Agent != nil && !plan.Agent.valid() {
		return nil, errors.New("MANAGED_INSTALL_INTENT_CHANGED")
	}
	return plan.Agent, nil
}

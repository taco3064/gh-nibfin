package cli

import "errors"

type installResultEncoder interface{ Encode(any) error }

func publishManagedRoute(encoder installResultEncoder, plan InitPlan,
	runners []sharedRegistration, ops managedHostOperations) error {
	if initIssueRecipe(plan) != "issue-managed" {
		return publishManagedHandoff(encoder, plan, runners, nil)
	}
	if ops.activate == nil {
		return errors.New("MANAGED_RELAY_ACTIVATION_REQUIRED")
	}
	relay, err := ops.activate(plan, runners)
	if err != nil {
		return err
	}
	return publishManagedHandoff(encoder, plan, runners, &relay)
}

func publishManagedHandoff(encoder installResultEncoder, plan InitPlan,
	runners []sharedRegistration, relay *managedRelayResult) error {
	code, wake := "MANAGED_INSTALLATION_INACTIVE", "inactive"
	if relay != nil {
		code, wake = "MANAGED_ROUTING_CONFIGURED", "configured"
	}
	return encoder.Encode(struct {
		Status       string               `json:"status"`
		Code         string               `json:"code"`
		Wake         string               `json:"wake"`
		MemberID     int64                `json:"member_id"`
		RepositoryID int64                `json:"repository_id"`
		Repository   string               `json:"repository"`
		Execution    *SharedExecution     `json:"execution"`
		Source       Producers            `json:"source"`
		Runners      []sharedRegistration `json:"runners"`
		Relay        *managedRelayResult  `json:"relay,omitempty"`
	}{"completed", code, wake, plan.Member.ID, plan.Repository.ID,
		plan.Repository.FullName, plan.Execution, Pins(), runners, relay})
}

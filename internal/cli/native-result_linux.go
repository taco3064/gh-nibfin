package cli

import (
	"encoding/json"
	"errors"
	"fmt"
)

type installedContinuation struct {
	Status  string            `json:"status"`
	Results []json.RawMessage `json:"results"`
	Code    string            `json:"code,omitempty"`
}

// Recover only an already adopted, active native installation. Provider metadata
// establishes configuration, never secret equality. Fresh or incomplete setup
// retains the normal conflict and unknown-effect handling.
func (a App) resumeReadyNativeInit(plan InitPlan, encoder installResultEncoder,
	ops managedHostOperations) (bool, error) {
	if plan.Execution == nil || ops.discover == nil {
		return false, nil
	}
	host, err := ops.discover()
	if err != nil {
		return false, err
	}
	if host == nil || host.SharedExecution != *plan.Execution ||
		host.MemberID != plan.Member.ID ||
		host.adoptionStatus(plan.Repository.ID) != "observed" {
		return false, nil
	}
	if err := verifyNativeSetupIdentity(a.API, nativeSetupFromPlan(plan)); err != nil {
		return false, err
	}
	if sharedWakeConfiguration(a.API, plan.Repository.FullName,
		*plan.Execution) != "configured_unverified" {
		return false, nil
	}
	runners, err := verifySharedHost(a.API, *host)
	if err != nil {
		return false, err
	}
	if len(runners) != 2 || !sharedRunnersOnline(runners) {
		return false, nil
	}
	return true, a.publishReadyManagedRoute(encoder, plan, runners, ops)
}

func readInstalledContinuation(output []byte, processError error) installedContinuation {
	pending := installedContinuation{Status: "pending", Results: []json.RawMessage{},
		Code: "INSTALLED_CONTINUATION_REQUIRES_READBACK"}
	var result installedContinuation
	if json.Unmarshal(output, &result) != nil || result.Results == nil {
		return pending
	}
	switch result.Status {
	case "dispatched", "paused":
		if processError != nil {
			return pending
		}
	case "pending":
		if processError == nil || processError.Error() != "CORE_PENDING_RESULT" {
			return pending
		}
	default:
		return pending
	}
	for _, entry := range result.Results {
		if !validContinuationEntry(entry, result.Status) {
			return pending
		}
	}
	return result
}

func validContinuationEntry(data json.RawMessage, status string) bool {
	var entry struct {
		Repository   string `json:"repository"`
		RepositoryID int64  `json:"repositoryId"`
		Number       int64  `json:"number"`
		ID           string `json:"id"`
		RunID        int64  `json:"runId"`
		URL          string `json:"url"`
		Status       string `json:"status"`
		Stage        string `json:"stage"`
		Code         string `json:"code"`
	}
	if json.Unmarshal(data, &entry) != nil || !repoPattern.MatchString(entry.Repository) {
		return false
	}
	if entry.Status == "pending" {
		return status != "dispatched" && entry.Stage != "" && entry.Code != ""
	}
	return entry.Status == "" && entry.RepositoryID > 0 && entry.Number > 0 &&
		entry.ID != "" && entry.RunID > 0 && entry.URL == fmt.Sprintf(
		"https://github.com/%s/actions/runs/%d", entry.Repository, entry.RunID)
}

func (a App) publishReadyManagedRoute(encoder installResultEncoder, plan InitPlan,
	runners []sharedRegistration, ops managedHostOperations) error {
	if !plan.NativeManaged {
		return publishManagedRoute(encoder, plan, runners, ops)
	}
	if plan.Execution == nil || initIssueRecipe(plan) != "issue" ||
		!sharedRunnersOnline(runners) || len(runners) != 2 {
		return errors.New("NATIVE_INSTALLATION_NOT_READY")
	}
	host, err := ops.discover()
	if err != nil || host == nil || host.SharedExecution != *plan.Execution ||
		host.MemberID != plan.Member.ID || len(host.Runners) != 2 ||
		host.adoptionStatus(plan.Repository.ID) != "observed" {
		return errors.New("NATIVE_HOST_BINDING_CHANGED")
	}
	for i, runner := range runners {
		if runner.ID != host.Runners[i].ID || runner.Name != host.Runners[i].Name {
			return errors.New("NATIVE_RUNNER_BINDING_CHANGED")
		}
	}
	if err := checkManagedAgent(ops, plan, host); err != nil {
		return err
	}
	if err := setManagedPool(a.API, plan.Repository.FullName); err != nil {
		return err
	}
	result := readInstalledContinuation(nil, nil)
	if ops.complete != nil {
		output, err := ops.complete()
		result = readInstalledContinuation(output, err)
	}
	body := struct {
		Status       string                `json:"status"`
		Code         string                `json:"code"`
		Installation string                `json:"installation"`
		Credential   string                `json:"credential"`
		Continuation installedContinuation `json:"continuation"`
		Runners      []sharedRegistration  `json:"runners"`
		MemberID     int64                 `json:"member_id"`
		Repository   string                `json:"repository"`
		RepositoryID int64                 `json:"repository_id"`
		Execution    *SharedExecution      `json:"execution"`
		Source       Producers             `json:"source"`
	}{Status: "completed", Code: "NATIVE_ROUTING_CONFIGURED", Installation: "ready",
		Credential: "configured_unverified", Continuation: result, Runners: runners,
		MemberID: plan.Member.ID, Repository: plan.Repository.FullName,
		RepositoryID: plan.Repository.ID, Execution: plan.Execution, Source: Pins()}
	if err := encoder.Encode(body); err != nil {
		return err
	}
	if result.Status == "pending" {
		return &CommandError{Code: "INSTALLED_CONTINUATION_PENDING", Exit: 2}
	}
	return nil
}

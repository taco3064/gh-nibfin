package cli

import (
	"encoding/json"
	"errors"
)

const sharedRunnerWorkflow = ".github/workflows/shared-runner.yml"

var errCoreSharedDestinationUnsupported = errors.New(
	"PINNED_CORE_SHARED_DESTINATION_UNSUPPORTED")

// Inspect GitHub configuration without activating a route or exposing a secret.
func sharedWakeConfiguration(api API, repo string, execution SharedExecution) string {
	base := "repos/" + repo + "/actions/"
	for _, expected := range []struct {
		name  string
		check func(string) bool
	}{
		{"NIBFIN_SHARED_RUNNER_POOL", func(value string) bool { return value == "true" }},
		{"NIBFIN_SHARED_RUNNER_DESTINATION", func(value string) bool {
			var observed SharedExecution
			return json.Unmarshal([]byte(value), &observed) == nil && observed == execution
		}},
	} {
		var variable struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		err := api.Call("GET", base+"variables/"+expected.name, nil, &variable)
		if err != nil && err.Error() != "NOT_FOUND" {
			return "unverified"
		}
		if variable.Name != expected.name || !expected.check(variable.Value) {
			return "configuration_required"
		}
	}
	var secret struct {
		Name string `json:"name"`
	}
	err := api.Call("GET", base+"secrets/NIBFIN_WAKE_TOKEN", nil, &secret)
	if err != nil && err.Error() != "NOT_FOUND" {
		return "unverified"
	}
	if secret.Name != "NIBFIN_WAKE_TOKEN" {
		return "configuration_required"
	}
	return "configured_unverified"
}

type SharedExecution struct {
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repositoryId"`
}

func sameSharedExecution(current, planned *SharedExecution) bool {
	if current == nil || planned == nil {
		return current == planned
	}
	return *current == *planned
}

func planSharedExecution(api API, plan *InitPlan, files map[string]string,
	pin SourcePin, name string) error {
	repo, user, err := repository(api, name, true)
	if err != nil {
		return err
	}
	if user.ID != plan.Member.ID {
		return errors.New("SHARED_EXECUTION_MEMBER_CHANGED")
	}
	// A peer install must not create or replace the execution repository's workflow.
	ref := repo.DefaultBranch
	if repo.ID == plan.Repository.ID {
		ref = plan.Head
	}
	if ref == "" {
		return errors.New("SHARED_EXECUTION_DEFAULT_BRANCH_UNAVAILABLE")
	}
	const contractPath = "src/shared-runner-contract.json"
	source, _, err := content(api, pin.Repository, contractPath, pin.Commit)
	if err != nil {
		if err.Error() == "NOT_FOUND" {
			return errCoreSharedDestinationUnsupported
		}
		return err
	}
	var contract struct {
		SchemaVersion       int    `json:"schemaVersion"`
		DestinationVariable string `json:"destinationVariable"`
		WorkflowPath        string `json:"workflowPath"`
	}
	if json.Unmarshal(source, &contract) != nil || contract.SchemaVersion != 1 ||
		contract.DestinationVariable != "NIBFIN_SHARED_RUNNER_DESTINATION" ||
		contract.WorkflowPath != sharedRunnerWorkflow {
		return errors.New("INVALID_CORE_SHARED_DESTINATION_CONTRACT")
	}
	workflow, _, err := content(api, pin.Repository, contract.WorkflowPath, pin.Commit)
	if err != nil {
		return err
	}
	if repo.ID == plan.Repository.ID {
		files[sharedRunnerWorkflow] = string(workflow)
	} else {
		current, _, err := content(api, repo.FullName, sharedRunnerWorkflow, ref)
		if err != nil {
			return err
		}
		if string(current) != string(workflow) {
			return errors.New("SHARED_EXECUTION_WORKFLOW_MISMATCH")
		}
	}
	plan.Execution = &SharedExecution{Repository: repo.FullName, RepositoryID: repo.ID}
	return nil
}

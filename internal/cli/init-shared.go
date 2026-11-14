package cli

import (
	"encoding/json"
	"errors"
	"net/url"
)

const sharedRunnerWorkflow = ".github/workflows/shared-runner.yml"
const sharedPoolVariable = "NIBFIN_SHARED_RUNNER_POOL"
const sharedDestinationVariable = "NIBFIN_SHARED_RUNNER_DESTINATION"

var errCoreSharedDestinationUnsupported = errors.New(
	"PINNED_CORE_SHARED_DESTINATION_UNSUPPORTED")

// Inspect GitHub configuration without activating a route or exposing a secret.
func sharedWakeConfiguration(api API, repo string, execution SharedExecution) string {
	base := "repos/" + repo + "/actions/"
	for _, expected := range []struct {
		name  string
		check func(string) bool
	}{
		{sharedPoolVariable, func(value string) bool { return value == "true" }},
		{sharedDestinationVariable, func(value string) bool {
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

type actionsVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Prepare a new destination while dispatch is disabled. Never repoint an
// existing route or set the activation variable as part of installation.
func prepareSharedWakeDestination(api API, repo string,
	execution SharedExecution) (string, error) {
	base := "repos/" + repo + "/actions/variables"
	path := base + "/" + sharedDestinationVariable
	var observed actionsVariable
	err := api.Call("GET", path, nil, &observed)
	if err == nil {
		var current SharedExecution
		if observed.Name == sharedDestinationVariable &&
			json.Unmarshal([]byte(observed.Value), &current) == nil && current == execution {
			return "unchanged", nil
		}
		return "", errors.New("SHARED_WAKE_DESTINATION_CONFLICT")
	}
	if err.Error() != "NOT_FOUND" {
		return "", err
	}
	var pool actionsVariable
	err = api.Call("GET", base+"/"+sharedPoolVariable, nil, &pool)
	if err != nil && err.Error() != "NOT_FOUND" {
		return "", err
	}
	if err == nil && (pool.Name != sharedPoolVariable || pool.Value != "false") {
		return "", errors.New("SHARED_WAKE_MUST_BE_DISABLED_DURING_INSTALLATION")
	}
	value, err := json.Marshal(execution)
	if err != nil {
		return "", err
	}
	expected := actionsVariable{Name: sharedDestinationVariable, Value: string(value)}
	_ = api.Call("POST", base, expected, nil)
	if err := api.Call("GET", path, nil, &observed); err != nil || observed != expected {
		return "", errors.New("SHARED_WAKE_DESTINATION_RESULT_UNVERIFIED")
	}
	return "prepared", nil
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
	if repo.DefaultBranch == "" {
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
	} else if err := verifySharedExecutionWorkflow(api, repo, workflow); err != nil {
		return err
	}
	plan.Execution = &SharedExecution{Repository: repo.FullName, RepositoryID: repo.ID}
	return nil
}

func verifySharedExecutionWorkflow(api API, repo Repository, expected []byte) error {
	base := "repos/" + repo.FullName
	var branch struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
		Commit    struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	branchPath := base + "/branches/" + url.PathEscape(repo.DefaultBranch)
	if err := api.Call("GET", branchPath, nil, &branch); err != nil {
		return err
	}
	if branch.Name != repo.DefaultBranch || !commitPattern.MatchString(branch.Commit.SHA) {
		return errors.New("SHARED_EXECUTION_DEFAULT_BRANCH_UNAVAILABLE")
	}
	if !branch.Protected {
		return errors.New("SHARED_EXECUTION_BRANCH_UNPROTECTED")
	}
	var workflow struct {
		ID    int64  `json:"id"`
		Path  string `json:"path"`
		State string `json:"state"`
	}
	endpoint := base + "/actions/workflows/shared-runner.yml"
	if err := api.Call("GET", endpoint, nil, &workflow); err != nil {
		if err.Error() == "NOT_FOUND" {
			return errors.New("SHARED_EXECUTION_WORKFLOW_NOT_INSTALLED")
		}
		return err
	}
	if workflow.ID <= 0 || workflow.Path != sharedRunnerWorkflow ||
		workflow.State != "active" {
		return errors.New("SHARED_EXECUTION_WORKFLOW_UNAVAILABLE")
	}
	current, _, err := content(api, repo.FullName, sharedRunnerWorkflow, branch.Commit.SHA)
	if err != nil {
		return err
	}
	if string(current) != string(expected) {
		return errors.New("SHARED_EXECUTION_WORKFLOW_MISMATCH")
	}
	return nil
}

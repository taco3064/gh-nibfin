package cli

import (
	"encoding/json"
	"errors"
)

const sharedRunnerWorkflow = ".github/workflows/shared-runner.yml"

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
			return errors.New("PINNED_CORE_SHARED_DESTINATION_UNSUPPORTED")
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
		return errors.New("PINNED_CORE_SHARED_DESTINATION_UNSUPPORTED")
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

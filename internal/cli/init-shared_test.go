package cli

import "testing"

func TestInitSharedInstallsWorkflowOnlyInExecutionRepository(t *testing.T) {
	api := initFixture()
	pin := Pins().Core
	source := "repos/" + pin.Repository + "/contents/"
	project := api.data["repos/example/project"].(Repository)
	_, err := planInitWithExecution(api, project.FullName, project.FullName)
	if err == nil || err.Error() != "PINNED_CORE_SHARED_DESTINATION_UNSUPPORTED" {
		t.Fatalf("old Core must be rejected, got %v", err)
	}
	api.data[source+"src/shared-runner-contract.json?ref="+pin.Commit] = encoded([]byte(
		"{\"schemaVersion\":1,\"destinationVariable\":\"NIBFIN_SHARED_RUNNER_DESTINATION\"," +
			"\"workflowPath\":\".github/workflows/shared-runner.yml\"}"))
	workflow := []byte("name: Verified shared workflow\n")
	api.data[source+sharedRunnerWorkflow+"?ref="+pin.Commit] = encoded(workflow)
	plan, err := planInitWithExecution(api, project.FullName, project.FullName)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Execution.RepositoryID != project.ID || len(plan.Files) != 5 {
		t.Fatalf("unexpected execution plan: %+v", plan)
	}
	execution := project
	execution.ID, execution.FullName = 20, "example/execution"
	api.data["repos/"+execution.FullName] = execution
	path := "repos/" + execution.FullName + "/contents/" + sharedRunnerWorkflow + "?ref=main"
	api.data[path] = encoded(workflow)
	plan, err = planInitWithExecution(api, project.FullName, execution.FullName)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 4 || plan.Execution.RepositoryID != execution.ID {
		t.Fatalf("peer must reuse the execution workflow: %+v", plan)
	}
	api.data[path] = encoded([]byte("name: Replaced workflow\n"))
	_, err = planInitWithExecution(api, project.FullName, execution.FullName)
	if err == nil || err.Error() != "SHARED_EXECUTION_WORKFLOW_MISMATCH" {
		t.Fatalf("changed workflow must be rejected, got %v", err)
	}
	if len(api.writes) != 0 {
		t.Fatal("planning must not write to GitHub")
	}
}

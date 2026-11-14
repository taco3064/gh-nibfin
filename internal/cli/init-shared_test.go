package cli

import (
	"net/url"
	"strings"
	"testing"
)

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
	base := "repos/" + execution.FullName
	head := strings.Repeat("d", 40)
	branch := map[string]any{
		"name": "main", "protected": true, "commit": map[string]string{"sha": head},
	}
	api.data[base+"/branches/main"] = branch
	workflowPath := base + "/actions/workflows/shared-runner.yml"
	api.data[workflowPath] = map[string]any{
		"id": 42, "path": sharedRunnerWorkflow, "state": "active",
	}
	path := base + "/contents/" + sharedRunnerWorkflow + "?ref=" + head
	api.data[path] = encoded(workflow)
	plan, err = planInitWithExecution(api, project.FullName, execution.FullName)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 4 || plan.Execution.RepositoryID != execution.ID {
		t.Fatalf("peer must reuse the execution workflow: %+v", plan)
	}
	branch["protected"] = false
	_, err = planInitWithExecution(api, project.FullName, execution.FullName)
	if err == nil || err.Error() != "SHARED_EXECUTION_BRANCH_UNPROTECTED" {
		t.Fatalf("unprotected execution branch must be rejected, got %v", err)
	}
	branch["protected"] = true
	api.data[path] = encoded([]byte("name: Replaced workflow\n"))
	_, err = planInitWithExecution(api, project.FullName, execution.FullName)
	if err == nil || err.Error() != "SHARED_EXECUTION_WORKFLOW_MISMATCH" {
		t.Fatalf("changed workflow must be rejected, got %v", err)
	}
	api.data[path] = encoded(workflow)
	api.data[workflowPath] = map[string]any{
		"id": 42, "path": sharedRunnerWorkflow, "state": "disabled_manually",
	}
	_, err = planInitWithExecution(api, project.FullName, execution.FullName)
	if err == nil || err.Error() != "SHARED_EXECUTION_WORKFLOW_UNAVAILABLE" {
		t.Fatalf("disabled workflow must be rejected, got %v", err)
	}
	if len(api.writes) != 0 {
		t.Fatal("planning must not write to GitHub")
	}
}

func TestSharedInitReusesExactOpenPR(t *testing.T) {
	api := initFixture()
	base := "repos/example/project"
	head := strings.Repeat("a", 40)
	sha := strings.Repeat("c", 40)
	branch := "nibfin/init-" + head[:12]
	repo := api.data[base].(Repository)
	plan := InitPlan{Repository: repo, Head: head,
		Execution: &SharedExecution{Repository: repo.FullName, RepositoryID: repo.ID},
		Files:     []Change{{Path: sharedRunnerWorkflow, Content: "name: Shared\n"}}}
	ref := reference{Ref: "refs/heads/" + branch}
	ref.Object.SHA = sha
	api.data[base+"/git/ref/heads/"+url.PathEscape(branch)] = ref
	compare := base + "/compare/" + head + "..." + sha
	api.data[compare] = map[string]any{"ahead_by": 1, "status": "ahead",
		"files": []map[string]string{{"filename": sharedRunnerWorkflow, "status": "added"}}}
	file := base + "/contents/" + sharedRunnerWorkflow + "?ref=" + sha
	api.data[file] = encoded([]byte(plan.Files[0].Content))
	pull := installPR{Number: 8, State: "open",
		URL: "https://github.com/example/project/pull/8"}
	pull.Head.Ref, pull.Head.SHA, pull.Base.Ref = branch, sha, "main"
	query := "?state=all&per_page=2&head=" + url.QueryEscape("example:"+branch)
	api.data[base+"/pulls"+query] = []installPR{pull}
	api.data[base+"/pulls/8"] = pull
	resumed, err := createInstallBranch(api, base, plan)
	if err != nil {
		t.Fatal(err)
	}
	got, err := createInstallPR(api, plan, resumed)
	if err != nil || got != pull.URL || len(api.writes) != 0 {
		t.Fatalf("expected existing PR without writes, got %q, %v", got, err)
	}
	closed := pull
	closed.State = "closed"
	api.data[base+"/pulls"+query] = []installPR{closed}
	if _, err := createInstallPR(api, plan, resumed); err == nil || len(api.writes) != 0 {
		t.Fatal("a closed installation PR must stop reuse before writing")
	}
	api.data[base+"/pulls"+query] = []installPR{}
	openQuery := "?state=open&head=" + url.QueryEscape("example:"+branch)
	api.data[base+"/pulls"+openQuery] = []installPR{pull}
	got, err = createInstallPR(api, plan, resumed)
	if err != nil || got != pull.URL || len(api.writes) != 1 {
		t.Fatalf("a verified branch without a PR must resume once, got %q, %v", got, err)
	}
	api.data[file] = encoded([]byte("name: Replaced\n"))
	if _, err := createInstallBranch(api, base, plan); err == nil {
		t.Fatal("modified installation branch must be refused")
	}
}

func TestSharedWakeReadbackDistinguishesMissingAndConfigured(t *testing.T) {
	api := initFixture()
	execution := SharedExecution{Repository: "example/execution", RepositoryID: 20}
	const repo = "example/project"
	const base = "repos/" + repo + "/actions/"
	wantStatus := func(want string) {
		t.Helper()
		if got := sharedWakeConfiguration(api, repo, execution); got != want {
			t.Fatalf("shared Wake status: got %s, want %s", got, want)
		}
	}
	wantStatus("configuration_required")
	pool := map[string]string{"name": "NIBFIN_SHARED_RUNNER_POOL", "value": "true"}
	api.data[base+"variables/NIBFIN_SHARED_RUNNER_POOL"] = pool
	destination := map[string]string{
		"name":  "NIBFIN_SHARED_RUNNER_DESTINATION",
		"value": `{"repository":"example/execution","repositoryId":21}`,
	}
	secretName := "NIBFIN_WAKE_TOKEN"
	api.data[base+"secrets/"+secretName] = map[string]string{"name": secretName}
	api.data[base+"variables/NIBFIN_SHARED_RUNNER_DESTINATION"] = destination
	wantStatus("configuration_required")
	destination["value"] = `{"repository":"example/execution","repositoryId":20}`
	wantStatus("configured_unverified")
	if len(api.writes) != 0 {
		t.Fatal("Wake readback must not activate shared dispatch")
	}
}

type sharedWakeWriteFixture struct{ *fakeAPI }

func (f sharedWakeWriteFixture) Call(method, path string, input, output any) error {
	if method == "POST" {
		value := input.(actionsVariable)
		f.data[path+"/"+value.Name] = value
	}
	return f.fakeAPI.Call(method, path, input, output)
}

func TestPrepareSharedWakeDestinationPreservesInactiveRouting(t *testing.T) {
	api := sharedWakeWriteFixture{initFixture()}
	repo := "example/project"
	base := "repos/" + repo + "/actions/variables"
	pool := base + "/" + sharedPoolVariable
	execution := SharedExecution{Repository: "example/execution", RepositoryID: 20}
	api.data[pool] = actionsVariable{Name: sharedPoolVariable, Value: "true"}
	if _, err := prepareSharedWakeDestination(api, repo, execution); err == nil ||
		len(api.writes) != 0 {
		t.Fatal("must not modify an active route")
	}
	delete(api.data, pool)
	status, err := prepareSharedWakeDestination(api, repo, execution)
	if err != nil || status != "prepared" || len(api.writes) != 1 {
		t.Fatalf("expected destination readback despite lost response: %s, %v", status, err)
	}
	status, err = prepareSharedWakeDestination(api, repo, execution)
	if err != nil || status != "unchanged" || len(api.writes) != 1 {
		t.Fatalf("repeat must reuse the destination: %s, %v", status, err)
	}
	execution.RepositoryID++
	if _, err := prepareSharedWakeDestination(api, repo, execution); err == nil ||
		len(api.writes) != 1 {
		t.Fatal("must preserve existing routing")
	}
	if _, ok := api.data[pool]; ok {
		t.Fatal("preparation must not turn on shared dispatch")
	}
}

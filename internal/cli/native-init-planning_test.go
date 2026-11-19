package cli

import (
	"strings"
	"testing"
)

func addNativeInitSharedCapability(f *migrationAPI, host sharedHost) {
	source := "repos/" + f.old.Core.Repository + "/contents/"
	contract := `{"schemaVersion":1,` +
		`"destinationVariable":"NIBFIN_SHARED_RUNNER_DESTINATION",` +
		`"workflowPath":".github/workflows/shared-runner.yml"}`
	f.data[source+"src/shared-runner-contract.json?ref="+f.old.Core.Commit] =
		encoded([]byte(contract))
	workflow := []byte("name: Verified current shared workflow\n")
	f.data[source+sharedRunnerWorkflow+"?ref="+f.old.Core.Commit] = encoded(workflow)
	base, head := "repos/"+host.Repository, strings.Repeat("a", 40)
	f.data[base+"/contents/"+sharedRunnerWorkflow+"?ref="+head] = encoded(workflow)
	f.data[base+"/branches/main"] = map[string]any{
		"name": "main", "protected": true, "commit": map[string]string{"sha": head}}
	f.data[base+"/actions/workflows/shared-runner.yml"] = map[string]any{
		"id": 42, "path": sharedRunnerWorkflow, "state": "active"}
}

func TestNativeExistingInitUsesCurrentSourcesAndRecognizesHistoricalRoute(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		f, host, _, _ := readyMigrationFixture(t)
		if artifact {
			f, host, _, _, _ = migrationFixture(t)
		}
		addNativeInitSharedCapability(f, host)
		plan, err := planNativeHostInit(f, "example/project", host.Repository, host)
		if err != nil || len(plan.Files) != 0 || plan.Sources == nil ||
			!sameProducerSources(*plan.Sources, f.old) || len(f.writes) != 0 {
			t.Fatal("existing init lost actual maintained sources", plan, err, f.writes)
		}
		if artifact != (plan.IssueRecipe == "issue-managed") {
			t.Fatal("historical route was replaced before original upgrade", plan)
		}
	}
}

func TestNativeNewPeerInitUsesExecutionSourcesWithoutRevivingArtifactRoute(t *testing.T) {
	f, host, _, _ := readyMigrationFixture(t)
	addNativeInitSharedCapability(f, host)
	peer := f.data[upgradeRepo].(Repository)
	peer.ID, peer.FullName = 11, "example/new-peer"
	f.data["repos/"+peer.FullName] = peer
	f.data["repos/"+peer.FullName+"/git/ref/heads/main"] = map[string]any{
		"ref": "refs/heads/main", "object": map[string]string{
			"sha": strings.Repeat("d", 40)}}
	plan, err := planNativeHostInit(f, peer.FullName, host.Repository, host)
	if err != nil || plan.Sources == nil ||
		!sameProducerSources(*plan.Sources, f.old) || plan.IssueRecipe != "issue" ||
		len(plan.Files) == 0 || len(f.writes) != 0 {
		t.Fatal("new peer did not select current native recipe", plan, err, f.writes)
	}
	for _, file := range plan.Files {
		if strings.Contains(file.Content, "issue-managed.yml") {
			t.Fatal("new peer revived withdrawn artifact caller", file.Path)
		}
	}
	f.denyRead = upgradeRepo + "/git/ref/heads/main"
	if _, err := planNativeHostInit(f, peer.FullName, host.Repository, host); err == nil ||
		len(f.writes) != 0 {
		t.Fatal("unavailable execution source authorized fallback", err, f.writes)
	}
}

func TestNativeInitRejectsMissingCapabilityAndChangedExecutionWorkflow(t *testing.T) {
	for _, missing := range []bool{false, true} {
		f, host, _, _ := readyMigrationFixture(t)
		addNativeInitSharedCapability(f, host)
		if missing {
			delete(f.data, "repos/"+f.old.Core.Repository+
				"/contents/src/shared-runner-contract.json?ref="+f.old.Core.Commit)
		} else {
			f.data[upgradeRepo+"/contents/"+sharedRunnerWorkflow+"?ref="+
				strings.Repeat("a", 40)] = encoded([]byte("foreign shared workflow"))
		}
		_, err := planNativeHostInit(f, host.Repository, host.Repository, host)
		if err == nil || len(f.writes) != 0 {
			t.Fatal("invalid current capability acquired a native plan", err, f.writes)
		}
	}
}

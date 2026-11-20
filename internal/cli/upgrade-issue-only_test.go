package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestUpgradeRecognizesAdoptedIssueOnlyExecutionWithoutLegacyChanges(t *testing.T) {
	api := upgradeFixture(t, false, true)
	const root = "example/project"
	const wake = ".github/workflows/nibfin-wake.yml"
	const stage = ".github/workflows/nibfin-stage.yml"
	head := strings.Repeat("a", 40)
	commitPath := upgradeRepo + "/git/commits/" + head
	commit := api.data[commitPath].(upgradeCommit)
	delete(api.baseEntries, wake)
	delete(api.baseEntries, stage)
	commit.Tree.SHA = fixtureTree(api.fakeAPI, api.baseEntries)
	api.data[commitPath] = commit
	api.denyRead = upgradeRepo + "/contents/" + wake + "?ref=" + head
	recipe := handoffTestRecipe()
	target := upgradeSource{Producers: api.target, maintained: true,
		issueOnly: &issueOnlyUpgradeBinding{recipe.execution, 42}}
	base, err := planNativeUpgradeTarget(api, root, "latest", target)
	if err != nil || !base.issueOnly || base.MemberID != 42 || len(base.Changes) != 0 {
		t.Fatal("observed Issue-only layout failed source selection", base, err)
	}
	target.handoff = &recipe
	plan, err := planNativeUpgradeTarget(api, root, "latest", target)
	if err != nil {
		t.Fatal(err)
	}
	wanted := []upgradeChange{
		{recipe.execPath, "added", strings.ReplaceAll(recipe.executor, "MEMBER_ID", "42")},
		{recipe.noticePath, "added", recipe.notice},
	}
	if !reflect.DeepEqual(plan.Changes, wanted) {
		t.Fatal("bootstrap touched more than its two maintained workflows", plan.Changes)
	}
	if err := revalidateUpgrade(api, plan, "latest"); err != nil || len(api.writes) != 0 {
		t.Fatal("verified Issue-only preview could not be read back", err, api.writes)
	}
	wrong := target
	wrong.issueOnly = &issueOnlyUpgradeBinding{SharedExecution{root, 1234}, 42}
	if _, err := planNativeUpgradeTarget(api, root, "latest", wrong); err == nil {
		t.Fatal("repository without exact adopted identity bypassed legacy checks")
	}
	edited := api.installed[managedIssueWorkflow] + "# unrelated edit\n"
	entry := api.baseEntries[managedIssueWorkflow]
	entry.SHA = blobHash([]byte(edited))
	api.baseEntries[managedIssueWorkflow] = entry
	api.data[upgradeRepo+"/contents/"+managedIssueWorkflow+"?ref="+head] =
		encoded([]byte(edited))
	commit.Tree.SHA = fixtureTree(api.fakeAPI, api.baseEntries)
	api.data[commitPath] = commit
	if _, err := planNativeUpgradeTarget(api, root, "latest", target); err == nil ||
		err.Error() != "INSTALLED_ISSUE_CALLER_NOT_RECOGNIZED" {
		t.Fatal("unrecognized protected Issue workflow was accepted", err)
	}
}

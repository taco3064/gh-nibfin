package cli

import (
	"strings"
	"testing"
)

func TestNativeCurrentTargetReusesPublishedReleaseProposalWithoutRewritingIdentity(
	t *testing.T) {
	f := upgradeFixture(t, true, true, true)
	f.data[pullListPath(upgradeBranchName)] = []upgradePull{}
	original, err := planNativeUpgrade(f, "example/project", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyUpgrade(f, original, "latest"); err != nil {
		t.Fatal(err)
	}
	pull := f.data[upgradeRepo+"/pulls/9"].(upgradePull)
	message := f.data[upgradeRepo+"/git/commits/"+pull.Head.SHA].(upgradeCommit).Message
	current := upgradeSource{Kind: "installed-current", Producers: f.target,
		Execution: &SharedExecution{"example/project", 10}}
	plan, err := planNativeUpgradeTarget(f, "example/project", "latest", current)
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.writes)
	result, err := applyUpgrade(f, plan, "latest")
	if err != nil || result.PR == "" || len(f.writes) != before {
		t.Fatal("same sources published a duplicate proposal", result, err, f.writes[before:])
	}
	if !strings.Contains(message, "Plan: "+original.ID) ||
		f.data[upgradeRepo+"/git/commits/"+pull.Head.SHA].(upgradeCommit).Message != message {
		t.Fatal("proposal release identity was rewritten")
	}
	advanceUpgradeBase(t, f, plan)
	advanced, err := replanUpgrade(f, plan, "latest", "")
	if err != nil || advanced.Target.Kind != "installed-current" ||
		advanced.DesiredRecipe != "issue" || !sameUpgradeTarget(advanced.Target, plan.Target) {
		t.Fatal("test main advance lost the selected native operation", advanced, err)
	}
	rebased, err := applyUpgrade(f, advanced, "latest")
	if err != nil || rebased.PR != result.PR {
		t.Fatal("original publisher did not rebase the retained proposal", err)
	}
	if countWrite(f.writes, "POST "+upgradeRepo+"/pulls") != 1 ||
		countWrite(f.writes, "POST graphql") != 1 {
		t.Fatal("rebase duplicated proposal publication", f.writes)
	}
	pull = f.data[upgradeRepo+"/pulls/9"].(upgradePull)
	commit := f.data[upgradeRepo+"/git/commits/"+pull.Head.SHA].(upgradeCommit)
	if commit.Message != message {
		t.Fatal("native rebase rewrote original release/plan metadata")
	}
	entries, err := readUpgradeTree(f, upgradeRepo, commit.Tree.SHA)
	if err != nil || entries["main-change.txt"].SHA !=
		blobHash([]byte("unrelated new main content")) {
		t.Fatal("native rebase lost unrelated main content", entries, err)
	}
	before = len(f.writes)
	if _, err := applyUpgrade(f, advanced, "latest"); err != nil || len(f.writes) != before {
		t.Fatal("confirmed native rebase was repeated", err, f.writes[before:])
	}
}

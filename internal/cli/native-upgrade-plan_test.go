package cli

import (
	"strings"
	"testing"
)

func TestNativeUpgradePreservesExistingNativeProposalIdentity(t *testing.T) {
	f := upgradeFixture(t, true, true, true)
	// The fixture defaults to an artifact caller; install the recognized native
	// recipe while retaining the same ordinary source upgrade and original member.
	recipes, err := callerRecipes(f, f.old.Core)
	if err != nil {
		t.Fatal(err)
	}
	body := templatesForRecipe(f.old, 42, recipes, "issue")[managedIssueWorkflow]
	f.installed[managedIssueWorkflow] = body
	f.baseEntries[managedIssueWorkflow] = upgradeEntry{Path: managedIssueWorkflow,
		Mode: "100644", Type: "blob", SHA: blobHash([]byte(body))}
	f.data[upgradeRepo+"/contents/"+managedIssueWorkflow+"?ref="+
		strings.Repeat("a", 40)] = encoded([]byte(body))
	commit := f.data[upgradeRepo+"/git/commits/"+strings.Repeat("a", 40)].(upgradeCommit)
	commit.Tree.SHA = fixtureTree(f.fakeAPI, f.baseEntries)
	f.data[upgradeRepo+"/git/commits/"+commit.SHA] = commit
	previous, err := planUpgrade(f, "example/project", "latest")
	if err != nil {
		t.Fatal(err)
	}
	next, err := planNativeUpgrade(f, "example/project", "latest")
	if err != nil || previous.ID != next.ID || !sameUpgradeIntent(previous, next) ||
		next.DesiredRecipe != "" {
		t.Fatal("unchanged native intent broke historical PR attribution", err, next)
	}
}

func TestNativeUpgradeExplicitRecipeSurvivesHistoricalReplanning(t *testing.T) {
	f := upgradeFixture(t, true, true, true)
	plan, err := planNativeUpgrade(f, "example/project", "latest")
	if err != nil || plan.DesiredRecipe != "issue" || plan.ManagedIssue {
		t.Fatal("artifact transition changed historical managed proposal semantics", err)
	}
	parent, err := replanUpgrade(f, plan, "latest", plan.Base)
	if err != nil || plan.ID != parent.ID || !sameUpgradeIntent(plan, parent) {
		t.Fatal("native transition was lost while reconstructing proposal", err)
	}
	for _, change := range plan.Changes {
		if change.Path == managedIssueWorkflow && strings.Contains(change.Content,
			"mode: managed") {
			t.Fatal("native migration replanned toward artifact transport")
		}
	}
}

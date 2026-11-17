package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedRecipePlanningIsExplicitAndRejectsLegacyReplacement(t *testing.T) {
	api := initFixture()
	recipes := recipeFixture()
	recipes["issue-managed"] = []string{"name: managed CORE_COMMIT", "mode: managed", ""}
	pin := Pins().Core
	api.data["repos/"+pin.Repository+"/contents/src/caller-recipes.json?ref="+pin.Commit] =
		encoded(jsonRelay(recipes))
	legacy, err := planInit(api, "example/project")
	if err != nil {
		t.Fatal(err)
	}
	managed, err := planInitRecipe(api, "example/project", "", "issue-managed")
	if err != nil || managed.IssueRecipe != "issue-managed" {
		t.Fatal(managed, err)
	}
	for _, file := range managed.Files {
		if file.Path != managedIssueWorkflow {
			continue
		}
		if !strings.Contains(file.Content, "mode: managed") {
			t.Fatal("missing managed source selection")
		}
	}
	for _, file := range legacy.Files {
		api.data["repos/example/project/contents/"+file.Path+"?ref="+legacy.Head] =
			encoded([]byte(file.Content))
	}
	_, err = planInitRecipe(api, "example/project", "", "issue-managed")
	if err == nil || err.Error() != "MANAGED_CALLER_REQUIRES_EXPLICIT_UPGRADE" {
		t.Fatalf("legacy caller must not be silently replaced: %v", err)
	}
	if len(api.writes) != 0 {
		t.Fatal(api.writes)
	}
}

func TestManagedIntentRecipeCompatibility(t *testing.T) {
	original := InitPlan{IssueRecipe: "issue"}
	current := InitPlan{IssueRecipe: "issue-managed"}
	if !managedIntentRecipeCompatible(original, current) {
		t.Fatal("verified merged upgrade must permit adoption refresh")
	}
	current.Files = []Change{{Path: managedIssueWorkflow, Content: "not yet installed"}}
	if managedIntentRecipeCompatible(original, current) {
		t.Fatal("unmerged upgrade must retain its separate intent")
	}
}

func TestUpgradePreservesManagedRecipe(t *testing.T) {
	f := upgradeFixture(t, true, true, true)
	plan, err := planUpgrade(f, "example/project", "latest")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range plan.Changes {
		if change.Path == managedIssueWorkflow {
			found = strings.Contains(change.Content, "mode: managed")
		}
	}
	if !found {
		t.Fatal("upgrade lost managed transport")
	}
	// An unknown edited caller must not normalize into either trusted recipe.
	recipes, err := callerRecipes(f, f.old.Core)
	if err != nil {
		t.Fatal(err)
	}
	if recognizeIssueRecipe(f.old, 42, recipes,
		f.installed[managedIssueWorkflow]+"# edit\n") != "" {
		t.Fatal("edited caller recognized")
	}
}

func jsonRelay(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func TestManagedCallerProposalRequiresExplicitInactiveRoute(t *testing.T) {
	for _, active := range []bool{false, true} {
		f := upgradeFixture(t, true, true, true)
		recipes, err := callerRecipes(f, f.old.Core)
		if err != nil {
			t.Fatal(err)
		}
		// The producer supports both recipes; this installation selected legacy.
		body := templates(f.old, 42, recipes)[managedIssueWorkflow]
		entry := f.baseEntries[managedIssueWorkflow]
		entry.SHA = blobHash([]byte(body))
		f.baseEntries[managedIssueWorkflow] = entry
		head := strings.Repeat("a", 40)
		f.data[upgradeRepo+"/contents/"+managedIssueWorkflow+"?ref="+head] =
			encoded([]byte(body))
		commit := f.data[upgradeRepo+"/git/commits/"+head].(upgradeCommit)
		commit.Tree.SHA = fixtureTree(f.fakeAPI, f.baseEntries)
		f.data[upgradeRepo+"/git/commits/"+head] = commit
		if active {
			f.data[upgradeRepo+"/actions/variables/"+sharedPoolVariable] =
				actionsVariable{sharedPoolVariable, "true"}
		}
		plan, err := planUpgrade(f, "example/project", "latest", true)
		if active {
			if err == nil {
				t.Fatal("active legacy route must remain intact")
			}
			continue
		}
		if err != nil || !plan.ManagedIssue {
			t.Fatal(plan, err)
		}
		found := false
		for _, change := range plan.Changes {
			found = found || change.Path == managedIssueWorkflow &&
				strings.Contains(change.Content, "mode: managed")
		}
		if !found || len(f.writes) != 0 {
			t.Fatal("explicit migration must only propose a verified caller")
		}
	}
}

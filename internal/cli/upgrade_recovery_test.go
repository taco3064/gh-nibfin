package cli

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestUpgradeRetainedProposalRebasesSamePRAndReusesConfirmedResult(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "response", true: "lost-response"}[lost],
			func(t *testing.T) {
				f := upgradeFixture(t, false, true)
				original := fixtureUpgradePlan(t, f)
				if _, err := applyUpgrade(f, original, "latest"); err != nil {
					t.Fatal(err)
				}
				current := advanceUpgradeBase(t, f, original)
				writes := len(f.writes)
				if lost {
					f.lost = "graphql"
				}
				result, err := applyUpgrade(f, current, "latest")
				if err != nil || result.PR != "https://github.com/example/project/pull/9" {
					t.Fatal(result, err)
				}
				if len(f.writes) != writes+1 || f.writes[writes] != "POST graphql" {
					t.Fatal("expected one native rebase on the retained PR", f.writes)
				}
				pull := f.data[upgradeRepo+"/pulls/9"].(upgradePull)
				commit := f.data[upgradeRepo+"/git/commits/"+pull.Head.SHA].(upgradeCommit)
				entries, err := readUpgradeTree(f, upgradeRepo, commit.Tree.SHA)
				if err != nil || entries["main-change.txt"].SHA !=
					blobHash([]byte("unrelated new main content")) {
					t.Fatal("rebase lost unrelated main change", entries, err)
				}
				count := len(f.writes)
				if _, err := applyUpgrade(f, current, "latest"); err != nil {
					t.Fatal("current rebased proposal must be recognized", err)
				}
				if len(f.writes) != count {
					t.Fatal("replayed confirmed rebase", f.writes)
				}
			})
	}
}

func TestUpgradeRetainedBranchPublishesAfterConfirmedAbsence(t *testing.T) {
	for _, behind := range []bool{false, true} {
		f := upgradeFixture(t, false, true)
		plan := fixtureUpgradePlan(t, f)
		f.lost = upgradeRepo + "/git/refs"
		f.denyAfterWrite = upgradeRepo + "/git/ref/heads/" + url.PathEscape(upgradeBranchName)
		if _, err := applyUpgrade(f, plan, "latest"); err == nil {
			t.Fatal("fixture must interrupt after branch creation")
		}
		f.denyRead = ""
		if behind {
			plan = advanceUpgradeBase(t, f, plan)
		}
		writes := len(f.writes)
		result, err := applyUpgrade(f, plan, "latest")
		if err != nil || result.PR == "" {
			t.Fatal(result, err)
		}
		expected := []string{"POST " + upgradeRepo + "/pulls"}
		if behind {
			expected = append(expected, "POST graphql")
		}
		observed, _ := json.Marshal(f.writes[writes:])
		want, _ := json.Marshal(expected)
		if string(observed) != string(want) {
			t.Fatal(string(observed), string(want))
		}
	}
}

func TestUpgradeRebaseUnfinishedOrConflictingCanResume(t *testing.T) {
	for _, mode := range []string{"conflict", "missing-data", "unfinished"} {
		t.Run(mode, func(t *testing.T) {
			f := upgradeFixture(t, false, true)
			plan := fixtureUpgradePlan(t, f)
			if _, err := applyUpgrade(f, plan, "latest"); err != nil {
				t.Fatal(err)
			}
			current := advanceUpgradeBase(t, f, plan)
			head := f.data[upgradeRepo+"/pulls/9"].(upgradePull).Head.SHA
			f.rebaseResult = mode
			result, err := applyUpgrade(f, current, "latest")
			if err == nil || result.PR != "" || !strings.Contains(err.Error(), "REBASE") {
				t.Fatal(result, err)
			}
			if f.data[upgradeRepo+"/pulls/9"].(upgradePull).Head.SHA != head {
				t.Fatal("failed operation changed proposal")
			}
			f.rebaseResult = ""
			if result, err := applyUpgrade(f, current, "latest"); err != nil || result.PR == "" {
				t.Fatal("unfinished operation did not resume", result, err)
			}
		})
	}
}

func TestUpgradeLostRebaseObservationResumesWithoutRepeatingEffect(t *testing.T) {
	f := upgradeFixture(t, false, true)
	plan := fixtureUpgradePlan(t, f)
	if _, err := applyUpgrade(f, plan, "latest"); err != nil {
		t.Fatal(err)
	}
	current := advanceUpgradeBase(t, f, plan)
	f.lost = "graphql"
	f.denyAfterWrite = upgradeRepo + "/git/ref/heads/" + url.PathEscape(upgradeBranchName)
	result, err := applyUpgrade(f, current, "latest")
	if err == nil || result.PR != "" {
		t.Fatal(result, err)
	}
	writes := len(f.writes)
	if _, err := applyUpgrade(f, current, "latest"); err == nil {
		t.Fatal("unavailable observation must remain pending")
	}
	f.denyRead = ""
	if result, err := applyUpgrade(f, current, "latest"); err != nil || result.PR == "" {
		t.Fatal(result, err)
	}
	if len(f.writes) != writes {
		t.Fatal("replayed confirmed rebase", f.writes)
	}
}

func TestUpgradeIncompleteProposalReadsNeverAuthorizePublication(t *testing.T) {
	for _, kind := range []string{"denied", "null", "object", "malformed", "full-page"} {
		t.Run(kind, func(t *testing.T) {
			f := upgradeFixture(t, false, true)
			plan := fixtureUpgradePlan(t, f)
			if _, err := applyUpgrade(f, plan, "latest"); err != nil {
				t.Fatal(err)
			}
			path := pullListPath(upgradeBranchName)
			switch kind {
			case "denied":
				f.denyRead = path
			case "null":
				f.data[path] = nil
			case "object":
				f.data[path] = map[string]any{}
			case "malformed":
				f.data[path] = []map[string]any{{"state": "open"}}
			case "full-page":
				pulls := make([]upgradePull, 100)
				for i := range pulls {
					pulls[i].Number = i + 1
				}
				f.data[path] = pulls
			}
			writes := len(f.writes)
			if result, err := applyUpgrade(f, plan, "latest"); err == nil || result.PR != "" {
				t.Fatal(result, err)
			}
			if len(f.writes) != writes {
				t.Fatal("incomplete observation authorized mutation", f.writes)
			}
		})
	}
}

func TestUpgradeRebaseGuardRejectsConcurrentHeadEdit(t *testing.T) {
	f := upgradeFixture(t, false, true)
	plan := fixtureUpgradePlan(t, f)
	if _, err := applyUpgrade(f, plan, "latest"); err != nil {
		t.Fatal(err)
	}
	current := advanceUpgradeBase(t, f, plan)
	path := upgradeRepo + "/git/ref/heads/" + url.PathEscape(upgradeBranchName)
	f.beforeWrite = func(endpoint string) {
		if endpoint == "graphql" {
			ref := f.data[path].(reference)
			ref.Object.SHA = strings.Repeat("e", 40)
			f.data[path] = ref
		}
	}
	result, err := applyUpgrade(f, current, "latest")
	if err == nil || result.PR != "" {
		t.Fatal(result, err)
	}
	if f.data[path].(reference).Object.SHA != strings.Repeat("e", 40) {
		t.Fatal("competing edit overwritten")
	}
}

func TestUpgradeCompletedInstallationIgnoresRetainedProposal(t *testing.T) {
	f := upgradeFixture(t, false, true)
	plan := fixtureUpgradePlan(t, f)
	if _, err := applyUpgrade(f, plan, "latest"); err != nil {
		t.Fatal(err)
	}
	pull := f.data[upgradeRepo+"/pulls/9"].(upgradePull)
	ref := reference{Ref: "refs/heads/main"}
	ref.Object.SHA = pull.Head.SHA
	f.data[upgradeRepo+"/git/ref/heads/main"] = ref
	recipes, err := upgradeRecipes(f, f.target)
	if err != nil {
		t.Fatal(err)
	}
	for path, text := range templatesForRecipe(f.target, plan.MemberID, recipes, "issue") {
		f.data[upgradeRepo+"/contents/"+path+"?ref="+pull.Head.SHA] = encoded([]byte(text))
	}
	current, err := planUpgrade(f, "example/project", "latest")
	if err != nil {
		t.Fatal(err)
	}
	writes := len(f.writes)
	result, err := applyUpgrade(f, current, "latest")
	if err != nil || result.Status != "no_change" || len(f.writes) != writes {
		t.Fatal(result, err, f.writes)
	}
}

func TestUpgradeRetainedSemanticEditsNeverAuthorizeRebase(t *testing.T) {
	for _, kind := range []string{"extra-file", "managed-mode", "managed-bytes",
		"metadata", "foreign-parent", "incomplete-ancestry"} {
		t.Run(kind, func(t *testing.T) {
			f := upgradeFixture(t, false, true)
			original := fixtureUpgradePlan(t, f)
			if _, err := applyUpgrade(f, original, "latest"); err != nil {
				t.Fatal(err)
			}
			current := advanceUpgradeBase(t, f, original)
			refPath := upgradeRepo + "/git/ref/heads/" + url.PathEscape(upgradeBranchName)
			ref := f.data[refPath].(reference)
			commit := f.data[upgradeRepo+"/git/commits/"+ref.Object.SHA].(upgradeCommit)
			tree, err := readUpgradeTree(f, upgradeRepo, commit.Tree.SHA)
			if err != nil {
				t.Fatal(err)
			}
			managed := ".nibfin/producer-pins.json"
			switch kind {
			case "extra-file":
				tree["extra.txt"] = upgradeEntry{Path: "extra.txt", Type: "blob",
					Mode: "100644", SHA: blobHash([]byte("foreign edit"))}
			case "managed-mode":
				entry := tree[managed]
				entry.Mode = "100755"
				tree[managed] = entry
			case "managed-bytes":
				entry := tree[managed]
				entry.SHA = blobHash([]byte("changed source intent"))
				tree[managed] = entry
			case "metadata":
				commit.Message = "Unrelated proposal"
			case "foreign-parent":
				commit.Parents[0].SHA = strings.Repeat("e", 40)
			case "incomplete-ancestry":
				f.data[upgradeRepo+"/compare/"+current.Base+"..."+original.Base] =
					map[string]any{"status": "behind"}
			}
			commit.Tree.SHA = fixtureTree(f.fakeAPI, tree)
			f.data[upgradeRepo+"/git/commits/"+ref.Object.SHA] = commit
			writes := len(f.writes)
			if result, err := applyUpgrade(f, current, "latest"); err == nil || result.PR != "" {
				t.Fatal(result, err)
			}
			if len(f.writes) != writes {
				t.Fatal("changed proposal authorized a write", f.writes)
			}
		})
	}
}

func TestUpgradePublicationWithoutProviderEffectResumesNormalStage(t *testing.T) {
	f := upgradeFixture(t, false, true)
	plan := fixtureUpgradePlan(t, f)
	f.refuseWrite = upgradeRepo + "/pulls"
	if result, err := applyUpgrade(f, plan, "latest"); err == nil || result.PR != "" {
		t.Fatal(result, err)
	}
	f.refuseWrite = ""
	result, err := applyUpgrade(f, plan, "latest")
	if err != nil || result.PR == "" {
		t.Fatal("confirmed absence must resume publication", result, err)
	}
	writes := len(f.writes)
	if _, err := applyUpgrade(f, plan, "latest"); err != nil || len(f.writes) != writes {
		t.Fatal("confirmed effect must be reused", err, f.writes)
	}
}

func TestUpgradePublicContinuationKeepsPreviewReadOnly(t *testing.T) {
	f := upgradeFixture(t, false, true)
	original := fixtureUpgradePlan(t, f)
	app := App{API: f, Process: upgradeNoProcess{t}}
	args := []string{"upgrade", "--repo", "example/project"}
	if err := app.Run(append(args, "--apply"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	advanceUpgradeBase(t, f, original)
	writes := len(f.writes)
	if err := app.Run(args, &bytes.Buffer{}); err != nil || len(f.writes) != writes {
		t.Fatal("preview must remain read-only for a behind proposal", err, f.writes)
	}
	var out bytes.Buffer
	if err := app.Run(append(args, "--apply"), &out); err != nil {
		t.Fatal(out.String(), err)
	}
	if !strings.Contains(out.String(), "https://github.com/example/project/pull/9") ||
		len(f.writes) != writes+1 || f.writes[writes] != "POST graphql" {
		t.Fatal(out.String(), f.writes)
	}
}

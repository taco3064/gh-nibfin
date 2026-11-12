package cli

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestUpgradeRevalidatesEveryBoundInputBeforeWrites(t *testing.T) {
	for _, kind := range []string{"repository", "permission", "default-branch", "base",
		"managed-content", "target-tuple", "target-resolution", "source-unavailable"} {
		t.Run(kind, func(t *testing.T) {
			f := upgradeFixture(t, false, true)
			plan := fixtureUpgradePlan(t, f)
			repo := f.data[upgradeRepo].(Repository)
			switch kind {
			case "repository":
				repo.ID++
			case "permission":
				repo.Permissions["push"] = false
			case "default-branch":
				repo.DefaultBranch = "another"
			case "base":
				ref := reference{Ref: "refs/heads/main"}
				ref.Object.SHA = strings.Repeat("b", 40)
				f.data[upgradeRepo+"/git/ref/heads/main"] = ref
			case "managed-content":
				replaceInstalledFile(t, f, ".github/workflows/nibfin-wake.yml", "edited", "100644")
			case "target-tuple":
				setFixtureTarget(f, f.old)
			case "target-resolution":
				f.data["repos/"+officialCLI+"/git/ref/tags/v1.2.3"] = map[string]any{
					"ref": "refs/tags/v1.2.3", "object": map[string]string{
						"sha": strings.Repeat("6", 40), "type": "commit"}}
			case "source-unavailable":
				delete(f.data, "repos/"+officialCore+"/commits/"+f.target.Core.Commit)
			}
			f.data[upgradeRepo] = repo
			result, err := applyUpgrade(f, plan, "latest")
			if err == nil || result.Status != "pending" || len(f.writes) != 0 {
				t.Fatal(kind, result, err, f.writes)
			}
		})
	}
}

func TestUpgradeStopsSubsequentWritesWhenBaseOrPermissionChanges(t *testing.T) {
	for _, effect := range []string{"/git/trees", "/git/commits", "/git/refs", "/pulls"} {
		for _, kind := range []string{"base", "permission"} {
			f := upgradeFixture(t, false, true)
			plan := fixtureUpgradePlan(t, f)
			f.afterWrite = func(path string) {
				if path != upgradeRepo+effect {
					return
				}
				if kind == "base" {
					ref := reference{Ref: "refs/heads/main"}
					ref.Object.SHA = strings.Repeat("b", 40)
					f.data[upgradeRepo+"/git/ref/heads/main"] = ref
				} else {
					repo := f.data[upgradeRepo].(Repository)
					repo.Permissions["push"] = false
					f.data[upgradeRepo] = repo
				}
			}
			result, err := applyUpgrade(f, plan, "latest")
			if err == nil || result.PR != "" || result.Status != "pending" {
				t.Fatal(result, err)
			}
			if kind == "base" && err.Error() != "UPGRADE_BASE_CHANGED_PREVIEW_AGAIN" {
				t.Fatal("observed base change must be explicit", err)
			}
			if f.writes[len(f.writes)-1] != "POST "+upgradeRepo+effect {
				t.Fatal("mutation after stale plan", f.writes)
			}
		}
	}
}

func TestUpgradeUnknownEffectsReadBackBeforeRetry(t *testing.T) {
	for _, effect := range []string{"/git/trees", "/git/commits", "/git/refs", "/pulls"} {
		f := upgradeFixture(t, false, true)
		plan := fixtureUpgradePlan(t, f)
		tree := treeHash(changedUpgradeTree(plan), "")
		commit := upgradeCommitID(plan, tree)
		branch := upgradeBranchName
		f.lost = upgradeRepo + effect
		switch effect {
		case "/git/trees":
			f.denyAfterWrite = upgradeRepo + "/git/trees/" + tree + "?recursive=1"
		case "/git/commits":
			f.denyAfterWrite = upgradeRepo + "/git/commits/" + commit
		case "/git/refs":
			f.denyAfterWrite = upgradeRepo + "/git/ref/heads/" + url.PathEscape(branch)
		case "/pulls":
			f.denyAfterWrite = pullListPath(branch)
		}
		result, err := applyUpgrade(f, plan, "latest")
		if err == nil || result.Status != "pending" {
			t.Fatal(effect, result, err)
		}
		count := len(f.writes)
		if _, err := applyUpgrade(f, plan, "latest"); err == nil {
			t.Fatal("unresolved retry")
		}
		if len(f.writes) != count {
			t.Fatal("blind replay", f.writes)
		}
		// Once the exact effect is visible, reconciliation can continue safely.
		f.denyRead = ""
		result, err = applyUpgrade(f, plan, "latest")
		if effect == "/git/refs" {
			if err == nil || !strings.Contains(err.Error(), "PR_UNKNOWN") {
				t.Fatal(err)
			}
			if len(f.writes) != count {
				t.Fatal("existing branch cannot authorize a PR replay")
			}
		} else if err != nil || result.PR == "" {
			t.Fatal(effect, result, err)
		}
		counts := map[string]int{}
		for _, write := range f.writes {
			counts[write]++
		}
		for write, count := range counts {
			if count != 1 {
				t.Fatal("duplicate effect", write, count)
			}
		}
	}
}

func TestUpgradeConflictingBranchesAndPRsNeverAuthorizeReplay(t *testing.T) {
	for _, kind := range []string{"foreign-branch", "competing-ref", "foreign-pr",
		"multiple-prs", "closed-pr", "wrong-base", "wrong-url", "wrong-head",
		"wrong-parent", "tree-extra-path", "branch-changed", "wrong-base-sha"} {
		t.Run(kind, func(t *testing.T) {
			f := upgradeFixture(t, false, true)
			plan := fixtureUpgradePlan(t, f)
			branch := upgradeBranchName
			refPath := upgradeRepo + "/git/ref/heads/" + url.PathEscape(branch)
			if kind == "foreign-branch" {
				ref := reference{Ref: "refs/heads/" + branch}
				ref.Object.SHA = strings.Repeat("f", 40)
				f.data[refPath] = ref
			}
			f.beforeWrite = func(path string) {
				if kind == "competing-ref" && path == upgradeRepo+"/git/refs" {
					ref := reference{Ref: "refs/heads/" + branch}
					ref.Object.SHA = strings.Repeat("f", 40)
					f.data[refPath] = ref
				}
			}
			f.afterWrite = func(path string) {
				if kind == "tree-extra-path" && path == upgradeRepo+"/git/trees" {
					tree := treeHash(changedUpgradeTree(plan), "")
					key := upgradeRepo + "/git/trees/" + tree + "?recursive=1"
					observed := f.data[key].(upgradeTree)
					observed.Entries = append(observed.Entries, upgradeEntry{Path: "foreign",
						Mode: "100644", Type: "blob", SHA: strings.Repeat("e", 40)})
					f.data[key] = observed
				}
				if kind == "wrong-parent" && path == upgradeRepo+"/git/commits" {
					sha := upgradeCommitID(plan, treeHash(changedUpgradeTree(plan), ""))
					key := upgradeRepo + "/git/commits/" + sha
					commit := f.data[key].(upgradeCommit)
					commit.Parents[0].SHA = strings.Repeat("f", 40)
					f.data[key] = commit
				}
				if kind == "branch-changed" && path == upgradeRepo+"/git/refs" {
					ref := f.data[refPath].(reference)
					ref.Object.SHA = strings.Repeat("e", 40)
					f.data[refPath] = ref
				}
				if path != upgradeRepo+"/pulls" {
					return
				}
				pull := f.data[upgradeRepo+"/pulls/9"].(upgradePull)
				switch kind {
				case "foreign-pr":
					pull.Head.Repository.ID++
				case "closed-pr":
					pull.State = "closed"
				case "wrong-base-sha":
					pull.Base.SHA = strings.Repeat("e", 40)
				case "wrong-base":
					pull.Base.Ref = "foreign"
				case "wrong-url":
					pull.URL = "https://example.invalid/pull/9"
				case "wrong-head":
					pull.Head.SHA = strings.Repeat("f", 40)
				}
				f.data[upgradeRepo+"/pulls/9"] = pull
				if kind == "multiple-prs" {
					f.data[pullListPath(branch)] = []upgradePull{pull, pull}
				}
			}
			result, err := applyUpgrade(f, plan, "latest")
			if err == nil || result.PR != "" {
				data, _ := json.Marshal(result)
				t.Fatal(string(data))
			}
			writes := len(f.writes)
			_, err = applyUpgrade(f, plan, "latest")
			if err == nil || len(f.writes) != writes {
				t.Fatal("replayed conflict", err, f.writes)
			}
		})
	}
}

func TestUpgradeChangedBaseOrTargetCannotReplayPriorProposal(t *testing.T) {
	for _, kind := range []string{"base", "target"} {
		f := upgradeFixture(t, false, true)
		plan := fixtureUpgradePlan(t, f)
		if _, err := applyUpgrade(f, plan, "latest"); err != nil {
			t.Fatal(err)
		}
		writes := len(f.writes)
		if kind == "base" {
			next := strings.Repeat("b", 40)
			commit := f.data[upgradeRepo+"/git/commits/"+plan.Base].(upgradeCommit)
			commit.SHA = next
			f.data[upgradeRepo+"/git/commits/"+next] = commit
			ref := reference{Ref: "refs/heads/main"}
			ref.Object.SHA = next
			f.data[upgradeRepo+"/git/ref/heads/main"] = ref
			for path, text := range f.installed {
				f.data[upgradeRepo+"/contents/"+path+"?ref="+next] = encoded([]byte(text))
			}
		} else {
			pins := fixtureProducers(t, f.fakeAPI, strings.Repeat("6", 40),
				strings.Repeat("7", 40), 103, true)
			setFixtureTarget(f, pins)
		}
		current, err := planUpgrade(f, "example/project", "latest")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := applyUpgrade(f, current, "latest"); err == nil ||
			!strings.Contains(err.Error(), "BRANCH_CONFLICT") {
			t.Fatal(kind, err)
		}
		if len(f.writes) != writes {
			t.Fatal("replayed prior unresolved proposal", f.writes)
		}
	}
}

func TestUpgradeUnverifiableExistingBranchAndOrphanPRDoNotMutate(t *testing.T) {
	for _, kind := range []string{"missing-commit", "missing-tree", "orphan-pr"} {
		f := upgradeFixture(t, false, true)
		plan := fixtureUpgradePlan(t, f)
		if kind == "orphan-pr" {
			f.data[pullListPath(upgradeBranchName)] = []upgradePull{{Number: 12}}
		} else {
			if _, err := applyUpgrade(f, plan, "latest"); err != nil {
				t.Fatal(err)
			}
			tree := treeHash(changedUpgradeTree(plan), "")
			if kind == "missing-commit" {
				delete(f.data, upgradeRepo+"/git/commits/"+upgradeCommitID(plan, tree))
			} else {
				delete(f.data, upgradeRepo+"/git/trees/"+tree+"?recursive=1")
			}
		}
		writes := len(f.writes)
		if _, err := applyUpgrade(f, plan, "latest"); err == nil {
			t.Fatal(kind)
		}
		if len(f.writes) != writes {
			t.Fatal("unverifiable effect authorized writes", f.writes)
		}
	}
}

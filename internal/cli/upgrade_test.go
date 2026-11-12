package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type upgradeNoProcess struct{ t *testing.T }

func (p upgradeNoProcess) Run(string, []string, []byte) ([]byte, error) {
	p.t.Fatal("upgrade must not execute downloaded code, Runner or service commands")
	return nil, nil
}

func TestUpgradeArgumentsAndHelp(t *testing.T) {
	for _, args := range [][]string{{}, {"--repo"}, {"--repo", "example/project", "extra"},
		{"--repo", "example/project", "--version", "main"},
		{"--repo", "example/project", "--apply=yes"}, {"--unknown"}} {
		err := (App{}).Run(append([]string{"upgrade"}, args...), &bytes.Buffer{})
		if err == nil || err.Error() != "INVALID_UPGRADE_ARGUMENTS" {
			t.Fatal(args, err)
		}
	}
	var out bytes.Buffer
	if err := (App{}).Run([]string{"help"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "upgrade --repo") {
		t.Fatal(out.String())
	}
}

func TestUpgradePreviewDeterministicAndNoExternalEffects(t *testing.T) {
	for _, oldUnified := range []bool{false, true} {
		for _, nextUnified := range []bool{false, true} {
			f := upgradeFixture(t, oldUnified, nextUnified)
			root := t.TempDir()
			t.Setenv("XDG_CACHE_HOME", root)
			t.Setenv("LOCALAPPDATA", root)
			app := App{API: f, Process: upgradeNoProcess{t}}
			var first, second bytes.Buffer
			args := []string{"upgrade", "--repo", "example/project"}
			if err := app.Run(args, &first); err != nil {
				t.Fatal(err)
			}
			if err := app.Run(args, &second); err != nil {
				t.Fatal(err)
			}
			if first.String() != second.String() {
				t.Fatal("preview changed")
			}
			if len(f.writes) != 0 {
				t.Fatal(f.writes)
			}
			files, err := os.ReadDir(root)
			if err != nil || len(files) != 0 {
				t.Fatal("preview installed a cache", err)
			}
			plan := fixtureUpgradePlan(t, f)
			if plan.MemberID != 42 {
				t.Fatal("member must remain original, not caller 7")
			}
			if plan.Target.CLI.Commit != strings.Repeat("5", 40) || plan.Base == "" ||
				plan.Compatibility == "" || plan.Current != f.old {
				t.Fatal(plan)
			}
			deleted := false
			for i, change := range plan.Changes {
				if i > 0 && plan.Changes[i-1].Path >= change.Path {
					t.Fatal("order")
				}
				if change.Path == ".github/workflows/nibfin-stage.yml" {
					deleted = change.Kind == "deleted"
					if oldUnified && !nextUnified && change.Kind != "added" {
						t.Fatal(change)
					}
				}
			}
			if deleted != (!oldUnified && nextUnified) {
				t.Fatal("layout transition")
			}
			if !strings.Contains(first.String(), "separate trusted operation") {
				t.Fatal("host adoption obligation missing")
			}
		}
	}
}

func TestUpgradeApplyPersonalAndOrganizationAndLostResponses(t *testing.T) {
	for _, owner := range []string{"User", "Organization"} {
		for _, lost := range []string{"", "/git/trees", "/git/commits", "/git/refs", "/pulls"} {
			f := upgradeFixture(t, false, true)
			repo := f.data[upgradeRepo].(Repository)
			repo.Owner.Type = owner
			f.data[upgradeRepo] = repo
			plan := fixtureUpgradePlan(t, f)
			if lost != "" {
				f.lost = upgradeRepo + lost
			}
			result, err := applyUpgrade(f, plan, "latest")
			if err != nil || result.Status != "pending" || result.PR == "" {
				t.Fatalf("%s %s: %+v %v", owner, lost, result, err)
			}
			writes := append([]string{}, f.writes...)
			result, err = applyUpgrade(f, plan, "latest")
			if err != nil || result.PR == "" || !reflect.DeepEqual(writes, f.writes) {
				t.Fatal("retry must reconcile without mutations", result, err, f.writes)
			}
			if len(writes) != 4 {
				t.Fatal("only tree, commit, branch and PR allowed", writes)
			}
			expected := changedUpgradeTree(plan)
			tree := treeHash(expected, "")
			observed, err := readUpgradeTree(f, upgradeRepo, tree)
			if err != nil {
				t.Fatal(err)
			}
			if observed["user.txt"] != f.baseEntries["user.txt"] {
				t.Fatal("user file")
			}
			if _, exists := observed[".github/workflows/nibfin-stage.yml"]; exists {
				t.Fatal("obsolete stage retained")
			}
			for path, entry := range observed {
				if entry.Type != "tree" && expected[path] != entry {
					t.Fatal(path, entry)
				}
			}
		}
	}
}

func TestUpgradeCommandApplyIsExplicitAndNoChangeHasNoWrites(t *testing.T) {
	for _, apply := range []bool{false, true} {
		f := upgradeFixture(t, true, true)
		f.target = f.old
		pins, _ := json.Marshal(f.old)
		path := "repos/" + officialCLI + "/contents/internal/cli/producers.json?ref=" +
			strings.Repeat("5", 40)
		f.data[path] = encoded(pins)
		args := []string{"upgrade", "--repo", "example/project", "--version", "v1.2.3"}
		if apply {
			args = append(args, "--apply")
		}
		var out bytes.Buffer
		if err := (App{API: f, Process: upgradeNoProcess{t}}).Run(args, &out); err != nil {
			t.Fatal(err)
		}
		if len(f.writes) != 0 || !strings.Contains(out.String(), "no_change") {
			t.Fatal(out.String(), f.writes)
		}
	}
	f := upgradeFixture(t, false, true)
	fixtureUpgradePlan(t, f)
	err := (App{API: f, Process: upgradeNoProcess{t}}).Run([]string{
		"upgrade", "--repo", "example/project", "--apply"}, &bytes.Buffer{})
	if err != nil || len(f.writes) != 4 {
		t.Fatal("explicit apply", err, f.writes)
	}
}

func replaceInstalledFile(t *testing.T, f *upgradeAPI, path, value, mode string) {
	t.Helper()
	entry := f.baseEntries[path]
	entry.Path, entry.Mode, entry.Type = path, mode, "blob"
	entry.SHA = blobHash([]byte(value))
	f.baseEntries[path] = entry
	f.data[upgradeRepo+"/contents/"+path+"?ref="+strings.Repeat("a", 40)] =
		encoded([]byte(value))
	sha := fixtureTree(f.fakeAPI, f.baseEntries)
	commit := f.data[upgradeRepo+"/git/commits/"+strings.Repeat("a", 40)].(upgradeCommit)
	commit.Tree.SHA = sha
	f.data[upgradeRepo+"/git/commits/"+commit.SHA] = commit
}

func TestUpgradeRejectsUnrecognizedInstallationsBeforeMutation(t *testing.T) {
	for _, kind := range []string{"missing-pins", "bad-pins", "substituted-pins",
		"edited-wake", "edited-issue", "edited-stage", "executable", "link",
		"missing-old-source", "member", "foreign-stage", "truncated-tree"} {
		t.Run(kind, func(t *testing.T) {
			f := upgradeFixture(t, kind == "foreign-stage", true)
			path := ".github/workflows/nibfin-wake.yml"
			switch kind {
			case "missing-pins":
				delete(f.data, upgradeRepo+"/contents/.nibfin/producer-pins.json?ref="+
					strings.Repeat("a", 40))
			case "bad-pins", "substituted-pins":
				value := "{}"
				if kind == "substituted-pins" {
					data, _ := json.Marshal(f.target)
					value = string(data)
				}
				replaceInstalledFile(t, f, ".nibfin/producer-pins.json", value, "100644")
			case "edited-wake", "edited-issue", "edited-stage":
				path = ".github/workflows/nibfin-" + strings.TrimPrefix(kind, "edited-") + ".yml"
				replaceInstalledFile(t, f, path, "user replacement", "100644")
			case "executable", "link":
				mode := "100755"
				if kind == "link" {
					mode = "120000"
				}
				replaceInstalledFile(t, f, path, f.installed[path], mode)
			case "member":
				replaceInstalledFile(t, f, path, strings.ReplaceAll(f.installed[path],
					"nibfin-user-42", "nibfin-user-99"), "100644")
			case "foreign-stage":
				replaceInstalledFile(t, f, ".github/workflows/nibfin-stage.yml",
					"user stage workflow", "100644")
			case "missing-old-source":
				delete(f.data, "repos/"+officialCore+"/commits/"+f.old.Core.Commit)
			case "truncated-tree":
				commit := f.data[upgradeRepo+"/git/commits/"+strings.Repeat("a", 40)].(upgradeCommit)
				key := upgradeRepo + "/git/trees/" + commit.Tree.SHA + "?recursive=1"
				tree := f.data[key].(upgradeTree)
				tree.Truncated = true
				f.data[key] = tree
			}
			if _, err := planUpgrade(f, "example/project", "latest"); err == nil {
				t.Fatal("accepted", kind)
			}
			if len(f.writes) != 0 {
				t.Fatal(f.writes)
			}
		})
	}
}

type upgradeBrokenOutput struct{}

func (upgradeBrokenOutput) Write([]byte) (int, error) {
	return 0, os.ErrClosed
}

func TestUpgradeOutputFailureAndPendingResult(t *testing.T) {
	f := upgradeFixture(t, false, true)
	args := []string{"upgrade", "--repo", "example/project", "--apply"}
	err := (App{API: f}).Run(args, upgradeBrokenOutput{})
	if err == nil || err.Error() != "OUTPUT_UNAVAILABLE" || len(f.writes) != 0 {
		t.Fatal("cannot mutate before reviewable preview output", err, f.writes)
	}
	fixtureUpgradePlan(t, f)
	f.lost = upgradeRepo + "/pulls"
	f.denyAfterWrite = pullListPath(upgradeBranchName)
	var out bytes.Buffer
	err = (App{API: f, Process: upgradeNoProcess{t}}).Run(args, &out)
	command, ok := err.(*CommandError)
	if !ok || command.Exit != 2 || !strings.Contains(out.String(), "pending") ||
		!strings.Contains(out.String(), "UPGRADE_PR_UNKNOWN_READ_BACK_BEFORE_RETRY") {
		t.Fatal(out.String(), err)
	}
}

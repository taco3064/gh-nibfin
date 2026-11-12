package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolatedUpgradeCache(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("LOCALAPPDATA", root)
	return root
}

type forbiddenUpgradeProcess struct{ t *testing.T }

func (f forbiddenUpgradeProcess) Run(string, []string, []byte) ([]byte, error) {
	f.t.Fatal("preview must never install a cache or run a host command")
	return nil, errors.New("UNEXPECTED_PROCESS")
}

func TestUpgradePreviewAndInstalledProtection(t *testing.T) {
	for _, owner := range []string{"User", "Organization"} {
		f, selection, target := upgradeFixture(t)
		repo := f.data[upgradeRepo].(Repository)
		repo.Owner.Type = owner
		f.data[upgradeRepo] = repo
		cache := isolatedUpgradeCache(t)
		var output bytes.Buffer
		app := App{API: f, Process: forbiddenUpgradeProcess{t}}
		if err := app.Run([]string{"upgrade", "--repo", "example/project",
			"--version", selection.Version}, &output); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Status string
			Plan   upgradePlan
		}
		if json.Unmarshal(output.Bytes(), &result) != nil || result.Status != "preview" ||
			result.Plan.MemberID != 42 || result.Plan.Target.Pins != target ||
			!result.Plan.Compatible || len(result.Plan.Changes) != 4 {
			t.Fatalf("invalid plan: %s", output.String())
		}
		for _, change := range result.Plan.Changes {
			if change.Path == ".github/workflows/nibfin-stage.yml" && change.After != nil {
				t.Fatal("verified legacy layout must delete the obsolete stage workflow")
			}
			if change.After != nil && strings.Contains(*change.After, "nibfin-user-7") {
				t.Fatal("upgrading operator must not replace the installed member")
			}
		}
		entries, err := os.ReadDir(cache)
		if err != nil || len(entries) != 0 || len(f.writes) != 0 {
			t.Fatal("preview changed local or provider state")
		}
	}
}

func TestUpgradeApplyReconcileAndMergeReadback(t *testing.T) {
	for _, owner := range []string{"User", "Organization"} {
		f, selection, target := upgradeFixture(t)
		repo := f.data[upgradeRepo].(Repository)
		repo.Owner.Type = owner
		f.data[upgradeRepo] = repo
		root := isolatedUpgradeCache(t)
		oldCache := filepath.Join(root, "Nibfin", "candidates", "core", "active-work")
		if os.MkdirAll(oldCache, 0700) != nil ||
			os.WriteFile(filepath.Join(oldCache, "keep"), []byte("old"), 0600) != nil {
			t.Fatal("prepare old cache")
		}
		plan, err := planUpgrade(f, "example/project", selection)
		if err != nil {
			t.Fatal(err)
		}
		app := App{API: f, Process: NativeProcess{}}
		result, err := app.applyUpgrade(plan)
		if err != nil || result.Status != "awaiting_review_merge" || result.PR == "" {
			t.Fatalf("apply: %+v %v", result, err)
		}
		if _, err := os.Stat(filepath.Join(result.Cache, "source-manifest.json")); err != nil {
			t.Fatal("verified package was not actually extracted and read back")
		}
		if _, err := os.Stat(filepath.Join(oldCache, "keep")); err != nil {
			t.Fatal("active work cache was removed")
		}
		writes := len(f.writes)
		again, err := app.applyUpgrade(plan)
		if err != nil || again.PR != result.PR || len(f.writes) != writes {
			t.Fatalf("outstanding update must reconcile without writes: %+v %v", again, err)
		}
		sha := f.pulls[0].Head.SHA
		if f.files[sha]["unrelated"] != "preserve me" ||
			f.files[sha][".nibfin/approvals.json"] != "original approved context" {
			t.Fatal("upgrade altered unrelated data or existing approvals")
		}
		if _, exists := f.files[sha][".github/workflows/nibfin-stage.yml"]; exists {
			t.Fatal("legacy workflow not deleted")
		}
		for _, write := range f.writes {
			if write != "POST "+upgradeRepo+"/git/trees" &&
				write != "POST "+upgradeRepo+"/git/commits" &&
				write != "POST "+upgradeRepo+"/git/refs" && write != "POST "+upgradeRepo+"/pulls" {
				t.Fatal("unexpected external mutation: ", write)
			}
		}
		// Controlled authorized merge: production code never moves the default ref.
		f.data[upgradeRepo+"/git/ref/heads/main"] = map[string]any{
			"ref": "refs/heads/main", "object": map[string]string{"sha": sha}}
		merged, err := planUpgrade(f, "example/project", selection)
		if err != nil || merged.Installed != target || len(merged.Changes) != 0 {
			t.Fatalf("merged inspection: %+v %v", merged, err)
		}
		complete, err := app.applyUpgrade(merged)
		if err != nil || complete.Code != "REPOSITORY_TARGET_READ_BACK" ||
			complete.Status != "completed" || len(f.writes) != writes ||
			complete.Adoption != "separate_authorized_host_adoption_required" {
			t.Fatalf("merged no-op: %+v %v", complete, err)
		}
	}
}

func TestUpgradeLostResponsesAndInterruptedProgress(t *testing.T) {
	for _, stage := range []string{"/git/trees", "/git/commits", "/git/refs", "/pulls"} {
		f, selection, _ := upgradeFixture(t)
		isolatedUpgradeCache(t)
		plan, err := planUpgrade(f, "example/project", selection)
		if err != nil {
			t.Fatal(err)
		}
		f.lost = stage
		app := App{API: f, Process: NativeProcess{}}
		result, err := app.applyUpgrade(plan)
		if err != nil || result.Status != "awaiting_review_merge" {
			t.Fatalf("lost %s must reconcile by readback: %+v %v", stage, result, err)
		}
		writes := len(f.writes)
		if _, err := app.applyUpgrade(plan); err != nil || len(f.writes) != writes {
			t.Fatalf("lost %s duplicate mutation: %v", stage, err)
		}
	}
	for _, stage := range []string{"/git/trees/", "/git/commits/", "/git/ref/heads/",
		"/pulls?"} {
		f, selection, _ := upgradeFixture(t)
		isolatedUpgradeCache(t)
		plan, err := planUpgrade(f, "example/project", selection)
		if err != nil {
			t.Fatal(err)
		}
		// Interrupt immediately after the corresponding mutation, before readback.
		f.hook = func(method, endpoint string) {
			if method == "POST" && strings.HasPrefix(stage, strings.TrimPrefix(
				endpoint, upgradeRepo)) {
				f.blocked = stage
			}
			if method == "POST" && stage == "/git/ref/heads/" &&
				endpoint == upgradeRepo+"/git/refs" {
				f.blocked = stage
			}
		}
		app := App{API: f, Process: NativeProcess{}}
		result, err := app.applyUpgrade(plan)
		if err == nil || result.Status != "pending" {
			t.Fatalf("unavailable %s must stay pending: %+v %v", stage, result, err)
		}
		writes := len(f.writes)
		if _, err := app.applyUpgrade(plan); err == nil || len(f.writes) != writes {
			t.Fatal("unresolved readback must never authorize another write")
		}
		f.hook, f.blocked = nil, ""
		if result, err := app.applyUpgrade(plan); err != nil ||
			result.Status != "awaiting_review_merge" {
			t.Fatalf("interrupted %s recovery: %+v %v", stage, result, err)
		}
	}
}

func TestUpgradeInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"--apply"}, {"--repo", "example/project"},
		{"--repo", "example/project", "--version", "main"},
		{"--repo", "example/project", "--candidate", "short"},
		{"--repo", "example/project", "--version", "v1.2.3", "extra"},
		{"--repo", "example/project", "--version", "v1.2.3", "--candidate", "x"}} {
		err := (App{}).Run(append([]string{"upgrade"}, args...), &bytes.Buffer{})
		if err == nil || err.Error() != "INVALID_UPGRADE_ARGUMENTS" {
			t.Fatalf("invalid upgrade accepted: %v %v", args, err)
		}
	}
}

func upgradeRefPath(plan upgradePlan) string {
	return upgradeRepo + "/git/ref/heads/" + url.PathEscape(upgradeBranchName(plan))
}

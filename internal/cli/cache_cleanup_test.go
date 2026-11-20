package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func privateCacheFixtureParent(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("native ownership classification is Linux-only")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(parent)
	canonical, resolveErr := filepath.EvalSymlinks(parent)
	if err != nil || resolveErr != nil || canonical != parent ||
		!privateCacheInfo(info, true) {
		t.Fatal("ineligible cache fixture namespace", info, err, resolveErr)
	}
	return parent
}

func cacheFixture(t *testing.T, parent string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("native ownership classification is Linux-only")
	}
	name := strings.Repeat("a", 40)
	dir := filepath.Join(parent, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"bin/nibfin-core.mjs": []byte("source"), ".node-version": []byte("24"),
	}
	declarations := []coreManifestFile{}
	for name, data := range files {
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, coreManifestFile{Path: name,
			Mode: "100644", Size: int64(len(data)), Hash: sha256Hash(data)})
	}
	manifest, err := json.Marshal(map[string]any{"schemaVersion": 1,
		"repository": officialCore, "commit": name, "files": declarations})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, coreManifestName)
	if err := os.WriteFile(target, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func unusedCache(string) string { return "" }

func TestCacheRetirementRetainsUncertainEntries(t *testing.T) {
	for _, reason := range []string{"selected_source", "active_use", "retained_recovery",
		"unknown_observation", "foreign_entry"} {
		t.Run(reason, func(t *testing.T) {
			parent := privateCacheFixtureParent(t)
			dir := cacheFixture(t, parent)
			policy := cacheRetirement{observe: unusedCache}
			switch reason {
			case "selected_source":
				policy.selected = dir
			case "retained_recovery":
				policy.retained = func(string) string { return reason }
			case "foreign_entry":
				if err := os.WriteFile(filepath.Join(dir, "foreign"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			default:
				policy.observe = func(string) string { return reason }
			}
			report := reconcileCoreCache(parent, policy)
			if len(report.Entries) != 1 || report.Entries[0].Reason != reason {
				t.Fatal(report)
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCacheRetirementResumesPartialDeletionAndAbsence(t *testing.T) {
	parent := privateCacheFixtureParent(t)
	dir := cacheFixture(t, parent)
	calls := 0
	policy := cacheRetirement{observe: unusedCache, remove: func(root *os.Root,
		name string) error {
		calls++
		if calls == 2 {
			return errors.New("simulated interruption")
		}
		return root.Remove(name)
	}}
	report := reconcileCoreCache(parent, policy)
	if report.Entries[0].Reason != "removal_failure" {
		t.Fatal(report)
	}
	if _, err := os.Stat(filepath.Join(dir, coreManifestName)); err != nil {
		t.Fatal(err)
	}
	policy.remove = nil
	report = reconcileCoreCache(parent, policy)
	if report.Entries[0].Status != "removed" {
		t.Fatal(report)
	}
	// A lost response needs no receipt: absence is already completed cleanup.
	if report := reconcileCoreCache(parent, policy); len(report.Entries) != 0 {
		t.Fatal(report)
	}
}

func TestCacheRetirementReobservesUseAndForeignAdditions(t *testing.T) {
	for _, change := range []string{"active_use", "foreign_entry", "changed_identity"} {
		t.Run(change, func(t *testing.T) {
			parent := privateCacheFixtureParent(t)
			dir := cacheFixture(t, parent)
			calls := 0
			observe := func(string) string {
				calls++
				if calls != 2 {
					return ""
				}
				switch change {
				case "active_use":
					return "active_use"
				case "foreign_entry":
					if err := os.WriteFile(filepath.Join(dir, "foreign"), nil, 0600); err != nil {
						t.Fatal(err)
					}
				case "changed_identity":
					if err := os.Rename(dir, dir+"-retained"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dir, 0700); err != nil {
						t.Fatal(err)
					}
				}
				return ""
			}
			report := reconcileCoreCache(parent, cacheRetirement{observe: observe})
			if report.Entries[0].Reason != change {
				t.Fatal(report)
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCacheRetirementDoesNotUseVerifierAgeOrFollowLinks(t *testing.T) {
	parent := privateCacheFixtureParent(t)
	for _, name := range []string{".verify-1", ".verify-2"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	dir := cacheFixture(t, parent)
	if err := os.Remove(filepath.Join(dir, ".node-version")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside", filepath.Join(dir, ".node-version")); err != nil {
		t.Fatal(err)
	}
	report := reconcileCoreCache(parent, cacheRetirement{observe: unusedCache})
	for _, entry := range report.Entries {
		if entry.Reason != "foreign_entry" {
			t.Fatal(report)
		}
	}
}

type discardedCleanupOutput struct{}

func (discardedCleanupOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCleanupOutputFailureDoesNotChangeObservedRemoval(t *testing.T) {
	parent := privateCacheFixtureParent(t)
	cacheFixture(t, parent)
	report := reconcileCoreCache(parent, cacheRetirement{observe: unusedCache})
	reportCacheCleanup(discardedCleanupOutput{}, report)
	if report.Entries[0].Status != "removed" {
		t.Fatal(report)
	}
}

func TestCacheBoundaryProtectsSelectionUntilFailureExit(t *testing.T) {
	parent := privateCacheFixtureParent(t)
	dir := cacheFixture(t, parent)
	observations := []cacheCleanupReport{}
	reconcile := func(selected string) cacheCleanupReport {
		report := reconcileCoreCache(parent, cacheRetirement{
			selected: selected, observe: unusedCache,
		})
		observations = append(observations, report)
		return report
	}
	operation := func() error {
		finish := beginCacheCleanup(discardedCleanupOutput{}, dir, reconcile)
		defer finish()
		if _, err := os.Stat(dir); err != nil {
			t.Fatal(err)
		}
		if observations[0].Entries[0].Reason != "selected_source" {
			t.Fatal(observations)
		}
		return io.ErrClosedPipe
	}
	if err := operation(); err != io.ErrClosedPipe {
		t.Fatal(err)
	}
	if len(observations) != 2 || observations[1].Entries[0].Status != "removed" {
		t.Fatal(observations)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetirementRetainsOtherRepositoryIntent(t *testing.T) {
	parent := privateCacheFixtureParent(t)
	dir := cacheFixture(t, parent)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	intentDir := filepath.Join(config, "nibfin", "install")
	if err := os.MkdirAll(intentDir, 0700); err != nil {
		t.Fatal(err)
	}
	intent := managedInstallIntent{Pins: Pins(), Plan: InitPlan{
		Repository: Repository{ID: 123, FullName: "another/repository"},
	}}
	intent.Pins.Core.Commit = filepath.Base(dir)
	data, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(intentDir, "123.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	report := reconcileCoreCache(parent, cacheRetirement{
		observe: unusedCache, retained: retainedCacheSource,
	})
	if report.Entries[0].Reason != "retained_recovery" {
		t.Fatal(report)
	}
}

func TestNativeCacheUseNodeDoesNotRelyOnOpenFiles(t *testing.T) {
	proc := t.TempDir()
	if err := os.WriteFile(filepath.Join(proc, "cmdline"),
		[]byte("node\x00/unrelated/entry.mjs\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	// No open-file evidence is needed to recognize an uncertain Node consumer.
	if reason := processCacheUse(proc, "/candidate"); reason != "unknown_observation" {
		t.Fatal(reason)
	}
	if err := os.WriteFile(filepath.Join(proc, "cmdline"),
		[]byte("gh-nibfin\x00init\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	if reason := processCacheUse(proc, "/candidate"); reason != "active_use" {
		t.Fatal(reason)
	}
	if err := os.WriteFile(filepath.Join(proc, "cmdline"),
		[]byte("node\x00/candidate/bin/nibfin-core.mjs\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	if reason := processCacheUse(proc, "/candidate"); reason != "active_use" {
		t.Fatal(reason)
	}
}

func TestReservedCleanupPreservesReplacementAndForeignContents(t *testing.T) {
	for _, change := range []string{"replacement", "foreign"} {
		t.Run(change, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "reserved")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			original, err := os.Lstat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if change == "replacement" {
				if err := os.Rename(dir, dir+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "foreign"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			reason := cleanupReservedCache(dir, original, map[string]coreArchiveFile{})
			if reason == "" {
				t.Fatal("unsafe reservation cleanup succeeded")
			}
			if _, err := os.Stat(filepath.Join(dir, "foreign")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCacheReportSanitizesForeignNames(t *testing.T) {
	report := cacheCleanupReport{}
	report.add("private-secret-name", "retained", "foreign_entry")
	data, err := json.Marshal(report)
	if err != nil || strings.Contains(string(data), "private-secret-name") {
		t.Fatal(string(data), err)
	}
}

func TestReservedCleanupRequiresCanonicalNamespace(t *testing.T) {
	parent := canonicalCacheFixtureParent(t)
	alias := filepath.Join(canonicalCacheFixtureParent(t), "alias")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(parent, "reserved")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]coreArchiveFile{}
	if reason := cleanupReservedCache(filepath.Join(alias, "reserved"), info,
		files); reason != "changed_identity" {
		t.Fatal("aliased namespace must be refused", reason)
	}
	if _, err := os.Stat(directory); err != nil {
		t.Fatal("refused cleanup changed the reservation", err)
	}
	if reason := cleanupReservedCache(directory, info, files); reason != "" {
		t.Fatal("canonical owned reservation must be removable", reason)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("reservation remains after cleanup", err)
	}
}

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRemovalRevalidatesAfterFinalObservation(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		kind := "retirement"
		if reserved {
			kind = "reserved"
		}
		t.Run(kind, func(t *testing.T) {
			for _, change := range []string{
				"replacement", "content", "addition", "entry", "namespace", "final",
			} {
				t.Run(change, func(t *testing.T) {
					checkCacheObservationChange(t, reserved, change)
				})
			}
		})
	}
}

func checkCacheObservationChange(t *testing.T, reserved bool, change string) {
	t.Helper()
	parent := privateCacheFixtureParent(t)
	directory := cacheFixture(t, parent)
	info, err := os.Lstat(directory)
	if err != nil {
		t.Fatal(err)
	}
	inventory := map[string]coreArchiveFile{}
	for _, name := range []string{
		"bin/nibfin-core.mjs", ".node-version", coreManifestName,
	} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		inventory[name] = coreArchiveFile{data: data, mode: 0644}
	}
	calls, trigger := 0, 3
	if reserved {
		trigger = 2
	}
	changed := false
	preserved := filepath.Join(directory, "bin", "nibfin-core.mjs")
	observe := func(string) string {
		calls++
		if change == "final" {
			_, err := os.Lstat(filepath.Join(directory, coreManifestName))
			if !os.IsNotExist(err) {
				return ""
			}
		} else if calls != trigger {
			return ""
		}
		if changed {
			return ""
		}
		changed = true
		switch change {
		case "replacement":
			// Keep the original inode alive so replacement identity is deterministic.
			err = os.Rename(preserved, filepath.Join(parent, "original"))
		case "entry":
			err = os.Rename(directory, directory+"-original")
			if err == nil {
				err = os.Mkdir(directory, 0700)
			}
			preserved = filepath.Join(directory, "foreign")
		case "namespace":
			moved := filepath.Join(t.TempDir(), "original")
			err = os.Rename(parent, moved)
			if err == nil {
				err = os.Mkdir(parent, 0700)
			}
			preserved = filepath.Join(moved, filepath.Base(directory),
				"bin", "nibfin-core.mjs")
			return ""
		case "addition", "final":
			preserved = filepath.Join(directory, "foreign")
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(preserved, []byte("foreign"), 0600); err != nil {
			t.Fatal(err)
		}
		return ""
	}
	var reason string
	if reserved {
		reason = cleanupReservedCacheObserved(directory, info, inventory, observe)
	} else {
		report := reconcileCoreCache(parent, cacheRetirement{observe: observe})
		if len(report.Entries) != 1 || report.Entries[0].Status != "deferred" {
			t.Fatal(report)
		}
		reason = report.Entries[0].Reason
	}
	if err != nil {
		t.Fatal(err)
	}
	want := "foreign_entry"
	if change == "replacement" || change == "entry" || change == "namespace" {
		want = "changed_identity"
	}
	if !changed || reason != want {
		t.Fatalf("changed=%t reason=%q, want %q", changed, reason, want)
	}
	if _, err := os.Stat(preserved); err != nil {
		t.Fatal("changed content was removed", err)
	}
}

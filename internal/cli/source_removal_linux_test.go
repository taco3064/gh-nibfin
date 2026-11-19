package cli

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func TestSourceConcurrentLeafReplacementAfterClassification(t *testing.T) {
	for attempt := 0; attempt < 400; attempt++ {
		source := sourceFixture(t, t.TempDir())
		if _, err := classifiedSourcePaths(source.root, source.record); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(source.Directory, ".node-version")
		foreign := filepath.Join(filepath.Dir(source.Directory), "foreign")
		if err := os.WriteFile(foreign, []byte("foreign data"), 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(foreign)
		if err != nil {
			t.Fatal(err)
		}
		var replacement sync.WaitGroup
		replacement.Add(1)
		go func() {
			defer replacement.Done()
			// Race a new leaf against capture/removal after full classification.
			_ = os.Rename(foreign, path)
		}()
		_ = removeClassifiedSourceEntry(source.root, "source/.node-version",
			source.record)
		replacement.Wait()
		info, err := file.Stat()
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if info.Sys().(*syscall.Stat_t).Nlink == 0 {
			t.Fatal("foreign replacement was deleted", attempt)
		}
		source.root.Close()
	}
}

func TestSourceCapturedEntryDoesNotDeleteReplacement(t *testing.T) {
	source := sourceFixture(t, t.TempDir())
	if _, err := classifiedSourcePaths(source.root, source.record); err != nil {
		t.Fatal(err)
	}
	if err := source.root.Mkdir("holding", 0700); err != nil {
		t.Fatal(err)
	}
	holding, err := source.root.OpenRoot("holding")
	if err != nil {
		t.Fatal(err)
	}
	defer holding.Close()
	path := "source/.node-version"
	if err := source.root.Rename(path, "holding/entry"); err != nil {
		t.Fatal(err)
	}
	// The original name can be replaced even after successful identity checks.
	if err := source.root.WriteFile(path, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeCapturedSourceEntry(holding, source.record.Entries[path]); err != nil {
		t.Fatal(err)
	}
	data, err := source.root.ReadFile(path)
	if err != nil || string(data) != "foreign" {
		t.Fatal("replacement at original name was deleted", err)
	}
}

func TestSourceRetirementResumesJournaledCapture(t *testing.T) {
	source := sourceFixture(t, t.TempDir())
	if err := source.root.Mkdir(".retiring-interrupted", 0700); err != nil {
		t.Fatal(err)
	}
	info, err := source.root.Stat(".retiring-interrupted")
	if err != nil {
		t.Fatal(err)
	}
	path := ".retiring-interrupted/entry"
	source.record.Entries[".retiring-interrupted"] = stampSourceEntry(info)
	source.record.Entries[path] = source.record.Entries["source/.node-version"]
	if err := source.save(); err != nil {
		t.Fatal(err)
	}
	if err := source.root.Rename("source/.node-version", path); err != nil {
		t.Fatal(err)
	}
	if result := source.Release(); result.Status != "removed" {
		t.Fatal("interrupted capture was not reconciled", result)
	}
	sourceGone(t, source)
}

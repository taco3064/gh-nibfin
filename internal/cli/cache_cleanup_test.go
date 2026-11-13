package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepEmptyVerifierDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".verify-1", ".verify-2", ".verify-3", "other"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, ".verify-2", "recovery-data")
	if err := os.WriteFile(file, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, name := range []string{".verify-1", ".verify-2", "other"} {
		if err := os.Chtimes(filepath.Join(root, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := sweepEmptyVerifierDirectories(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".verify-1")); !os.IsNotExist(err) {
		t.Fatalf("stale empty verifier directory retained: %v", err)
	}
	for _, name := range []string{file, filepath.Join(root, ".verify-3"),
		filepath.Join(root, "other")} {
		if _, err := os.Stat(name); err != nil {
			t.Fatal(err)
		}
	}
}

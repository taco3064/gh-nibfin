package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeHandoffACLRejectsAnUntrustedReader(t *testing.T) {
	directory := privateRunnerTestRoot(t)
	if err := initializePrivateRunnerDirectory(directory); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "synthetic-capability.json")
	if err := os.WriteFile(file, []byte("synthetic fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := requirePrivateHandoffPaths([]string{file}); err != nil {
		t.Fatal("private capability was refused")
	}
	cmd := exec.Command("icacls.exe", file, "/grant", "*S-1-1-0:(R)")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("grant fixture read access: %v: %s", err, out)
	}
	if err := requirePrivateHandoffPaths([]string{file}); err == nil {
		t.Fatal("an unrelated reader can access the registration capability")
	}
}

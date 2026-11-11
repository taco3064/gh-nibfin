package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeRunnerACLRejectsAnUntrustedWriter(t *testing.T) {
	directory := t.TempDir()
	if err := initializePrivateRunnerDirectory(directory); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "runner.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	trusted, err := trustedRunnerACLAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := privateWindowsPath(file, runnerWriteMask, trusted); err != nil || !ok {
		t.Fatalf("trusted ACL rejected: allowed=%t error=%v", ok, err)
	}
	cmd := exec.Command("icacls.exe", file, "/grant", "*S-1-1-0:(W)")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("grant fixture access: %v: %s", err, out)
	}
	if ok, err := privateWindowsPath(file, runnerWriteMask, trusted); err != nil || ok {
		t.Fatalf("untrusted write was not refused: allowed=%t error=%v", ok, err)
	}
}

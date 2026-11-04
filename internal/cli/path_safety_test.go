package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunnerPathsRejectGitWorktreesButAllowOrdinaryHomeDirectories(t *testing.T) {
	root := privateRunnerTestRoot(t)
	if _, err := runnerDirectory(filepath.Join(root, "managed")); err != nil {
		t.Fatal("non-repository paths must remain usable")
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runnerDirectory(filepath.Join(root, "managed")); err == nil {
		t.Fatal("a Runner installation must not be placed under a Git worktree")
	}
}

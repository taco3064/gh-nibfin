package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitAncestorChecksRegularFileParents(t *testing.T) {
	root := privateRunnerTestRoot(t)
	file := filepath.Join(root, "signing.pem")
	if err := os.WriteFile(file, []byte("synthetic key fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if hasGitAncestor(file) {
		t.Fatal("a regular file outside a checkout must remain usable")
	}
	if _, err := runnerDirectory(file); err == nil {
		t.Fatal("a regular file must not become a Runner directory")
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if !hasGitAncestor(file) {
		t.Fatal("a worktree .git file must block paths under that checkout")
	}
}

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

func TestRunnerPathChecksTheTargetItself(t *testing.T) {
	root := privateRunnerTestRoot(t)
	target := filepath.Join(root, "managed")
	if os.MkdirAll(filepath.Join(target, ".git"), 0700) != nil {
		t.Fatal("cannot create target repository fixture")
	}
	if _, err := runnerDirectory(target); err == nil {
		t.Fatal("an existing target checkout must not become a trusted Runner")
	}
}

func TestRunnerPathChecksCanonicalSymlinkParent(t *testing.T) {
	root := privateRunnerTestRoot(t)
	checkout := filepath.Join(root, "checkout")
	if os.MkdirAll(filepath.Join(checkout, ".git"), 0700) != nil {
		t.Fatal("cannot create repository fixture")
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(checkout, alias); err != nil {
		t.Skip("this test account cannot create filesystem symlinks")
	}
	if _, err := runnerDirectory(filepath.Join(alias, "managed")); err == nil {
		t.Fatal("a symlink parent cannot bypass worktree rejection")
	}
}

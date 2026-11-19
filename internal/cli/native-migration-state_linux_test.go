package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeMigrationCleanupKeepsInstallIntentAndExistingLockInode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, plan := nativeWakeFixture()
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("original install intent"), 0600); err != nil {
		t.Fatal(err)
	}
	state, save, unlock, err := nativeMigrationState(plan)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	state.SecretState = "observed"
	if err := save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if err := removeNativeMigrationState(plan); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path + ".lock")
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("cleanup removed the admitted lock inode", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original install intent" {
		t.Fatal("cleanup touched original installation intent", err)
	}
	if _, err := os.Stat(path + ".native-migration"); !os.IsNotExist(err) {
		t.Fatal("completed progress was retained", err)
	}
}

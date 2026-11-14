package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInitSharedRunnerPackagesResumeMissingSlotWithoutRegistration(t *testing.T) {
	a, api, host, directory := runnerFixture(t)
	plan := InitPlan{
		Repository: api.data["repos/example/project"].(Repository),
		Member:     api.data["user"].(User),
	}
	plan.Execution = &SharedExecution{
		Repository: plan.Repository.FullName, RepositoryID: plan.Repository.ID,
	}
	parent := filepath.Dir(directory)
	download := a.Download
	attempts := 0
	a.Download = func(app RunnerApplication, path string) error {
		attempts++
		if attempts == 2 {
			return errors.New("INTERRUPTED_FIXTURE_DOWNLOAD")
		}
		return download(app, path)
	}
	if _, err := a.stageRunnerSlots(plan, parent, api.app); err == nil {
		t.Fatal("interrupted second slot must leave initialization incomplete")
	}
	for range 2 {
		paths, err := a.stageRunnerSlots(plan, parent, api.app)
		if err != nil || len(paths) != 2 {
			t.Fatalf("expected two verified packages, got %q, %v", paths, err)
		}
	}
	if attempts != 3 || api.tokens != 0 || host.calls != 0 || len(api.writes) != 0 {
		t.Fatal("resuming preparation must preserve verified slots and avoid registration")
	}
	first := filepath.Join(parent, "nibfin-10-7-1")
	credentials := filepath.Join(first, ".credentials")
	if err := os.WriteFile(credentials, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.stageRunnerSlots(plan, parent, api.app); err == nil || attempts != 3 {
		t.Fatal("a slot containing registration credentials must not be staged")
	}
	if err := os.Remove(credentials); err != nil {
		t.Fatal(err)
	}
	record, err := readRunnerRecord(first, plan.Repository, plan.Member)
	if err != nil {
		t.Fatal(err)
	}
	record.State = "registration-pending"
	if err := writeRunnerRecord(first, record); err != nil {
		t.Fatal(err)
	}
	if paths, err := a.stageRunnerSlots(plan, parent, api.app); err != nil ||
		len(paths) != 2 || api.tokens != 0 || attempts != 3 {
		t.Fatal("pending registration must reach reconciliation without replacement")
	}
	if err := os.WriteFile(filepath.Join(first, ".nibfin-runner-package"),
		[]byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.stageRunnerSlots(plan, parent, api.app); err == nil || attempts != 3 {
		t.Fatal("tampered slot must stop without replacing either package")
	}
}

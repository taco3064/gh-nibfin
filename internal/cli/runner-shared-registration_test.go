package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedSlotPathMatchesServiceInstance(t *testing.T) {
	first, second := sharedRunnerSlotName(10, 7, 1), sharedRunnerSlotName(10, 7, 2)
	for _, test := range []struct {
		path, name string
		valid      bool
	}{
		{"/var/lib/nibfin/runners/slot-1", first, true},
		{"/var/lib/nibfin/runners/slot-2", second, true},
		{"/var/lib/nibfin/runners/slot-2", first, false},
		{"/var/lib/nibfin/runners/slot-1", "unrelated", false},
		{filepath.Join("cache", first), first, true},
	} {
		if sharedSlotPathMatches(test.path, test.name, first, second) != test.valid {
			t.Fatalf("unexpected slot path decision for %q and %q", test.path, test.name)
		}
	}
}

func TestSharedRegistrationRecoversOnlyAnObservedOfflineSlot(t *testing.T) {
	a, api, host, path := runnerFixture(t)
	repo := api.data["repos/example/project"].(Repository)
	member := api.data["user"].(User)
	record, err := createRunnerPackage(a.Download, repo, member, "slot", path, api.app)
	if err != nil {
		t.Fatal(err)
	}
	host.lost = true
	record, err = registerRunnerWithLabel(api, host, repo, member, path, record,
		api.now, sharedRunnerLabel)
	if err == nil || record.State != "registration-pending" {
		t.Fatalf("expected pending registration, got %+v, %v", record, err)
	}
	if _, err := reconcileSharedRegistration(api, repo, member, path, record,
		api.runners); err == nil {
		t.Fatal("missing private credentials must retain pending")
	}
	for _, file := range []string{".credentials", ".credentials_rsaparams"} {
		if err := os.WriteFile(filepath.Join(path, file), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	api.runners[0].Status = "online"
	if _, err := reconcileSharedRegistration(api, repo, member, path, record,
		api.runners); err == nil {
		t.Fatal("online registration must remain pending")
	}
	api.runners[0].Status = "offline"
	result, err := reconcileSharedRegistration(api, repo, member, path, record, api.runners)
	if err != nil || result.State != "registered" || api.tokens != 1 || host.calls != 1 {
		t.Fatalf("expected read-only provider recovery, got %+v, %v", result, err)
	}
}

func TestSharedOfflineRegistrationRequiresBothNativeLabels(t *testing.T) {
	a, api, host, directory := runnerFixture(t)
	repo := api.data["repos/example/project"].(Repository)
	member := api.data["user"].(User)
	name := sharedRunnerSlotName(repo.ID, member.ID, 1)
	path := filepath.Join(filepath.Dir(directory), name)
	record, err := createRunnerPackage(a.Download, repo, member, name, path, api.app)
	if err != nil {
		t.Fatal(err)
	}
	record, err = registerRunnerWithLabel(api, host, repo, member, path, record,
		api.now, sharedRunnerLabel)
	if err != nil || record.State != "registered" {
		t.Fatalf("shared registration result: %+v, %v", record, err)
	}
	if len(api.runners) != 1 || api.runners[0].Status != "offline" ||
		!hasMemberLabel(api.runners[0], member.ID) ||
		!hasRunnerLabel(api.runners[0], sharedRunnerLabel) {
		t.Fatalf("unexpected registration readback: %+v", api.runners)
	}
	if _, err := registerRunnerWithLabel(api, host, repo, member, path, record,
		api.now, sharedRunnerLabel); err == nil || api.tokens != 1 {
		t.Fatal("existing registration must not request another token")
	}
	observed, err := readRegisteredSharedSlot(api, repo, member, path, record)
	if err != nil || observed.RunnerID != record.RunnerID || api.tokens != 1 {
		t.Fatalf("offline resume must verify the original registration: %+v, %v",
			observed, err)
	}
	plan := InitPlan{Repository: repo, Member: member}
	paths, err := a.stageRunnerSlots(plan, filepath.Dir(directory), api.app)
	if err != nil || len(paths) != 2 || paths[0] != path || api.tokens != 1 {
		t.Fatalf("init after partial registration must reuse slot one: %v, %v", paths, err)
	}
	api.runners[0].Status = "online"
	if _, err := readRegisteredSharedSlot(api, repo, member, path, record); err == nil {
		t.Fatal("an online Runner cannot be treated as an offline installation")
	}
}

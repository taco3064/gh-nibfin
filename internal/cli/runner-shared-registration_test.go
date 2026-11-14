package cli

import (
	"path/filepath"
	"testing"
)

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

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedInitRecoversPartiallyStartedSlots(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	api := initFixture()
	api.data["repos/actions/runner/releases/tags/v2.338.2"] = map[string]any{
		"tag_name": "v2.338.2",
		"assets": []map[string]string{{
			"name": sharedRunnerArchive, "digest": "sha256:" + sharedRunnerHash,
			"browser_download_url": sharedRunnerDownload + sharedRunnerArchive,
		}},
	}
	a := App{API: api}
	plan := InitPlan{
		Repository: api.data["repos/example/project"].(Repository),
		Member:     api.data["user"].(User),
	}
	runners := []sharedRegistration{
		{ID: 25, Name: "nibfin-10-7-1", Status: "online"},
		{ID: 26, Name: "nibfin-10-7-2", Status: "offline"},
	}
	app, parent, err := a.sharedSlotsCache(plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, runners[0].Name)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeRunnerRecord(path, RunnerInstall{
		Repository: plan.Repository.FullName, RepositoryID: plan.Repository.ID,
		MemberID: plan.Member.ID, RunnerID: runners[0].ID, Name: runners[0].Name,
		State: "registered", Application: app,
	}); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(path, stagedSecrets[0])
	if err := os.WriteFile(secretPath, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	sources, err := a.readStagedSharedSlots(plan, runners)
	if err != nil || len(sources) != 0 {
		t.Fatalf("expected to resume the installed slots without sources: %v, %v", sources, err)
	}
	runners[1].Status = "online"
	paths, err := a.stagedSharedRegistrationPaths(plan, runners, false)
	if err != nil || len(paths) != 1 || paths[0] != path {
		t.Fatalf("expected staged credentials after both IDs are online: %v, %v", paths, err)
	}
	if err := retireStagedRegistration(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(secretPath); !os.IsNotExist(err) || len(api.writes) != 0 {
		t.Fatalf("expected local cleanup without GitHub writes: %v, %v", err, api.writes)
	}
}

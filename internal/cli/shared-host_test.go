package cli

import "testing"

func TestSharedHostRequiresBothOriginalRegistrations(t *testing.T) {
	api := initFixture()
	host, err := decodeSharedHost([]byte(`{
		"schemaVersion": 1, "repository": "example/project", "repositoryId": 10,
		"memberId": 7, "adoptedRepositoryIds": [10, 20],
		"runners": [{"id":25,"name":"slot-1"},{"id":26,"name":"slot-2"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if host.adoptionStatus(20) != "observed" || host.adoptionStatus(21) != "required" {
		t.Fatal("Runner reuse must distinguish adopted and new repositories")
	}
	path := "repos/example/project/actions/runners?per_page=100&page=1"
	runner := func(id int, name string) map[string]any {
		return map[string]any{"id": id, "name": name, "os": "Linux", "labels": []any{
			map[string]string{"name": "nibfin-user-7"},
			map[string]string{"name": "nibfin-shared"},
		}}
	}
	api.data[path] = map[string]any{
		"total_count": 2, "runners": []any{runner(25, "slot-1"), runner(26, "slot-2")},
	}
	for range 2 {
		if err := verifySharedHost(api, *host); err != nil {
			t.Fatal(err)
		}
	}
	api.data[path] = map[string]any{
		"total_count": 2, "runners": []any{runner(25, "slot-1"), runner(27, "slot-2")},
	}
	if err := verifySharedHost(api, *host); err == nil ||
		err.Error() != "SHARED_RUNNER_REGISTRATION_CHANGED" {
		t.Fatalf("replacement registration must be rejected, got %v", err)
	}
	if len(api.writes) != 0 {
		t.Fatal("Runner discovery must not register or modify Runners")
	}
}

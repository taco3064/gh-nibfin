package cli

import (
	"errors"
	"testing"
)

type sharedInspectionProcess struct{ help string }

func (p sharedInspectionProcess) Run(_ string, args []string, _ []byte) ([]byte, error) {
	if args[1] == "help" {
		return []byte(p.help), nil
	}
	return nil, errors.New("fixture inspection failure")
}

func TestSharedInspectionDoesNotTreatBrokenAdoptionAsLegacy(t *testing.T) {
	for _, test := range []struct {
		help   string
		legacy bool
	}{
		{`{"commands":["contract"]}`, true},
		{`{"commands":["contract","inspect-shared-host"]}`, false},
		{`{"commands":[]}`, false},
		{"broken", false},
	} {
		_, err := readInstalledSharedHost(sharedInspectionProcess{test.help}, "node", "core")
		if err == nil || errors.Is(err, errLegacySharedHost) != test.legacy {
			t.Fatalf("unexpected inspection result: %v", err)
		}
	}
}

func TestSharedServiceInstallReadback(t *testing.T) {
	const unit = `,"unit":"nibfin-shared-runner@.service"`
	for _, input := range []struct {
		json, status string
	}{
		{`{"status":"installed"` + unit + `,"servicesStarted":false}`,
			"installed"},
		{`{"status":"unchanged"` + unit + `,"servicesStarted":false}`,
			"unchanged"},
		{`{"status":"installed"` + unit + `,"servicesStarted":true}`,
			""},
		{`{"status":"installed","unit":"nibfin-shared-runner@.service"}`, ""},
		{`{"status":"installed","unit":"another@.service","servicesStarted":false}`, ""},
	} {
		status, err := decodeSharedServiceInstall([]byte(input.json))
		if status != input.status || (err != nil) != (input.status == "") {
			t.Fatalf("unexpected installation readback %q, %v", status, err)
		}
	}
}

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
		status := "online"
		if id == 26 {
			status = "offline"
		}
		return map[string]any{"id": id, "name": name, "os": "Linux",
			"status": status, "busy": id == 25, "labels": []any{
				map[string]string{"name": "nibfin-user-7"},
				map[string]string{"name": "nibfin-shared"},
			}}
	}
	api.data[path] = map[string]any{
		"total_count": 2, "runners": []any{runner(25, "slot-1"), runner(26, "slot-2")},
	}
	for range 2 {
		runners, err := host.verifyInit(api, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(runners) != 2 || !runners[0].Busy || runners[0].Status != "online" ||
			runners[1].Status != "offline" {
			t.Fatalf("expected online busy and offline registrations, got %+v", runners)
		}
	}
	for _, options := range [][2]string{{"/runner", ""}, {"", "third-slot"}} {
		_, err := host.verifyInit(api, "", options[0], options[1])
		if err == nil || err.Error() != "SHARED_EXECUTION_LEGACY_RUNNER_OPTIONS_CONFLICT" {
			t.Fatalf("shared host must refuse a legacy Runner request, got %v", err)
		}
	}
	if _, err := host.verifyInit(api, "example/other", "", ""); err == nil ||
		err.Error() != "SHARED_EXECUTION_HOST_CONFLICT" {
		t.Fatalf("shared host must refuse a different execution repository, got %v", err)
	}
	check, observed := inspectSharedRegistrations(api, Repository{ID: 20}, host, nil)
	if check.Code != "SHARED_RUNNER_NOT_ONLINE" || len(observed) != 2 {
		t.Fatalf("preflight must expose the offline slot, got %+v", check)
	}
	check, _ = inspectSharedRegistrations(api, Repository{ID: 21}, host, nil)
	if check.Code != "SHARED_TARGET_ADOPTION_UNVERIFIED" {
		t.Fatalf("preflight must distinguish an unadopted Repo, got %+v", check)
	}
	api.data[path] = map[string]any{
		"total_count": 2, "runners": []any{runner(25, "slot-1"), runner(27, "slot-2")},
	}
	if _, err := verifySharedHost(api, *host); err == nil ||
		err.Error() != "SHARED_RUNNER_REGISTRATION_CHANGED" {
		t.Fatalf("replacement registration must be rejected, got %v", err)
	}
	if len(api.writes) != 0 {
		t.Fatal("Runner discovery must not register or modify Runners")
	}
}

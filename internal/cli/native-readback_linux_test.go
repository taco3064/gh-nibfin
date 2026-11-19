package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func nativeFixtureHost(t *testing.T, api *nativeWakeAPI) *sharedHost {
	t.Helper()
	host, err := decodeSharedHost([]byte(`{
		"schemaVersion":1,"repositoryId":10,"repository":"example/project","memberId":7,
		"adoptedRepositoryIds":[10],
		"runners":[{"id":25,"name":"slot-1"},{"id":26,"name":"slot-2"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	runners := []RunnerInfo{}
	for index, name := range []string{"slot-1", "slot-2"} {
		runners = append(runners, RunnerInfo{ID: int64(25 + index), Name: name,
			OS: "Linux", Status: "online", Labels: []struct {
				Name string `json:"name"`
			}{{"nibfin-user-7"}, {"nibfin-shared"}}})
	}
	api.data["repos/example/project/actions/runners?per_page=100&page=1"] = map[string]any{
		"total_count": 2, "runners": runners}
	return host
}

func TestNativeMissingIntentRequiresActualCompleteInstallation(t *testing.T) {
	for _, scenario := range []string{"host-missing", "member", "host-binding", "adoption",
		"destination", "failed-read", "unrelated-secret", "runner"} {
		t.Run(scenario, func(t *testing.T) {
			base, plan, merge := managedResumeFixture(t)
			plan.NativeManaged = true
			api := &nativeWakeAPI{fakeAPI: base}
			if scenario != "unrelated-secret" {
				merge()
			}
			host := nativeFixtureHost(t, api)
			value, _ := json.Marshal(plan.Execution)
			destination := "repos/example/project/actions/variables/" + sharedDestinationVariable
			api.data[destination] = actionsVariable{
				Name: sharedDestinationVariable, Value: string(value)}
			_ = api.publishNativeWakeSecret(plan.Repository.FullName)
			api.secretWrites = 0
			switch scenario {
			case "host-missing":
				host = nil
			case "member":
				host.MemberID++
			case "host-binding":
				host.RepositoryID++
			case "adoption":
				host.AdoptedRepositoryIDs = []int64{}
			case "destination":
				api.data[destination] = actionsVariable{
					Name: sharedDestinationVariable, Value: "unrelated"}
			case "failed-read":
				api.readFailure = destination
			case "runner":
				host.Runners[0].ID++
			}
			activations := 0
			ops := managedHostOperations{
				discover: func() (*sharedHost, error) { return host, nil },
				native: func(InitPlan, []sharedRegistration) (nativeContinuation, error) {
					activations++
					return nativeContinuation{}, nil
				},
			}
			var out bytes.Buffer
			app := App{API: api}
			err := app.resumeManagedInit(context.Background(), plan, nil, &out, ops)
			if err == nil || api.secretWrites != 0 || len(api.writes) != 0 || activations != 0 {
				t.Fatal("incomplete/unrelated configuration was reused or overwritten", err)
			}
		})
	}
}

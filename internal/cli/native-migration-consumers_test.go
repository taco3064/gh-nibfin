package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

type nativeConsumerAPI struct {
	apis  map[string]*migrationAPI
	first *migrationAPI
}

func (api nativeConsumerAPI) Call(method, path string, input, output any) error {
	for name, candidate := range api.apis {
		if path == "repos/"+name || strings.HasPrefix(path, "repos/"+name+"/") {
			return candidate.Call(method, path, input, output)
		}
	}
	return api.first.Call(method, path, input, output)
}

func (api nativeConsumerAPI) publishNativeWakeSecret(name string) error {
	return api.apis[name].publishNativeWakeSecret(name)
}

func nativeConsumersFixture(t *testing.T) (nativeConsumerAPI, sharedHost,
	nativeMigrationOperations, *int) {
	t.Helper()
	names := []string{"taco3064/nibfin-core", "taco3064/nibfin-act",
		"taco3064/nibfin-e2e", "taco3064/gh-nibfin"}
	host := sharedHost{MemberID: 42, SharedExecution: SharedExecution{names[0], 10},
		AdoptedRepositoryIDs: []int64{10, 11, 12, 13}}
	host.Runners = []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}{{31, "one"}, {32, "two"}}
	api := nativeConsumerAPI{apis: map[string]*migrationAPI{}}
	origins := []any{}
	seed, _, _, _ := readyMigrationFixture(t)
	for n, name := range names {
		data := map[string]any{}
		for path, value := range seed.data {
			data[path] = value
		}
		f := &migrationAPI{upgradeAPI: &upgradeAPI{fakeAPI: &fakeAPI{data: data},
			old: seed.old, target: seed.target}}
		for path, value := range f.data {
			if path == upgradeRepo || strings.HasPrefix(path, upgradeRepo+"/") {
				delete(f.data, path)
				f.data[strings.Replace(path, upgradeRepo, "repos/"+name, 1)] = value
			}
		}
		repo := f.data["repos/"+name].(Repository)
		repo.FullName, repo.ID = name, int64(n+10)
		f.data["repos/"+name] = repo
		destination, _ := json.Marshal(host.SharedExecution)
		f.data["repos/"+name+"/actions/variables/"+sharedDestinationVariable] =
			actionsVariable{Name: sharedDestinationVariable, Value: string(destination)}
		api.apis[name] = f
		if api.first == nil {
			api.first = f
		}
		origins = append(origins, map[string]any{"repository": name,
			"repositoryId": repo.ID, "event": map[string]any{
				"repository": name, "repositoryId": repo.ID, "memberId": 42}})
	}
	var adopted relayAdoptions
	data, _ := json.Marshal(map[string]any{"memberId": 42,
		"pin": api.first.old.Core, "origins": origins})
	_ = json.Unmarshal(data, &adopted)
	states := map[int64]*nativeWakeSetup{}
	retirements := 0
	ops := nativeMigrationOperations{
		bindings: func() (relayAdoptions, error) { return adopted, nil },
		state: func(plan InitPlan) (*nativeWakeSetup, func() error, func(), error) {
			if states[plan.Repository.ID] == nil {
				states[plan.Repository.ID] = &nativeWakeSetup{}
			}
			return states[plan.Repository.ID], func() error { return nil }, func() {}, nil
		},
		installed: func(InitPlan, Producers) (*sharedHost, []sharedRegistration, error) {
			return &host, []sharedRegistration{{ID: 31, Name: "one", Status: "online"},
				{ID: 32, Name: "two", Status: "online"}}, nil
		},
		refresh: func(InitPlan, sharedHost) error { return nil },
		retire: func() ([]byte, error) {
			retirements++
			return []byte(`{"schemaVersion":1,"status":"retired"}`), nil
		},
	}
	return api, host, ops, &retirements
}

func TestNativeMigrationObservesAllActualConsumersBeforeSharedCleanup(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		api, host, ops, retirements := nativeConsumersFixture(t)
		if unreadable {
			api.apis["taco3064/gh-nibfin"].denyRead =
				"repos/taco3064/gh-nibfin/git/ref/heads/main"
		}
		results, _, err := migrateNativeConsumers(api, host, "latest", ops, false)
		if unreadable {
			if err == nil || len(results) != 3 || *retirements != 0 {
				t.Fatal("unknown last consumer allowed cleanup", err, results, *retirements)
			}
		} else if err != nil || len(results) != 4 || *retirements != 1 {
			t.Fatal("ready consumers did not reach owned cleanup", err, results, *retirements)
		}
		for name, f := range api.apis {
			if f.secretWrites != 0 || len(f.writes) != 0 {
				t.Fatal("completed consumer wrote again", name, f.secretWrites, f.writes)
			}
		}
	}
}

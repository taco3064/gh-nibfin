package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func readyMigrationFixture(t *testing.T) (*migrationAPI, sharedHost,
	nativeMigrationOperations, *int) {
	t.Helper()
	f, host, _, _, ops := migrationFixture(t)
	host.Runners = []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}{{31, "one"}, {32, "two"}}
	recipes, err := callerRecipes(f, f.old.Core)
	if err != nil {
		t.Fatal(err)
	}
	f.installed = templatesForRecipe(f.old, host.MemberID, recipes, "issue")
	for path, body := range f.installed {
		f.baseEntries[path] = upgradeEntry{Path: path, Mode: "100644", Type: "blob",
			SHA: blobHash([]byte(body))}
		f.data[upgradeRepo+"/contents/"+path+"?ref="+strings.Repeat("a", 40)] =
			encoded([]byte(body))
	}
	commit := upgradeCommit{SHA: strings.Repeat("a", 40)}
	commit.Tree.SHA = fixtureTree(f.fakeAPI, f.baseEntries)
	f.data[upgradeRepo+"/git/commits/"+commit.SHA] = commit
	f.target = f.old
	pins, _ := json.Marshal(f.old)
	f.data["repos/"+officialCLI+"/contents/internal/cli/producers.json?ref="+
		strings.Repeat("5", 40)] = encoded(pins)
	f.data[upgradeRepo+"/actions/secrets/"+nativeWakeSecret] = nativeSecretMetadata{
		Name: nativeWakeSecret, CreatedAt: "2026-11-20", UpdatedAt: "2026-11-20"}
	refreshes := 0
	ops.refresh = func(InitPlan, sharedHost) error { refreshes++; return nil }
	ops.installed = func(plan InitPlan, pins Producers) (
		*sharedHost, []sharedRegistration, error) {
		if plan.Repository.ID != 10 || pins.Core != f.old.Core {
			t.Fatal("incorrect installed observation inputs", plan, pins)
		}
		return &host, []sharedRegistration{
			{ID: 31, Name: "one", Status: "online"},
			{ID: 32, Name: "two", Status: "online"}}, nil
	}
	return f, host, ops, &refreshes
}

func TestNativeMigrationMainWithoutLocalReceiptDoesNotRepublish(t *testing.T) {
	f, host, ops, refreshes := readyMigrationFixture(t)
	retirements, completions := 0, 0
	ops.retire = func() ([]byte, error) {
		retirements++
		return []byte(`{"schemaVersion":1,"status":"retired"}`), nil
	}
	ops.complete = func() ([]byte, error) {
		completions++
		if completions == 1 {
			return []byte(`{"status":"pending","results":[],` +
					`"githubApi":{"kind":"network","message":"private"}}`),
				errors.New("CORE_PENDING_RESULT")
		}
		return []byte(`{"status":"paused","results":[]}`), nil
	}
	for n := range 2 {
		results, continuation, err := migrateNativeConsumers(f, host, "latest", ops, true)
		if n == 0 && err == nil || n == 1 && err != nil ||
			len(results) != 1 || results[0].Status != "ready" || continuation == nil {
			t.Fatal("unfinished lifecycle did not resume", results, continuation, err)
		}
	}
	if f.secretWrites != 0 || len(f.writes) != 0 || *refreshes != 2 ||
		retirements != 2 || completions != 2 {
		t.Fatal("main/current configuration repeated completed provider writes",
			f.secretWrites, f.writes, *refreshes, retirements, completions)
	}
}

func TestNativeMigrationPartialAndUnknownPeerPreserveSharedRelay(t *testing.T) {
	f, host, _, _, ops := migrationFixture(t)
	if _, _, err := migrateNativeConsumers(f, host, "latest", ops, false); err == nil {
		t.Fatal("proposal pending was reported ready")
	}
	ops.bindings = func() (relayAdoptions, error) {
		return relayAdoptions{}, errors.New("GITHUB_API_READ_UNKNOWN")
	}
	before := len(f.writes)
	if _, _, err := migrateNativeConsumers(f, host, "latest", ops, false); err == nil ||
		len(f.writes) != before {
		t.Fatal("unobservable adoptions mutated shared routing", err, f.writes)
	}
}

func TestNativeMigrationProjectsAllFourActualAdoptions(t *testing.T) {
	host := sharedHost{MemberID: 42, AdoptedRepositoryIDs: []int64{10, 11, 12, 13}}
	var adopted relayAdoptions
	origins := []any{}
	for n, name := range []string{"taco3064/nibfin-core", "taco3064/nibfin-act",
		"taco3064/nibfin-e2e", "taco3064/gh-nibfin"} {
		origins = append(origins, map[string]any{"repository": name,
			"repositoryId": n + 10, "event": map[string]any{
				"repository": name, "repositoryId": n + 10, "memberId": 42}})
	}
	data, _ := json.Marshal(map[string]any{"memberId": 42, "origins": origins})
	_ = json.Unmarshal(data, &adopted)
	if err := nativeMigrationBindings(adopted, host); err != nil {
		t.Fatal(err)
	}
	adopted.Origins = adopted.Origins[:3]
	if err := nativeMigrationBindings(adopted, host); err == nil {
		t.Fatal("missing actual consumer accepted")
	}
}

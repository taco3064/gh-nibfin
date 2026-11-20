package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func titleMaintainedMigrationRecipes(t *testing.T, api *migrationAPI) {
	t.Helper()
	pin := api.old.Core
	recipes, err := callerRecipes(api, pin)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"issue", "issue-managed", "action-dispatch"} {
		lines := recipes[kind]
		prefix := []string{lines[0], "run-name: >-", "  Maintained operation title"}
		recipes[kind] = append(prefix, lines[1:]...)
	}
	data, err := json.Marshal(recipes)
	if err != nil {
		t.Fatal(err)
	}
	path := "repos/" + pin.Repository + "/contents/src/caller-recipes.json?ref="
	api.data[path+pin.Commit] = encoded(data)
}

func TestNativeMigrationPreparesWakeForMaintainedUntitledCallers(t *testing.T) {
	for _, route := range []string{"native", "artifact"} {
		t.Run(route, func(t *testing.T) {
			api, host, ops, _ := readyMigrationFixture(t)
			state := &nativeWakeSetup{}
			if route == "artifact" {
				api, host, _, state, ops = migrationFixture(t)
			} else {
				addNativeInitSharedCapability(api, host)
				runners := []RunnerInfo{}
				for _, slot := range host.Runners {
					runners = append(runners, RunnerInfo{ID: slot.ID, Name: slot.Name,
						OS: "Linux", Status: "online", Labels: []struct {
							Name string `json:"name"`
						}{{memberLabel(host.MemberID)}, {sharedRunnerLabel}}})
				}
				api.data[upgradeRepo+"/actions/runners?per_page=100&page=1"] = map[string]any{
					"total_count": len(runners), "runners": runners}
				ops.installed = func(plan InitPlan, pins Producers) (
					*sharedHost, []sharedRegistration, error) {
					discover := func() (*sharedHost, error) { return &host, nil }
					return readNativeInstallationWithPins(api, plan, discover, pins)
				}
			}
			titleMaintainedMigrationRecipes(t, api)
			ops.selected = &upgradeSource{Producers: api.old, maintained: true}
			plan, err := migrationUpgradePlan(api, host.Repository, "latest", ops)
			if err != nil {
				t.Fatal(err)
			}
			installation, err := nativeMigrationPlan(api, host, plan)
			if err != nil {
				t.Fatal(err)
			}
			save := func() error { return nil }
			if err := prepareNativeConsumerWake(api, host, "latest", ops, installation,
				plan, state, save); err != nil {
				t.Fatal("recognized layout could not prepare the credential", err)
			}
			if route == "artifact" && (api.secretWrites != 1 ||
				state.SecretState != "confirmed") {
				t.Fatal("active artifact route did not confirm its one write", state)
			}
			if route == "native" && (api.secretWrites != 0 || state.SecretState != "observed") {
				t.Fatal("active native route did not reuse observed configuration", state)
			}
			if len(api.writes) != 0 {
				t.Fatal("credential preparation changed unrelated resources", api.writes)
			}
			head := strings.Repeat("a", 40)
			path := upgradeRepo + "/contents/" + managedIssueWorkflow + "?ref=" + head
			body := api.installed[managedIssueWorkflow] + "# foreign edit\n"
			observed := encoded([]byte(body))
			api.data[path] = observed
			entry := api.baseEntries[managedIssueWorkflow]
			entry.SHA = observed.SHA
			api.baseEntries[managedIssueWorkflow] = entry
			commit := upgradeCommit{SHA: head}
			commit.Tree.SHA = fixtureTree(api.fakeAPI, api.baseEntries)
			api.data[upgradeRepo+"/git/commits/"+head] = commit
			observe := observeMigrationNative
			if route == "artifact" {
				observe = observeMigrationArtifact
			}
			err = observe(api, host, installation, "latest", ops)
			if err == nil || err.Error() != "INSTALLED_ISSUE_CALLER_NOT_RECOGNIZED" {
				t.Fatal("a changed caller passed the current route readback", err)
			}
		})
	}
}

package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func sourceComparison(f *fakeAPI, left, right SourcePin, status string) {
	ahead, behind := 0, 0
	if status == "ahead" {
		ahead = 1
	} else if status == "behind" {
		behind = 1
	}
	f.data["repos/"+left.Repository+"/compare/"+left.Commit+"..."+right.Commit] =
		map[string]any{"status": status, "ahead_by": ahead, "behind_by": behind}
}

func TestNativeSourceSelectionKeepsOneWholeNonDowngradedPair(t *testing.T) {
	for _, mode := range []string{"release-newer", "current-newer", "mixed", "diverged"} {
		t.Run(mode, func(t *testing.T) {
			f := upgradeFixture(t, true, true, true)
			current := upgradeSource{Kind: "installed-current", Producers: f.old,
				Execution: &SharedExecution{"example/project", 10}}
			release := upgradeSource{Version: "v1.2.3", Producers: f.target}
			core, action := "ahead", "ahead"
			switch mode {
			case "current-newer":
				core, action = "behind", "behind"
			case "mixed":
				action = "behind"
			case "diverged":
				core = "diverged"
			}
			sourceComparison(f.fakeAPI, current.Producers.Core, release.Producers.Core, core)
			sourceComparison(f.fakeAPI, current.Producers.Action, release.Producers.Action,
				action)
			selected, err := chooseNativeSource(f, current, &release)
			if mode == "mixed" || mode == "diverged" {
				if err == nil || len(f.writes) != 0 {
					t.Fatal("mixed or diverged pair acquired write authority", selected, err)
				}
				return
			}
			want := release.Producers
			if mode == "current-newer" {
				want = current.Producers
			}
			if err != nil || !sameProducerSources(selected.Producers, want) ||
				len(f.writes) != 0 {
				t.Fatal("selector mixed source pairs or wrote during observation", selected, err)
			}
		})
	}
}

func TestNativeCurrentSourceDoesNotRequireAReleaseOrOldArtifact(t *testing.T) {
	f := upgradeFixture(t, true, true, true)
	current := upgradeSource{Kind: "installed-current", Producers: f.old,
		Execution: &SharedExecution{"example/project", 10}}
	selected, err := chooseNativeSource(f, current, nil)
	if err != nil || !sameProducerSources(selected.Producers, f.old) {
		t.Fatal(err, selected)
	}
	delete(f.data, "repos/"+officialCore+"/actions/artifacts/101")
	plan, err := planNativeUpgradeTarget(f, "example/project", "latest", current)
	if err != nil || plan.Target.Kind != "installed-current" {
		t.Fatal("old artifact locator became an admission gate", err)
	}
	data, _ := json.Marshal(plan.Target)
	if strings.Contains(string(data), `"version"`) ||
		strings.Contains(string(data), `"cli"`) || strings.Contains(string(data), `"head"`) {
		t.Fatal("current target impersonated a release or captured a snapshot", string(data))
	}
}

func TestNativeMigrationSelectsOneTargetForAllConsumers(t *testing.T) {
	api, host, ops, retired := nativeConsumersFixture(t)
	selections := 0
	ops.target = func(sharedHost, string) (upgradeSource, error) {
		selections++
		return upgradeSource{Kind: "installed-current", Producers: api.first.old,
			Execution: &host.SharedExecution}, nil
	}
	original := ops.refresh
	ops.refresh = func(plan InitPlan, actual sharedHost) error {
		// A release changes while peers are processed; it is not this operation's
		// target authority and must not be re-resolved by another consumer.
		for _, f := range api.apis {
			delete(f.data, "repos/"+officialCLI+"/releases/latest")
		}
		return original(plan, actual)
	}
	results, _, err := migrateNativeConsumers(api, host, "latest", ops, false)
	if err != nil || len(results) != 4 || selections != 1 || *retired != 1 {
		t.Fatal("consumers reselected or mixed targets", err, selections, results)
	}
}

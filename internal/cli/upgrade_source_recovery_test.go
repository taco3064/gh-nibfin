package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestUpgradeRetainedSourceIntentRefusesChangedCLIWithoutManagedChanges(t *testing.T) {
	states := []string{"unpublished", "behind", "rebased", "legacy", "legacy-rebased"}
	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			f := upgradeFixture(t, false, true)
			original := fixtureUpgradePlan(t, f)
			if state == "unpublished" {
				f.refuseWrite = upgradeRepo + "/pulls"
			}
			_, err := applyUpgrade(f, original, "latest")
			if (err != nil) != (state == "unpublished") {
				t.Fatal(err)
			}
			f.refuseWrite = ""
			if strings.HasPrefix(state, "legacy") {
				pull := f.data[upgradeRepo+"/pulls/9"].(upgradePull)
				key := upgradeRepo + "/git/commits/" + pull.Head.SHA
				commit := f.data[key].(upgradeCommit)
				commit.Message = strings.Split(commit.Message, "Sources: ")[0]
				f.data[key] = commit
				if _, err := applyUpgrade(f, original, "latest"); err != nil {
					t.Fatal("original legacy binding must remain recoverable", err)
				}
			}
			current := original
			if state == "behind" || strings.Contains(state, "rebased") {
				current = advanceUpgradeBase(t, f, original)
			}
			if strings.Contains(state, "rebased") {
				if _, err := applyUpgrade(f, current, "latest"); err != nil {
					t.Fatal(err)
				}
			}
			// A new verified CLI commit may carry the exact same producer pins.
			// Generated managed files alone cannot distinguish the desired source.
			cliCommit := strings.Repeat("6", 40)
			base := "repos/" + officialCLI
			f.data[base+"/git/ref/tags/v1.2.3"] = map[string]any{
				"ref": "refs/tags/v1.2.3", "object": map[string]string{
					"type": "commit", "sha": cliCommit}}
			f.data[base+"/commits/"+cliCommit] = map[string]string{"sha": cliCommit}
			pins, _ := json.Marshal(f.target)
			f.data[base+"/contents/internal/cli/producers.json?ref="+cliCommit] =
				encoded(pins)
			changed, err := planUpgrade(f, "example/project", "latest")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(current.Changes, changed.Changes) ||
				changed.Target.CLI == current.Target.CLI {
				t.Fatal("fixture must change only source intent, not managed output")
			}
			writes := len(f.writes)
			result, err := applyUpgrade(f, changed, "latest")
			if err == nil || !strings.Contains(err.Error(), "SOURCE_INTENT") ||
				result.PR != "" || len(f.writes) != writes {
				t.Fatal("changed source must remain pending without writes", result,
					err, f.writes[writes:])
			}
			// Exercise the real public argument and pending-result boundary too.
			app := App{API: f, Process: upgradeNoProcess{t}}
			var out bytes.Buffer
			err = app.Run([]string{"upgrade", "--repo", "example/project", "--apply"},
				&out)
			if err == nil || !strings.Contains(out.String(), "SOURCE_INTENT") ||
				len(f.writes) != writes {
				t.Fatal(out.String(), err, f.writes[writes:])
			}
		})
	}
}

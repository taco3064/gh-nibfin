package snapshot

import (
	"encoding/json"
	"testing"
)

func TestPauseLabelIsNativeObservationNotCoreVerdict(t *testing.T) {
	for _, present := range []bool{false, true} {
		api, _ := fixture(t)
		var issues []map[string]any
		path := page(issueList, 1)
		if err := json.Unmarshal(api.routes[path], &issues); err != nil {
			t.Fatal(err)
		}
		if present {
			labels := issues[0]["labels"].([]any)
			issues[0]["labels"] = append(labels, map[string]string{"name": "pause"})
		}
		api.routes[path] = wire(issues)
		result := (Provider{API: api}).Collect(selected)
		issue := result.Issues.Items[0]
		if issue.PauseLabel == nil || issue.PauseLabel.Present != present ||
			issue.PauseLabel.Actor != "Unknown" || issue.Core.Pause.Availability != Unknown {
			t.Fatal(issue)
		}
	}
}

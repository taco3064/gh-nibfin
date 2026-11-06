package snapshot

import (
	"encoding/json"
	"testing"
)

func TestFullArrayEndAndChangingTotals(t *testing.T) {
	f, source := fixture(t)
	items := []json.RawMessage{}
	for i := 1; i <= 100; i++ {
		items = append(items, numbered(source["dependency"], i, int64(1000+i)))
	}
	f.routes[page(dependencyList, 1)] = wire(items)
	f.routes[page(dependencyList, 2)] = wire([]any{})
	s := collect(f, Limits{})
	if !s.Issues.Items[0].BlockedBy.Status.Complete {
		t.Fatal("an empty second page should establish the end")
	}
	f, source = fixture(t)
	items = nil
	for i := 1; i <= 100; i++ {
		items = append(items, numbered(source["run"], i, int64(1000+i)))
	}
	f.routes[page(runList, 1)] = wire(map[string]any{
		"total_count": 101, "workflow_runs": items})
	f.routes[page(runList, 2)] = wire(map[string]any{
		"total_count": 102, "workflow_runs": []json.RawMessage{source["run"]}})
	s = collect(f, Limits{})
	if s.Runs.Status.Reason != Changed || len(s.Runs.Items) != 100 {
		t.Fatal("changed total was accepted")
	}
	assertReads(t, f)
}

package dashboard

import (
	"encoding/json"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

const overviewByteLimit = 2 * 1024 * 1024

// Reserve room for a failure envelope so oversized new data cannot silently
// suppress freshness/coverage updates or erase previously observed rows.
func encodeOverview(old, next snapshot.Overview, id uint64) (
	snapshot.Overview, []byte) {
	value := reconcileOverview(old, next)
	data, err := json.Marshal(OverviewUpdate{id, "reset", value})
	if err == nil && len(data) <= overviewByteLimit-64*1024 {
		return value, data
	}
	value = next
	value.Work = nil
	value.Errors = []string{"PROJECTION_LIMIT"}
	value.Repositories.Items, value.Issues.Items, value.Runners.Items = nil, nil, nil
	for _, status := range []*snapshot.Status{&value.Discovery,
		&value.Repositories.Status, &value.Issues.Status, &value.Runners.Status} {
		*status = snapshot.Status{Availability: snapshot.Unknown, Complete: false,
			Reason: snapshot.Limit, ObservedAt: next.ObservedAt, Source: "projection"}
	}
	value = reconcileOverview(old, value)
	data, err = json.Marshal(OverviewUpdate{id, "reset", value})
	if err != nil || len(data) > overviewByteLimit {
		return old, nil
	}
	return value, data
}

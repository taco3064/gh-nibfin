package snapshot_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

// This consumer uses only the exported interface. It needs no provider auth.
func TestConsumerJSONDistinguishesEmptyUnknownDeniedAndBindings(t *testing.T) {
	const document = `{
	  "schema_version": 1,
	  "repository": {"status": {"availability":"known", "complete":true},
	    "items":[{"id":10,"name":"example/project",
	      "url":"https://github.com/example/project"}]},
	  "issues": {"status":{"availability":"known","complete":true},"items":[]},
	  "runs": {"status":{"availability":"unknown","complete":false,
	    "reason":"not_found_or_concealed"},"items":null},
	  "pull_requests": {"status":{"availability":"denied","complete":false,
	    "reason":"permission_denied"},"items":null},
	  "core":{"completion":{"availability":"unknown","verification":"unverified",
	    "reason":"no_supported_reader","source":"none","binding":null}}
	}`
	var s snapshot.Snapshot
	if err := json.Unmarshal([]byte(document), &s); err != nil {
		t.Fatal(err)
	}
	if s.Issues.Items == nil || s.Runs.Items != nil ||
		s.PullRequests.Status.Availability != snapshot.Denied ||
		s.Core.Completion.Binding != nil || s.Repository.Items[0].ID != 10 {
		t.Fatal("consumer cannot distinguish native facts and unavailable evidence")
	}
	data, err := json.Marshal(s)
	if err != nil || !strings.Contains(string(data), `"items":[]`) ||
		!strings.Contains(string(data), `"items":null`) {
		t.Fatal("round trip lost nullability")
	}
	// An unavailable transport is an ordinary typed result, not a panic.
	result := (snapshot.Provider{Now: func() time.Time {
		return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	}}).Collect("example/project")
	if result.Repository.Status.Availability != snapshot.Unknown {
		t.Fatal("missing transport claimed available")
	}
}

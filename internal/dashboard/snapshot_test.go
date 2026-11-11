package dashboard

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type nativeFixture struct {
	mu       sync.Mutex
	routes   map[string]json.RawMessage
	failures map[string]string
	calls    []string
}

func (f *nativeFixture) Call(method, path string, input, output any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+" "+path)
	if method != "GET" || input != nil {
		return errors.New("unexpected write")
	}
	if message := f.failures[path]; message != "" {
		return errors.New(message)
	}
	raw, ok := f.routes[path]
	if !ok {
		return errors.New("NOT_FOUND")
	}
	return json.Unmarshal(raw, output)
}

const selectedRepository = "example/project"
const issuesEndpoint = "repos/example/project/issues?state=all&sort=created&direction=asc"
const pullsEndpoint = "repos/example/project/pulls?state=all&sort=created&direction=asc"
const runsEndpoint = "repos/example/project/actions/runs"
const dependencyEndpoint = "repos/example/project/issues/1/dependencies/blocked_by"
const fixtureRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const checksEndpoint = "repos/example/project/commits/" + fixtureRevision +
	"/check-runs?filter=all"

func firstPage(path string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "per_page=100&page=1"
}

func encoded(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func httpFixture(t *testing.T) *nativeFixture {
	t.Helper()
	data, err := os.ReadFile("../snapshot/testdata/native.json")
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	return &nativeFixture{failures: map[string]string{}, routes: map[string]json.RawMessage{
		"repos/example/project":        source["repository"],
		"repos/other/project":          source["other_repository"],
		"repos/other/project/issues/1": source["dependency"],
		firstPage(issuesEndpoint):      encoded([]json.RawMessage{source["issue"]}),
		firstPage(pullsEndpoint):       encoded([]json.RawMessage{source["pull"]}),
		firstPage(dependencyEndpoint):  encoded([]json.RawMessage{source["dependency"]}),
		firstPage(runsEndpoint): encoded(map[string]any{
			"total_count": 1, "workflow_runs": []json.RawMessage{source["run"]}}),
		firstPage(checksEndpoint): encoded(map[string]any{
			"total_count": 1, "check_runs": []json.RawMessage{source["check"]}}),
	}}
}

type retainedSnapshot struct{ value snapshot.Snapshot }

func (p retainedSnapshot) Collect(string) snapshot.Snapshot { return p.value }

func TestHTTPPreservesNativeSnapshotSemantics(t *testing.T) {
	instant := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for _, kind := range []string{"native", "empty", "partial", "denied", "offline",
		"stale", "unresolved"} {
		t.Run(kind, func(t *testing.T) {
			fixture := httpFixture(t)
			switch kind {
			case "empty":
				fixture.routes[firstPage(issuesEndpoint)] = encoded([]any{})
				fixture.routes[firstPage(pullsEndpoint)] = encoded([]any{})
				fixture.routes[firstPage(runsEndpoint)] = encoded(map[string]any{
					"total_count": 0, "workflow_runs": []any{}})
			case "partial":
				// Retain the observed first issue when a later record is invalid.
				var issues []json.RawMessage
				_ = json.Unmarshal(fixture.routes[firstPage(issuesEndpoint)], &issues)
				fixture.routes[firstPage(issuesEndpoint)] = encoded(append(issues,
					json.RawMessage(`{"id":102,"number":2}`)))
			case "denied":
				fixture.failures["repos/example/project"] = "PERMISSION_DENIED"
			case "offline":
				fixture.failures["repos/example/project"] = "token /private/host raw agent log"
			case "unresolved":
				fixture.failures["repos/other/project"] = "PERMISSION_DENIED"
			}
			provider := snapshot.Provider{API: fixture,
				Now: func() time.Time { return instant }}
			expected := provider.Collect(selectedRepository)
			responseTime := instant
			var collector Collector = provider
			if kind == "stale" {
				responseTime = instant.Add(5 * time.Minute)
				collector = retainedSnapshot{expected}
				expected = expected.At(responseTime)
			}
			s := launch(t, Config{Provider: collector,
				Now: func() time.Time { return responseTime }})
			code, body := request(t, s, proof(s))
			if code != 200 {
				t.Fatal(code, body)
			}
			var actual snapshot.Snapshot
			if err := json.Unmarshal([]byte(body), &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("HTTP changed provider semantics: %+v != %+v", actual, expected)
			}
			if actual.SchemaVersion != 1 || actual.Core.Completion.Verification != "unverified" ||
				actual.Core.Completion.Binding != nil {
				t.Fatal("HTTP invented authority")
			}
			switch kind {
			case "empty":
				if actual.Issues.Items == nil || len(actual.Issues.Items) != 0 ||
					!actual.Issues.Status.Complete {
					t.Fatal("empty became unavailable")
				}
			case "partial":
				if actual.Issues.Status.Complete || len(actual.Issues.Items) != 1 {
					t.Fatal("partial became complete or lost facts")
				}
			case "denied":
				if actual.Repository.Status.Availability != snapshot.Denied {
					t.Fatal("denied became empty success")
				}
			case "offline":
				if actual.Repository.Status.Availability != snapshot.Unknown ||
					actual.Issues.Items != nil {
					t.Fatal("offline became empty success")
				}
			case "stale":
				if actual.Issues.Status.Reason != snapshot.Stale ||
					actual.Issues.Items[0].BlockedBy.Items[0].To.Resolution.Complete {
					t.Fatal("stale nested evidence remained current")
				}
			case "unresolved":
				target := actual.Issues.Items[0].BlockedBy.Items[0].To
				if target.Resolution.Availability != snapshot.Denied ||
					target.Identity.Repository.ID != 0 || target.Identity.ID != 201 {
					t.Fatal("unresolved edge fabricated or erased")
				}
			case "native":
				if strings.Contains(body, "<script>") || !strings.Contains(body, `\u003cscript`) {
					t.Fatal("hostile text not serialized safely")
				}
			}
			for _, private := range []string{"/private/", "raw agent log", "javascript:",
				"evil.example", "secret_token"} {
				if strings.Contains(body, private) {
					t.Fatal("unsafe field exposed", private)
				}
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			for _, call := range fixture.calls {
				if !strings.HasPrefix(call, "GET repos/") {
					t.Fatal("unexpected mutation", call)
				}
			}
		})
	}
}

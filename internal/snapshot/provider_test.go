package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const selected = "example/project"
const issueList = "repos/" + selected +
	"/issues?state=all&sort=created&direction=asc"
const pullList = "repos/" + selected +
	"/pulls?state=all&sort=created&direction=asc"
const runList = "repos/" + selected + "/actions/runs"
const dependencyList = "repos/" + selected + "/issues/1/dependencies/blocked_by"
const revision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const checkList = "repos/" + selected + "/commits/" + revision +
	"/check-runs?filter=all"

var instant = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

type fixtureAPI struct {
	routes map[string]json.RawMessage
	errors map[string]error
	calls  []string
	after  func(string)
}

func (f *fixtureAPI) Call(method, path string, input, output any) error {
	f.calls = append(f.calls, method+" "+path)
	if method != "GET" || input != nil {
		return errors.New("unexpected write")
	}
	if f.after != nil {
		f.after(path)
	}
	if err := f.errors[path]; err != nil {
		return err
	}
	value, ok := f.routes[path]
	if !ok {
		return errors.New("NOT_FOUND")
	}
	if json.Unmarshal(value, output) != nil {
		return errors.New("INVALID_PROVIDER_RESULT")
	}
	return nil
}

func wire(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func page(path string, number int) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%sper_page=100&page=%d", path, separator, number)
}

func fixture(t *testing.T) (*fixtureAPI, map[string]json.RawMessage) {
	t.Helper()
	data, err := os.ReadFile("testdata/native.json")
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	f := &fixtureAPI{routes: map[string]json.RawMessage{}, errors: map[string]error{}}
	f.routes["repos/"+selected] = source["repository"]
	f.routes["repos/other/project"] = source["other_repository"]
	f.routes["repos/other/project/issues/1"] = source["dependency"]
	f.routes[page(issueList, 1)] = wire([]json.RawMessage{source["issue"]})
	f.routes[page(dependencyList, 1)] = wire([]json.RawMessage{source["dependency"]})
	f.routes[page(pullList, 1)] = wire([]json.RawMessage{source["pull"]})
	f.routes[page(runList, 1)] = wire(map[string]any{
		"total_count": 1, "workflow_runs": []json.RawMessage{source["run"]}})
	f.routes[page(checkList, 1)] = wire(map[string]any{
		"total_count": 1, "check_runs": []json.RawMessage{source["check"]}})
	return f, source
}

func collect(f *fixtureAPI, limits Limits) Snapshot {
	return (Provider{API: f, Now: func() time.Time { return instant },
		Limits: limits}).Collect(selected)
}

func assertReads(t *testing.T, f *fixtureAPI) {
	t.Helper()
	for _, call := range f.calls {
		if !strings.HasPrefix(call, "GET repos/") ||
			strings.ContainsAny(call, "\n\r\\") || strings.Contains(call, "..") {
			t.Fatalf("unsafe call: %q", call)
		}
	}
}

func assertUnknown(t *testing.T, core Core) {
	t.Helper()
	for _, claim := range []Claim{core.Completion, core.Pause, core.Execution} {
		if claim.Availability != Unknown || claim.Verification != "unverified" ||
			claim.Reason != NoReader || claim.Binding != nil {
			t.Fatalf("invented evidence: %+v", claim)
		}
	}
}

func TestNativeProjectionAndJSONContract(t *testing.T) {
	f, source := fixture(t)
	s := collect(f, Limits{})
	if !s.Repository.Status.Complete || !s.Issues.Status.Complete ||
		!s.PullRequests.Status.Complete || !s.Runs.Status.Complete {
		t.Fatalf("incomplete fixture: %+v", s)
	}
	issue := s.Issues.Items[0]
	var original nativeIssue
	_ = json.Unmarshal(source["issue"], &original)
	if issue.Title != original.Title || issue.State != original.State ||
		!reflect.DeepEqual(issue.Assignees, original.Assignees) ||
		!reflect.DeepEqual(issue.Progress, []string{"scope", "shaped", "delivered"}) {
		t.Fatalf("lost source facts: %+v", issue)
	}
	edge := issue.BlockedBy.Items[0]
	if edge.From.Number != edge.To.Identity.Number ||
		edge.From.Repository.ID == edge.To.Identity.Repository.ID ||
		edge.To.Identity.ID != 201 || !edge.To.Resolution.Complete {
		t.Fatalf("cross repository identity lost: %+v", edge)
	}
	pr := s.PullRequests.Items[0]
	run := s.Runs.Items[0]
	if pr.Identity.ID != 301 || pr.MergedAt == nil || pr.Head != revision ||
		len(pr.Checks.Items) != 1 || pr.Checks.Items[0].ID != 501 ||
		run.ID != 401 || run.Attempt != 2 || *run.Conclusion != "success" ||
		run.PullRequests[0] != pr.Identity {
		t.Fatalf("activity facts incorrect: %+v %+v", pr, run)
	}
	assertUnknown(t, s.Core)
	assertUnknown(t, issue.Core)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private/token", "evil.example", "javascript:"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("unsafe output: %s", secret)
		}
	}
	if !strings.Contains(string(data), `\u003cscript\u003e`) {
		t.Fatal("JSON must keep markup inert")
	}
	var roundTrip Snapshot
	if err := json.Unmarshal(data, &roundTrip); err != nil ||
		!reflect.DeepEqual(s, roundTrip) {
		t.Fatalf("JSON contract changed: %v", err)
	}
	assertReads(t, f)
}

func numbered(source json.RawMessage, number int, id int64) json.RawMessage {
	var value map[string]any
	_ = json.Unmarshal(source, &value)
	value["id"], value["number"] = id, number
	if _, ok := value["repository_url"]; ok {
		value["url"] = fmt.Sprintf("%s/issues/%d", value["repository_url"], number)
	}
	return wire(value)
}

func TestIssuePaginationOver100ExcludesPRAndKeepsClosed(t *testing.T) {
	f, source := fixture(t)
	items := []json.RawMessage{}
	for i := 1; i <= 101; i++ {
		raw := numbered(source["issue"], i, int64(i+1000))
		if i == 101 {
			var value map[string]any
			_ = json.Unmarshal(raw, &value)
			value["state"] = "closed"
			raw = wire(value)
		}
		items = append(items, raw)
		path := fmt.Sprintf("repos/%s/issues/%d/dependencies/blocked_by", selected, i)
		f.routes[page(path, 1)] = wire([]any{})
	}
	pr := numbered(source["issue"], 102, 1102)
	var entry map[string]any
	_ = json.Unmarshal(pr, &entry)
	entry["pull_request"] = map[string]any{"url": "https://api.github.com/ignored"}
	f.routes[page(issueList, 1)] = wire(items[:100])
	f.routes[page(issueList, 2)] = wire([]json.RawMessage{items[100], wire(entry)})
	s := collect(f, Limits{})
	if !s.Issues.Status.Complete || len(s.Issues.Items) != 101 ||
		s.Issues.Items[100].State != "closed" {
		t.Fatalf("pagination lost issues: %+v count %d", s.Issues.Status,
			len(s.Issues.Items))
	}
	if !reflect.DeepEqual(s.Issues.Items[0].BlockedBy.Items, []Edge{}) {
		t.Fatal("complete empty differs from unavailable")
	}
	assertReads(t, f)
}

func TestEveryCollectionPaginatesAndRejectsLaterFailure(t *testing.T) {
	for _, kind := range []string{"issues", "dependencies", "pulls", "runs", "checks"} {
		for _, failure := range []string{"none", "denied", "malformed", "repeat", "limit"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				f, source := fixture(t)
				path, field, key := issueList, "", "issue"
				switch kind {
				case "dependencies":
					path, key = dependencyList, "dependency"
				case "pulls":
					path, key = pullList, "pull"
				case "runs":
					path, field, key = runList, "workflow_runs", "run"
				case "checks":
					path, field, key = checkList, "check_runs", "check"
				}
				items := []json.RawMessage{}
				for i := 1; i <= 101; i++ {
					raw := numbered(source[key], i, int64(i+1000))
					items = append(items, raw)
					if kind == "dependencies" {
						p := fmt.Sprintf("repos/other/project/issues/%d", i)
						f.routes[p] = raw
					}
					if kind == "issues" {
						p := issuePath(selected, i) + "/dependencies/blocked_by"
						f.routes[page(p, 1)] = wire([]any{})
					}
				}
				wrap := func(items []json.RawMessage) json.RawMessage {
					if field == "" {
						return wire(items)
					}
					return wire(map[string]any{"total_count": 101, field: items})
				}
				f.routes[page(path, 1)] = wrap(items[:100])
				f.routes[page(path, 2)] = wrap(items[100:])
				limits := Limits{}
				switch failure {
				case "denied":
					f.errors[page(path, 2)] = errors.New("PERMISSION_DENIED")
				case "malformed":
					f.routes[page(path, 2)] = wire(map[string]any{"invalid": true})
				case "repeat":
					f.routes[page(path, 2)] = wrap(items[:1])
				case "limit":
					limits.Pages = 1
				}
				s := collect(f, limits)
				status, count := s.Issues.Status, len(s.Issues.Items)
				switch kind {
				case "dependencies":
					status = s.Issues.Items[0].BlockedBy.Status
					count = len(s.Issues.Items[0].BlockedBy.Items)
				case "pulls":
					status, count = s.PullRequests.Status, len(s.PullRequests.Items)
				case "runs":
					status, count = s.Runs.Status, len(s.Runs.Items)
				case "checks":
					status = s.PullRequests.Items[0].Checks.Status
					count = len(s.PullRequests.Items[0].Checks.Items)
				}
				if failure == "none" {
					if !status.Complete || count != 101 {
						t.Fatalf("pagination failed: %+v count %d", status, count)
					}
				} else if status.Complete || status.Availability == Known || count != 100 {
					t.Fatalf("truncated collection claimed complete: %+v %d", status, count)
				}
				assertReads(t, f)
			})
		}
	}
}

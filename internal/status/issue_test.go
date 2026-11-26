package status

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
)

// single is a single-Issue answer: item plus state and assignees.
func single(repo string, n int, state string, assignees []string, labels ...string) map[string]any {
	it := item(repo, n, labels...)
	it["state"] = state
	as := []map[string]string{}
	for _, a := range assignees {
		as = append(as, map[string]string{"login": a})
	}
	it["assignees"] = as
	return it
}

func readIssue(t *testing.T, repo string, n int) (Issue, bool, *op.Error) {
	t.Helper()
	return ReadIssue(context.Background(), "github.com", "me", op.RunnersReport{Repo: "github.com/e/exec"}, repo, n)
}

func TestReadIssueListed(t *testing.T) {
	routes := map[string]any{
		"repos/a/one/issues/3": single("a/one", 3, "open", []string{"other", "ME"}, "Scope", "pending", "pause"),
		// a/one#3 was created 2026-11-04.
		runsPath("a/one", ">=2026-11-04T00:00:00+00:00", 1): map[string]any{"workflow_runs": []any{
			runJSONOf("Nibfin · a/one#3 · wake", "2026-11-05T00:00:00Z", "https://github.com/a/one/actions/runs/1"),
			runJSONOf("Nibfin · a/one#4 · wake", "2026-11-06T00:00:00Z", "https://github.com/a/one/actions/runs/2"),
		}},
		runsPath("e/exec", ">=2026-11-04T00:00:00+00:00", 1): map[string]any{"workflow_runs": []any{
			runJSONOf("Nibfin · a/one#3 · deliver", "2026-11-07T00:00:00Z", "https://github.com/e/exec/actions/runs/9"),
		}},
		"repos/a/one/issues/3/events?per_page=100&page=1": []any{
			map[string]any{"event": "labeled", "created_at": "2026-11-05T00:00:00Z", "actor": map[string]any{"id": 7}, "label": map[string]any{"name": "pending"}},
		},
		"repos/a/one/issues/3/comments?per_page=100&page=1": []any{
			map[string]any{"id": 1, "user": map[string]any{"id": 7}, "body": pendingMarker + "\nwhy", "html_url": "https://github.com/a/one/issues/3#c1", "created_at": "2026-11-05T00:00:01Z"},
		},
	}
	fx := setup(t, routes)
	fx.graph.repo("a/one", "WRITE").blockers[3] = []map[string]any{blocker("b/two", 9, "OPEN")}

	is, listed, err := readIssue(t, "a/one", 3)
	if err != nil || !listed {
		t.Fatalf("ReadIssue = %v, %v", listed, err)
	}
	if is.Number != 3 || !is.Pause || !is.Pending || is.PendingComment != "https://github.com/a/one/issues/3#c1" {
		t.Errorf("issue = %+v", is)
	}
	if !reflect.DeepEqual(is.Labels, []string{"Scope", "pending", "pause"}) {
		t.Errorf("labels = %v", is.Labels)
	}
	if len(is.BlockedBy) != 1 || is.BlockedBy[0].Repo != "b/two" || is.BlockedBy[0].Number != 9 || is.BlockedByError != nil {
		t.Errorf("blockers = %+v, %v", is.BlockedBy, is.BlockedByError)
	}
	var urls []string
	for _, r := range is.Runs {
		urls = append(urls, r.URL)
	}
	if !reflect.DeepEqual(urls, []string{"https://github.com/e/exec/actions/runs/9", "https://github.com/a/one/actions/runs/1"}) || is.RunsError != nil {
		t.Errorf("runs = %v, %v", urls, is.RunsError)
	}
	if !reflect.DeepEqual(fx.hosts, []string{"github.com"}) {
		t.Errorf("REST hosts = %v", fx.hosts)
	}
	for _, g := range fx.gh.gets {
		if strings.HasPrefix(g, "search/") || g == "user" || strings.Contains(g, "/contents/") {
			t.Errorf("ReadIssue read %s", g)
		}
	}
}

// TestListedRequest: an open request Issue without scope is in the person
// view (the Scope stage), as a scope Issue is.
func TestListedRequest(t *testing.T) {
	for name, labels := range map[string][]string{
		"request":         {"request"},
		"Request":         {"Request", "bug"},
		"request & scope": {"request", "scope"},
	} {
		b, err := json.Marshal(single("a/one", 3, "open", []string{"me"}, labels...))
		if err != nil {
			t.Fatal(err)
		}
		var j issueStateJSON
		if err := json.Unmarshal(b, &j); err != nil {
			t.Fatal(err)
		}
		if !listedIssue(j, "a/one", "me") {
			t.Errorf("%s: not listed", name)
		}
	}
}

func TestReadIssueNotListed(t *testing.T) {
	pr := single("a/one", 3, "open", []string{"me"}, "scope")
	pr["pull_request"] = map[string]any{}
	moved := single("a/two", 3, "open", []string{"me"}, "scope")
	for name, v := range map[string]any{
		"closed":              single("a/one", 3, "closed", []string{"me"}, "scope"),
		"unassigned":          single("a/one", 3, "open", []string{"other"}, "scope"),
		"no scope or request": single("a/one", 3, "open", []string{"me"}, "pause"),
		"pr":                  pr,
		"moved":               moved,
		"404":                 &api.HTTPError{StatusCode: http.StatusNotFound, Message: "Not Found"},
		"410":                 &api.HTTPError{StatusCode: http.StatusGone, Message: "Gone"},
	} {
		t.Run(name, func(t *testing.T) {
			fx := setup(t, map[string]any{"repos/a/one/issues/3": v})
			is, listed, err := readIssue(t, "a/one", 3)
			if listed || err != nil || is.Number != 0 {
				t.Errorf("ReadIssue = %+v, %v, %v", is, listed, err)
			}
			if len(fx.gh.gets) != 1 || len(fx.graph.queries) != 0 {
				t.Errorf("reads = %v, %d queries", fx.gh.gets, len(fx.graph.queries))
			}
		})
	}
}

func TestReadIssueFailure(t *testing.T) {
	setup(t, map[string]any{"repos/a/one/issues/3": &api.HTTPError{StatusCode: http.StatusBadGateway, Message: "bad"}})
	_, listed, err := readIssue(t, "a/one", 3)
	if listed || err == nil || err.Code != op.CodeGitHubAPIFailed || !strings.Contains(err.Error(), "a/one#3") {
		t.Errorf("ReadIssue = %v, %v", listed, err)
	}

	setup(t, nil)
	newREST = func(string) (templates.RESTClient, error) { return nil, errors.New("no login") }
	if _, _, err := readIssue(t, "a/one", 3); err == nil || err.Code != op.CodeAuthUnavailable {
		t.Errorf("no login = %v", err)
	}
}

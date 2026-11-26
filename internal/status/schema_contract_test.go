package status

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// compileSchema compiles the committed schema file, not Schema(), so the
// published bytes are what is tested, with an independent validator.
func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	b, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	const loc = "https://github.com/taco3064/gh-nibfin/schema/status.schema.json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource(loc, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(loc)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// outputOf prints res as status --json does.
func outputOf(t *testing.T, res any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func validate(s *jsonschema.Schema, out []byte) error {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(out))
	if err != nil {
		return err
	}
	return s.Validate(v)
}

func failedHost(context.Context) op.HostReport {
	e := func(code string) *op.Error { return &op.Error{Code: code, Err: fmt.Errorf("%s happened", code)} }
	return op.HostReport{
		Host:      op.HostInfo{Error: e(op.CodePodmanMissing)},
		Memory:    op.MemoryReport{Error: e(op.CodePodmanMissing)},
		Runners:   op.RunnersReport{Runners: []op.RunnerState{}, Error: e(op.CodeMachineStopped), GitHubError: e(op.CodeGitHubAPIFailed)},
		Providers: []op.ProviderReport{{Provider: "claude", Error: e(op.CodePodmanFailed)}},
		Autostart: op.AutostartReport{Kind: "task", Error: e(op.CodeAutostartFailed)},
	}
}

func TestSchemaContract(t *testing.T) {
	s := compileSchema(t)
	check := func(name string, res op.Result, keys ...string) {
		t.Helper()
		out := outputOf(t, res)
		if err := validate(s, out); err != nil {
			t.Errorf("%s: invalid against %s: %v\n%s", name, SchemaFile, err, out)
		}
		for _, k := range keys {
			if !bytes.Contains(out, []byte(`"`+k+`"`)) {
				t.Errorf("%s: the scenario lacks %q", name, k)
			}
		}
	}

	// Person view, every part read.
	routes := personRoutes(item("a/one", 3, "scope", "pending"), item("b/two", 7, "request"))
	routes["repos/a/one/contents/.github/workflows/nibfin-job.yml"] = file(jobYML(imgA))
	routes["repos/a/one/issues/3/events?per_page=100&page=1"] = []any{labeled(7, "pending", "2026-11-20T00:00:00Z")}
	routes["repos/a/one/issues/3/comments?per_page=100&page=1"] = []any{comment(5, 7, marker+"x", "2026-11-20T00:00:01Z")}
	routes[runsPath("a/one", ">=2026-11-04T00:00:00+00:00", 1)] = map[string]any{"workflow_runs": []any{
		runJSONOf("Nibfin · a/one#3 · wake", "2026-11-05T00:00:00Z", "u1")}}
	routes[runsPath("b/two", ">=2026-11-08T00:00:00+00:00", 1)] = map[string]any{"workflow_runs": []any{}}
	routes[runsPath("e/exec", ">=2026-11-04T00:00:00+00:00", 1)] = map[string]any{"workflow_runs": []any{
		map[string]any{"name": nil, "display_title": "Nibfin · a/one#3 · deliver", "status": "in_progress", "conclusion": nil,
			"created_at": "2026-11-06T00:00:00Z", "html_url": "u2"}}}
	fx := setup(t, routes)
	readHost = func(context.Context) op.HostReport {
		return op.HostReport{Host: op.HostInfo{Describe: "podman machine m", Running: true}, Memory: op.MemoryReport{TotalKB: 1, AvailableKB: 1},
			Runners:   op.RunnersReport{Repo: "github.com/e/exec", Runners: []op.RunnerState{{Listed: true, Status: "online"}}},
			Providers: []op.ProviderReport{{Provider: "claude", LoggedIn: true}}, Autostart: op.AutostartReport{Kind: "task", Name: "n", Present: true}}
	}
	fx.graph.repo("a/one", "TRIAGE").blockers[3] = []map[string]any{blocker("a/one", 1, "OPEN"), blocker("x/y", 2, "CLOSED")}
	fx.graph.repo("b/two", nil)
	fx.graph.viewer = []map[string]any{nibfinNode("a/one", "TRIAGE", true), nibfinNode("e/exec", "ADMIN", true)}
	check("person read", run(t, ""), "user", "pendingComment", "blockedBy", "CLOSED", "conclusion", "permission", "execution", "slot")

	// Repository view, every part read.
	routes = repoRoutes()
	routes[runsPath("o/r", ">=2026-11-02T00:00:00+00:00", 1)] = map[string]any{"workflow_runs": []any{
		runJSONOf("Nibfin · o/r#1 · wake", "2026-11-05T00:00:00Z", "u1")}}
	fx = setup(t, routes)
	// The gh login writes to o/r but is not admin: GitHub's runner list is
	// not read and the local slots are reported.
	readHost = func(context.Context) op.HostReport {
		return op.HostReport{Runners: op.RunnersReport{Repo: "github.com/o/r", GitHubNeedsAdmin: true, Runners: []op.RunnerState{{}}}}
	}
	fx.graph.repo("o/r", "WRITE").blockers[1] = []map[string]any{blocker("o/r", 2, "OPEN")}
	fx.graph.viewer = []map[string]any{nibfinNode("o/r", "WRITE", true)}
	check("repo read", run(t, "github.com/o/r"), "usesNibfin", "changedAt", "pr", "permission", "blockedBy", "runs", "execution", "githubNeedsAdmin")

	// Person view, every part failed.
	var items []any
	for i := 1; i <= 100; i++ {
		items = append(items, item("a/one", i, "scope"))
	}
	items[0] = item("a/one", 1, "pending")
	routes = map[string]any{
		"user":        &api.HTTPError{StatusCode: 401, Message: "Bad credentials"},
		searchPath(1): map[string]any{"total_count": 300, "items": items},
		searchPath(2): &api.HTTPError{StatusCode: 502, Message: "Bad Gateway"},
		"repos/a/one/contents/.github/workflows/nibfin-job.yml": &api.HTTPError{StatusCode: 403, Message: "Forbidden"},
		"repos/a/one/issues/1/events?per_page=100&page=1":       &api.HTTPError{StatusCode: 500},
	}
	for p := 1; p <= 10; p++ {
		var runs []any
		for i := 0; i < 100; i++ {
			runs = append(runs, runJSONOf("CI", "2026-11-03T00:00:00Z", fmt.Sprintf("u%d-%d", p, i)))
		}
		routes[runsPath("a/one", ">=2026-11-01T00:00:00+00:00", p)] = map[string]any{"workflow_runs": runs}
	}
	fx = setup(t, routes)
	readHost = failedHost
	latestImage = func(context.Context) (string, error) { return "", op.Errorf(op.CodeImageUnverified, "bad") }
	fx.graph.missing["a/one"] = true
	fx.graph.viewerFail = &api.HTTPError{StatusCode: 401, Message: "Bad credentials"}
	res := run(t, "")
	check("person failed", res, "userError", "issuesError", "error", "permissionError", "blockedByError", "runsError",
		"runsTruncated", "pendingCommentError", "nibfinReposError", "githubError")
	if res["user"] != nil || res["repos"].([]RepoIssues)[0].Permission != nil {
		t.Errorf("person failed: %v", res)
	}

	// Repository view, every part failed.
	routes = map[string]any{
		"repos/o/r/contents/.github/workflows/nibfin-job.yml":   &api.HTTPError{StatusCode: 403, Message: "Forbidden"},
		"repos/o/r/contents/.github/workflows/nibfin-issue.yml": &api.HTTPError{StatusCode: 403, Message: "Forbidden"},
	}
	fx = setup(t, routes)
	readHost = failedHost
	fx.graph.fail = fmt.Errorf("connection reset")
	fx.graph.viewerFail = fmt.Errorf("connection reset")
	res = run(t, "github.com/o/r")
	check("repo failed", res, "issuesError", "permissionError", "nibfinReposError", "error")
	if res["nibfinRun"].(NibfinRun).Error == nil || res["image"].(RepoImage).Error == nil || len(res["issues"].([]Issue)) != 0 {
		t.Errorf("repo failed: %v", res)
	}

	// Repository view with the execution repository unknown.
	fx = setup(t, routes)
	readHost = failedHost
	fx.graph.viewer = []map[string]any{nibfinNode("o/r", "READ", true)}
	res = run(t, "github.com/o/r")
	check("repo execution unknown", res, "execution")
	if res["nibfinRepos"].([]NibfinRepo)[0].Execution != nil {
		t.Errorf("execution = %v", res["nibfinRepos"])
	}

	// The schema is strict.
	fx = setup(t, personRoutes(item("a/one", 1)))
	person := run(t, "")
	for name, edit := range map[string]func(m map[string]any){
		"extra key":     func(m map[string]any) { m["extra"] = true },
		"without repos": func(m map[string]any) { delete(m, "repos") },
		"with issues":   func(m map[string]any) { m["issues"] = []any{} },
		"null user":     func(m map[string]any) { m["user"] = nil },
		"issue key": func(m map[string]any) {
			m["repos"].([]any)[0].(map[string]any)["issues"].([]any)[0].(map[string]any)["x"] = 1
		},
	} {
		var m map[string]any
		if err := json.Unmarshal(outputOf(t, person), &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		if err := validate(s, outputOf(t, m)); err == nil {
			t.Errorf("%s: valid against %s", name, SchemaFile)
		}
	}
}

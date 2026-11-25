package status

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// fakeGraph answers the GraphQL documents status sends from an in-memory
// model, the way go-gh does: data is unmarshalled, then errors are returned
// as *api.GraphQLError. A repository it has no model of has WRITE
// permission and Issues without blockers.
type fakeGraph struct {
	t       *testing.T
	repos   map[string]*graphRepo // "owner/name", lower case
	missing map[string]bool       // repositories GitHub reports as NOT_FOUND
	viewer  []map[string]any      // viewer.repositories nodes
	// fail is returned for every blocker query; viewerFail for the
	// repository list.
	fail, viewerFail error
	queries          []string
	hosts            []string
}

type graphRepo struct {
	perm     any // string or nil
	blockers map[int][]map[string]any
	missing  map[int]bool // Issues GitHub reports as NOT_FOUND
}

func newFakeGraph(t *testing.T) *fakeGraph {
	return &fakeGraph{t: t, repos: map[string]*graphRepo{}, missing: map[string]bool{}}
}

func (f *fakeGraph) repo(name string, perm any) *graphRepo {
	r := &graphRepo{perm: perm, blockers: map[int][]map[string]any{}, missing: map[int]bool{}}
	f.repos[strings.ToLower(name)] = r
	return r
}

func blocker(repo string, n int, state string) map[string]any {
	return map[string]any{"number": n, "title": fmt.Sprintf("Blocker %d", n), "state": state,
		"url": fmt.Sprintf("https://github.com/%s/issues/%d", repo, n), "repository": map[string]any{"nameWithOwner": repo}}
}

var (
	repoAliasRE  = regexp.MustCompile(`(r\d+): repository\(owner: \$(\w+), name: \$(\w+)\) \{`)
	issueAliasRE = regexp.MustCompile(`(i\d+): issue\(number: (\d+)\)`)
	issueNumRE   = regexp.MustCompile(`issue\(number: (\d+)\)`)
)

// page returns the connection of list from cursor "c<offset>".
func page(list []map[string]any, cursor any, size int) map[string]any {
	off := 0
	if c, ok := cursor.(string); ok && c != "" {
		off, _ = strconv.Atoi(strings.TrimPrefix(c, "c"))
	}
	end := min(off+size, len(list))
	nodes := list[off:end]
	if nodes == nil {
		nodes = []map[string]any{}
	}
	return map[string]any{"pageInfo": map[string]any{"hasNextPage": end < len(list), "endCursor": fmt.Sprintf("c%d", end)}, "nodes": nodes}
}

func (f *fakeGraph) model(owner, name string) *graphRepo {
	if r := f.repos[strings.ToLower(owner+"/"+name)]; r != nil {
		return r
	}
	return &graphRepo{perm: "WRITE", blockers: map[int][]map[string]any{}, missing: map[int]bool{}}
}

func (f *fakeGraph) DoWithContext(_ context.Context, query string, vars map[string]any, resp any) error {
	f.queries = append(f.queries, query)
	if strings.HasPrefix(query, "mutation") {
		f.t.Errorf("status sent a mutation")
		return fmt.Errorf("write")
	}
	data := map[string]any{}
	var errs []api.GraphQLErrorItem
	switch {
	case strings.Contains(query, "viewer {"):
		if f.viewerFail != nil {
			return f.viewerFail
		}
		data["viewer"] = map[string]any{"repositories": page(f.viewer, vars["after"], 100)}
	case f.fail != nil:
		return f.fail
	case strings.HasPrefix(query, "query($o: String!"):
		r := f.model(vars["o"].(string), vars["n"].(string))
		n, _ := strconv.Atoi(issueNumRE.FindStringSubmatch(query)[1])
		data["repository"] = map[string]any{"issue": map[string]any{"blockedBy": page(r.blockers[n], vars["c"], 100)}}
	default:
		locs := repoAliasRE.FindAllStringSubmatchIndex(query, -1)
		for k, loc := range locs {
			alias := query[loc[2]:loc[3]]
			owner, name := vars[query[loc[4]:loc[5]]].(string), vars[query[loc[6]:loc[7]]].(string)
			end := len(query)
			if k+1 < len(locs) {
				end = locs[k+1][0]
			}
			if f.missing[strings.ToLower(owner+"/"+name)] {
				data[alias] = nil
				errs = append(errs, api.GraphQLErrorItem{Type: "NOT_FOUND", Path: []any{alias}, Message: "Could not resolve to a Repository with the name '" + owner + "/" + name + "'."})
				continue
			}
			r := f.model(owner, name)
			rd := map[string]any{"viewerPermission": r.perm}
			for _, m := range issueAliasRE.FindAllStringSubmatch(query[loc[1]:end], -1) {
				n, _ := strconv.Atoi(m[2])
				if r.missing[n] {
					rd[m[1]] = nil
					errs = append(errs, api.GraphQLErrorItem{Type: "NOT_FOUND", Path: []any{alias, m[1]}, Message: fmt.Sprintf("Could not resolve to an issue with the number of %d.", n)})
					continue
				}
				rd[m[1]] = map[string]any{"blockedBy": page(r.blockers[n], nil, 100)}
			}
			data[alias] = rd
		}
	}
	b, err := json.Marshal(data)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := json.Unmarshal(b, resp); err != nil {
		f.t.Fatal(err)
	}
	if len(errs) > 0 {
		return &api.GraphQLError{Errors: errs}
	}
	return nil
}

func (f *fakeGraph) blockerQueries() int {
	n := 0
	for _, q := range f.queries {
		if !strings.Contains(q, "viewer {") {
			n++
		}
	}
	return n
}

func nibfinNode(repo, perm string, uses bool) map[string]any {
	var ref any = map[string]any{"target": map[string]any{"file": nil}}
	if uses {
		ref = map[string]any{"target": map[string]any{"file": map[string]any{"oid": "abc"}}}
	}
	return map[string]any{"nameWithOwner": repo, "url": "https://github.com/" + repo, "visibility": "PRIVATE", "viewerPermission": perm, "defaultBranchRef": ref}
}

func runsPath(repo, created string, pg int) string {
	return q("repos/"+repo+"/actions/runs", "created", created, "per_page", "100", "page", fmt.Sprint(pg))
}

func runJSONOf(title, created, url string) map[string]any {
	return map[string]any{"name": "Nibfin · Issue", "display_title": title, "status": "completed", "conclusion": "success", "created_at": created, "html_url": url}
}

func runners(repo string) func(context.Context) op.HostReport {
	return func(context.Context) op.HostReport {
		return op.HostReport{Runners: op.RunnersReport{Repo: repo, Runners: []op.RunnerState{}}}
	}
}

func personRoutes(items ...any) map[string]any {
	return map[string]any{
		"user":        map[string]any{"login": "me"},
		searchPath(1): map[string]any{"total_count": len(items), "items": items},
	}
}

func issueByNumber(t *testing.T, repos []RepoIssues, repo string, n int) Issue {
	t.Helper()
	for _, r := range repos {
		if r.Repo != repo {
			continue
		}
		for _, is := range r.Issues {
			if is.Number == n {
				return is
			}
		}
	}
	t.Fatalf("%s#%d not listed", repo, n)
	return Issue{}
}

func TestBlockers(t *testing.T) {
	var items []any
	for i := 1; i <= 60; i++ {
		items = append(items, item("a/one", i))
	}
	items = append(items, item("b/two", 7))
	fx := setup(t, personRoutes(items...))
	one := fx.graph.repo("a/one", "TRIAGE")
	one.blockers[1] = []map[string]any{blocker("a/one", 2, "OPEN"), blocker("x/other", 9, "CLOSED")}
	var many []map[string]any
	for i := 0; i < 130; i++ {
		many = append(many, blocker("x/other", 100+i, "OPEN"))
	}
	one.blockers[55] = many
	one.missing[3] = true
	fx.graph.repo("b/two", nil)

	res := run(t, "")
	repos := res["repos"].([]RepoIssues)
	is1 := issueByNumber(t, repos, "a/one", 1)
	want := []Blocker{
		{Repo: "a/one", Number: 2, Title: "Blocker 2", State: "OPEN", URL: "https://github.com/a/one/issues/2"},
		{Repo: "x/other", Number: 9, Title: "Blocker 9", State: "CLOSED", URL: "https://github.com/x/other/issues/9"},
	}
	if !reflect.DeepEqual(is1.BlockedBy, want) || is1.BlockedByError != nil {
		t.Errorf("#1 = %+v, %v", is1.BlockedBy, is1.BlockedByError)
	}
	if is55 := issueByNumber(t, repos, "a/one", 55); len(is55.BlockedBy) != 130 || is55.BlockedBy[129].Number != 229 || is55.BlockedByError != nil {
		t.Errorf("#55 = %d blockers, %v", len(is55.BlockedBy), is55.BlockedByError)
	}
	is3 := issueByNumber(t, repos, "a/one", 3)
	if is3.BlockedByError == nil || is3.BlockedByError.Code != op.CodeGitHubAPIFailed || !strings.Contains(is3.BlockedByError.Error(), "a/one#3") {
		t.Errorf("#3 = %+v", is3)
	}
	if is4 := issueByNumber(t, repos, "a/one", 4); is4.BlockedByError != nil || is4.BlockedBy == nil || len(is4.BlockedBy) != 0 {
		t.Errorf("#4 = %+v", is4)
	}
	if *repos[0].Permission != "TRIAGE" || repos[0].PermissionError != nil || repos[1].Permission != nil || repos[1].PermissionError != nil {
		t.Errorf("permissions = %v %v / %v %v", repos[0].Permission, repos[0].PermissionError, repos[1].Permission, repos[1].PermissionError)
	}
	// 61 Issues: two batch queries, one follow-up page, no query per Issue.
	if n := fx.graph.blockerQueries(); n != 3 {
		t.Errorf("%d blocker queries:\n%s", n, strings.Join(fx.graph.queries, "\n"))
	}
	if !reflect.DeepEqual(fx.graph.hosts, []string{"github.com"}) {
		t.Errorf("graph hosts = %v", fx.graph.hosts)
	}

	b, _ := json.Marshal(issueByNumber(t, repos, "a/one", 4))
	if !strings.Contains(string(b), `"blockedBy":[]`) || !strings.Contains(string(b), `"runs":[]`) {
		t.Errorf("json = %s", b)
	}
}

func TestBlockersFailures(t *testing.T) {
	// A whole query failing is every Issue's and the repository's error.
	fx := setup(t, personRoutes(item("a/one", 1), item("a/one", 2)))
	fx.graph.fail = &api.HTTPError{StatusCode: 502, Message: "Bad Gateway"}
	repos := run(t, "")["repos"].([]RepoIssues)
	for _, is := range repos[0].Issues {
		if is.BlockedByError == nil || !strings.Contains(is.BlockedByError.Error(), "Bad Gateway") {
			t.Errorf("issue = %+v", is)
		}
	}
	if repos[0].PermissionError == nil || repos[0].Permission != nil {
		t.Errorf("permission = %v %v", repos[0].Permission, repos[0].PermissionError)
	}

	// A repository GitHub cannot resolve.
	fx = setup(t, personRoutes(item("a/one", 1)))
	fx.graph.missing["a/one"] = true
	repos = run(t, "")["repos"].([]RepoIssues)
	if e := repos[0].PermissionError; e == nil || !strings.Contains(e.Error(), "Could not resolve") || repos[0].Issues[0].BlockedByError == nil {
		t.Errorf("repo = %+v", repos[0])
	}
}

func TestPermissionRepoView(t *testing.T) {
	// No open Issues: the permission is still read.
	routes := repoRoutes()
	routes["repos/o/r/issues?state=open&per_page=100&page=1"] = []any{}
	fx := setup(t, routes)
	fx.graph.repo("o/r", "ADMIN")
	res := run(t, "github.com/o/r")
	if p := res["permission"].(*string); p == nil || *p != "ADMIN" || res["permissionError"] != nil {
		t.Errorf("permission = %v %v", res["permission"], res["permissionError"])
	}

	// A failed Issue list: the same.
	routes["repos/o/r/issues?state=open&per_page=100&page=1"] = &api.HTTPError{StatusCode: 500}
	fx = setup(t, routes)
	fx.graph.repo("o/r", "READ")
	if p := run(t, "github.com/o/r")["permission"].(*string); p == nil || *p != "READ" {
		t.Errorf("permission = %v", p)
	}

	// A failed read.
	fx = setup(t, routes)
	fx.graph.fail = fmt.Errorf("connection reset")
	res = run(t, "github.com/o/r")
	if res["permission"].(*string) != nil || res["permissionError"].(*op.Error).Code != op.CodeGitHubAPIFailed {
		t.Errorf("permission = %v %v", res["permission"], res["permissionError"])
	}
}

func TestRuns(t *testing.T) {
	routes := personRoutes(item("a/one", 3), item("a/one", 5), item("b/two", 7))
	// a/one#3 is the oldest of a/one (2026-11-04); b/two#7 is 2026-11-08.
	routes[runsPath("a/one", ">=2026-11-04T00:00:00+00:00", 1)] = map[string]any{"workflow_runs": []any{
		runJSONOf("Nibfin · a/one#3 · wake", "2026-11-05T00:00:00Z", "https://github.com/a/one/actions/runs/1"),
		runJSONOf("Nibfin · A/One#3 · wake", "2026-11-07T00:00:00Z", "https://github.com/a/one/actions/runs/2"),
		runJSONOf("Nibfin · a/one@refs/heads/main · wake", "2026-11-07T00:00:00Z", "https://github.com/a/one/actions/runs/3"),
		runJSONOf("Nibfin · b/two#7 · wake", "2026-11-07T00:00:00Z", "https://github.com/a/one/actions/runs/4"),
		runJSONOf("CI", "2026-11-07T00:00:00Z", "https://github.com/a/one/actions/runs/5"),
	}}
	routes[runsPath("b/two", ">=2026-11-08T00:00:00+00:00", 1)] = map[string]any{"workflow_runs": []any{}}
	// The execution repository: from the oldest of every listed Issue.
	routes[runsPath("e/exec", ">=2026-11-04T00:00:00+00:00", 1)] = map[string]any{"workflow_runs": []any{
		map[string]any{"name": nil, "display_title": "Nibfin · a/one#3 · deliver", "status": "in_progress", "conclusion": nil,
			"created_at": "2026-11-06T00:00:00Z", "html_url": "https://github.com/e/exec/actions/runs/9"},
		runJSONOf("Nibfin · b/two#7 · shape", "2026-11-09T00:00:00Z", "https://github.com/e/exec/actions/runs/8"),
		runJSONOf("Nibfin · e/exec@main · route", "2026-11-09T00:00:00Z", "https://github.com/e/exec/actions/runs/7"),
	}}
	fx := setup(t, routes)
	readHost = runners("GitHub.com/e/exec")
	repos := run(t, "")["repos"].([]RepoIssues)

	is3 := issueByNumber(t, repos, "a/one", 3)
	var urls []string
	for _, r := range is3.Runs {
		urls = append(urls, r.URL)
	}
	if !reflect.DeepEqual(urls, []string{"https://github.com/a/one/actions/runs/2", "https://github.com/e/exec/actions/runs/9", "https://github.com/a/one/actions/runs/1"}) || is3.RunsError != nil {
		t.Errorf("#3 runs = %v, %v", urls, is3.RunsError)
	}
	if r := is3.Runs[1]; r.Repo != "e/exec" || r.Name != "Nibfin · a/one#3 · deliver" || r.Workflow != "" || r.Status != "in_progress" || r.Conclusion != nil {
		t.Errorf("exec run = %+v", r)
	}
	if r := is3.Runs[0]; r.Repo != "a/one" || r.Workflow != "Nibfin · Issue" || *r.Conclusion != "success" || r.CreatedAt != "2026-11-07T00:00:00Z" {
		t.Errorf("issue run = %+v", r)
	}
	// A run in a/one naming b/two#7 is not b/two's.
	if is7 := issueByNumber(t, repos, "b/two", 7); len(is7.Runs) != 1 || is7.Runs[0].URL != "https://github.com/e/exec/actions/runs/8" {
		t.Errorf("#7 runs = %+v", is7.Runs)
	}
	if is5 := issueByNumber(t, repos, "a/one", 5); len(is5.Runs) != 0 || is5.RunsError != nil || is5.RunsTruncated {
		t.Errorf("#5 = %+v", is5)
	}
	n := map[string]int{}
	for _, g := range fx.gh.gets {
		if strings.Contains(g, "/actions/runs?") {
			n[strings.Split(g, "/actions")[0]]++
		}
	}
	if !reflect.DeepEqual(n, map[string]int{"repos/a/one": 1, "repos/b/two": 1, "repos/e/exec": 1}) {
		t.Errorf("run lists = %v", n)
	}
}

func TestRunsCases(t *testing.T) {
	created := ">=2026-11-02T00:00:00+00:00" // item n=1

	// The execution repository is the Issue's: scanned once.
	routes := repoRoutes()
	routes["repos/o/r/issues?state=open&per_page=100&page=1"] = []any{item("o/r", 1)}
	routes[runsPath("o/r", created, 1)] = map[string]any{"workflow_runs": []any{runJSONOf("Nibfin · o/r#1 · wake", "2026-11-03T00:00:00Z", "u1")}}
	fx := setup(t, routes)
	readHost = runners("github.com/O/R")
	is := run(t, "github.com/o/r")["issues"].([]Issue)
	if len(is[0].Runs) != 1 || is[0].RunsError != nil {
		t.Errorf("issue = %+v", is[0])
	}
	if n := countPrefix(fx.gh.gets, "repos/o/r/actions/runs?created="); n != 1 {
		t.Errorf("%d scans", n)
	}

	// The execution repository on another host names no listed Issue: not
	// scanned, no error.
	fx = setup(t, routes)
	readHost = runners("ghe.example/e/exec")
	is = run(t, "github.com/o/r")["issues"].([]Issue)
	if is[0].RunsError != nil || len(is[0].Runs) != 1 || countPrefix(fx.gh.gets, "repos/e/exec") != 0 || len(fx.hosts) != 1 {
		t.Errorf("issue = %+v, gets %v", is[0], fx.gh.gets)
	}

	// One repository fails: its error, and the other's runs remain.
	fx = setup(t, routes)
	readHost = runners("github.com/e/exec")
	routes[runsPath("e/exec", created, 1)] = &api.HTTPError{StatusCode: 403, Message: "Resource not accessible"}
	is = run(t, "github.com/o/r")["issues"].([]Issue)
	if e := is[0].RunsError; e == nil || e.Code != op.CodeGitHubAPIFailed || !strings.Contains(e.Error(), "list the runs of e/exec: HTTP 403") || len(is[0].Runs) != 1 {
		t.Errorf("issue = %+v", is[0])
	}

	// The runner host cannot be read: the execution repository is unknown.
	fx = setup(t, routes)
	readHost = func(context.Context) op.HostReport {
		return op.HostReport{Runners: op.RunnersReport{Error: &op.Error{Code: op.CodeMachineStopped}}}
	}
	res := run(t, "github.com/o/r")
	is = res["issues"].([]Issue)
	if e := is[0].RunsError; e == nil || e.Error() != "execution repository unknown: machine_stopped" || len(is[0].Runs) != 1 {
		t.Errorf("issue = %+v", is[0])
	}

	// No runners: only the Issue's repository, no error.
	fx = setup(t, routes)
	is = run(t, "github.com/o/r")["issues"].([]Issue)
	if is[0].RunsError != nil || len(is[0].Runs) != 1 {
		t.Errorf("issue = %+v", is[0])
	}

	// An unknown creation time.
	bad := item("o/r", 1)
	bad["created_at"] = "yesterday"
	routes["repos/o/r/issues?state=open&per_page=100&page=1"] = []any{bad, item("o/r", 2)}
	fx = setup(t, routes)
	is = run(t, "github.com/o/r")["issues"].([]Issue)
	if e := is[0].RunsError; e == nil || !strings.Contains(e.Error(), "creation time of o/r#1 unknown") {
		t.Errorf("issue = %+v", is[0])
	}
	if is[1].RunsError == nil || !strings.Contains(is[1].RunsError.Error(), "list the runs of o/r") {
		// #2 (2026-11-03) is scanned from its own time; no route: 404.
		t.Errorf("issue 2 = %+v", is[1])
	}
	if countPrefix(fx.gh.gets, runsPath("o/r", ">=2026-11-03T00:00:00+00:00", 1)) != 1 {
		t.Errorf("gets = %v", fx.gh.gets)
	}
}

// countPrefix counts the reads starting with prefix, without nibfinRun's
// own list of successful runs.
func countPrefix(gets []string, prefix string) int {
	n := 0
	for _, g := range gets {
		if strings.HasPrefix(g, prefix) && !strings.Contains(g, "status=success") {
			n++
		}
	}
	return n
}

func TestRunsTruncated(t *testing.T) {
	routes := repoRoutes()
	routes["repos/o/r/issues?state=open&per_page=100&page=1"] = []any{item("o/r", 1)}
	created := ">=2026-11-02T00:00:00+00:00"
	for p := 1; p <= 11; p++ {
		var runs []any
		for i := 0; i < 100; i++ {
			runs = append(runs, runJSONOf("CI", "2026-11-03T00:00:00Z", fmt.Sprintf("u%d-%d", p, i)))
		}
		if p == 1 {
			runs[0] = runJSONOf("Nibfin · o/r#1 · wake", "2026-11-03T00:00:00Z", "mine")
		}
		routes[runsPath("o/r", created, p)] = map[string]any{"workflow_runs": runs}
	}
	fx := setup(t, routes)
	is := run(t, "github.com/o/r")["issues"].([]Issue)
	if !is[0].RunsTruncated || len(is[0].Runs) != 1 || is[0].RunsError != nil {
		t.Errorf("issue = %+v", is[0])
	}
	if n := countPrefix(fx.gh.gets, "repos/o/r/actions/runs?created="); n != 10 {
		t.Errorf("%d pages", n)
	}
}

func TestIssueOfTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Nibfin · o/r#12 · wake":           "o/r#12",
		"NIBFIN · O/R#12 · deliver":        "o/r#12",
		"Nibfin · o/r@refs/heads/x · wake": "",
		"Nibfin · o/r@x#5 · route":         "",
		"Nibfin · o/r#05 · wake":           "",
		"Nibfin · o/r#12":                  "",
		"Nibfin · r#12 · wake":             "",
		"CI · o/r#12 · wake":               "",
	} {
		if got := issueOfTitle(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}

func TestNibfinRepos(t *testing.T) {
	fx := setup(t, personRoutes())
	readHost = runners("github.com/E/Exec")
	for i := 0; i < 120; i++ {
		fx.graph.viewer = append(fx.graph.viewer, nibfinNode(fmt.Sprintf("o/plain%d", i), "WRITE", false))
	}
	fx.graph.viewer = append(fx.graph.viewer, nibfinNode("z/last", "READ", true), nibfinNode("e/exec", "ADMIN", true),
		map[string]any{"nameWithOwner": "o/empty", "url": "u", "visibility": "PUBLIC", "viewerPermission": "ADMIN", "defaultBranchRef": nil})
	fx.graph.viewer[3] = nibfinNode("A/first", "MAINTAIN", true)
	res := run(t, "")
	got := res["nibfinRepos"].([]NibfinRepo)
	var names []string
	for _, r := range got {
		names = append(names, r.Repo)
	}
	if !reflect.DeepEqual(names, []string{"A/first", "e/exec", "z/last"}) || res["nibfinReposError"] != nil {
		t.Fatalf("repos = %v, %v", names, res["nibfinReposError"])
	}
	if !*got[1].Execution || *got[0].Execution || *got[0].Permission != "MAINTAIN" || got[0].Visibility != "PRIVATE" || got[0].URL != "https://github.com/A/first" {
		t.Errorf("repos = %+v", got)
	}
	viewerQueries := 0
	for _, q := range fx.graph.queries {
		if strings.Contains(q, "viewer {") {
			viewerQueries++
			if !strings.Contains(q, `file(path: ".github/workflows/nibfin-issue.yml")`) || !strings.Contains(q, "ORGANIZATION_MEMBER") {
				t.Errorf("query = %s", q)
			}
		}
	}
	if viewerQueries != 2 {
		t.Errorf("%d pages", viewerQueries)
	}

	// The runner host unreadable: execution unknown.
	fx = setup(t, personRoutes())
	fx.graph.viewer = []map[string]any{nibfinNode("e/exec", "ADMIN", true)}
	readHost = func(context.Context) op.HostReport {
		return op.HostReport{Runners: op.RunnersReport{Error: &op.Error{Code: op.CodePodmanMissing}}}
	}
	if got := run(t, "")["nibfinRepos"].([]NibfinRepo); len(got) != 1 || got[0].Execution != nil {
		t.Errorf("repos = %+v", got)
	}

	// The query fails: the error, and no partial list.
	fx = setup(t, personRoutes())
	fx.graph.viewerFail = &api.HTTPError{StatusCode: 401, Message: "Bad credentials"}
	res = run(t, "")
	if got := res["nibfinRepos"].([]NibfinRepo); len(got) != 0 || got == nil {
		t.Errorf("repos = %v", got)
	}
	if e := res["nibfinReposError"].(*op.Error); e.Code != op.CodeGitHubAPIFailed || !strings.Contains(e.Error(), "Bad credentials") {
		t.Errorf("error = %v", e)
	}
}

package op

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
)

// fakeGitHub answers the three documents the label operations send from an
// in-memory model, the way go-gh does: data is unmarshalled, then errors
// are returned as *api.GraphQLError.
type fakeGitHub struct {
	repos     map[string]*fakeRepo // "owner/name", lower case
	calls     []string             // "query", "mutation", "readback"
	failIDs   map[string]bool      // mutation fields that fail, by issue ID
	afterMut  func()               // runs after a mutation, before read-back
	transport error                // returned for every call when set
	hosts     []string             // hosts newGraphQL was asked for
}

type fakeRepo struct {
	labels map[string]string // name -> ID
	issues map[int]*fakeIssue
}

type fakeIssue struct {
	id, url string
	labels  []string
	pr      bool // a pull request: repository.issue reports NOT_FOUND
}

func newFake(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{repos: map[string]*fakeRepo{}, failIDs: map[string]bool{}}
	oldNew, oldHosts := newGraphQL, knownHosts
	newGraphQL = func(host string) (graphQL, error) { f.hosts = append(f.hosts, host); return f, nil }
	knownHosts = func() []string { return []string{"github.com", "ghe.example.com"} }
	t.Cleanup(func() { newGraphQL, knownHosts = oldNew, oldHosts })
	return f
}

func (f *fakeGitHub) repo(name string, labels ...string) *fakeRepo {
	r := &fakeRepo{labels: map[string]string{}, issues: map[int]*fakeIssue{}}
	for _, l := range labels {
		r.labels[l] = "L_" + name + "_" + l
	}
	f.repos[strings.ToLower(name)] = r
	return r
}

func (r *fakeRepo) issue(repo string, n int, labels ...string) *fakeIssue {
	i := &fakeIssue{id: "I_" + repo + "#" + strconv.Itoa(n), url: "https://github.com/" + repo + "/issues/" + strconv.Itoa(n), labels: labels}
	r.issues[n] = i
	return i
}

func (f *fakeGitHub) issueByID(id string) (*fakeRepo, *fakeIssue) {
	for _, r := range f.repos {
		for _, i := range r.issues {
			if i.id == id {
				return r, i
			}
		}
	}
	return nil, nil
}

func (f *fakeGitHub) labelNodes(r *fakeRepo, i *fakeIssue) []map[string]string {
	out := []map[string]string{}
	for _, l := range i.labels {
		out = append(out, map[string]string{"id": r.labels[l], "name": l})
	}
	return out
}

var (
	repoField  = regexp.MustCompile(`(r\d+): repository\(owner: \$(\w+), name: \$(\w+)\) \{`)
	labelField = regexp.MustCompile(`label: label\(name: "(\w+)"\)`)
	issueField = regexp.MustCompile(`(i\d+): issue\(number: (\d+)\)`)
	mutField   = regexp.MustCompile(`(m\d+): (\w+)\(input: \{labelableId: \$(\w+), labelIds: \$(\w+)\}\)`)
)

func (f *fakeGitHub) DoWithContext(_ context.Context, query string, vars map[string]any, response any) error {
	if f.transport != nil {
		return f.transport
	}
	data := map[string]any{}
	var errs []api.GraphQLErrorItem
	switch {
	case strings.HasPrefix(query, "mutation"):
		f.calls = append(f.calls, "mutation")
		for _, m := range mutField.FindAllStringSubmatch(query, -1) {
			id := vars[m[3]].(string)
			r, i := f.issueByID(id)
			if f.failIDs[id] || i == nil {
				data[m[1]] = nil
				errs = append(errs, api.GraphQLErrorItem{Message: "boom", Path: []any{m[1]}})
				continue
			}
			for _, lid := range vars[m[4]].([]string) {
				name := ""
				for n, v := range r.labels {
					if v == lid {
						name = n
					}
				}
				if m[2] == "addLabelsToLabelable" {
					if !contains(i.labels, name) {
						i.labels = append(i.labels, name)
					}
				} else {
					i.labels = remove(i.labels, name)
				}
			}
			data[m[1]] = map[string]any{"clientMutationId": nil}
		}
		if f.afterMut != nil {
			f.afterMut()
		}
	case strings.Contains(query, "nodes(ids:"):
		f.calls = append(f.calls, "readback")
		nodes := []any{}
		for _, id := range vars["ids"].([]string) {
			r, i := f.issueByID(id)
			if i == nil {
				nodes = append(nodes, nil)
				continue
			}
			nodes = append(nodes, map[string]any{"id": i.id, "url": i.url, "labels": map[string]any{"nodes": f.labelNodes(r, i)}})
		}
		data["nodes"] = nodes
	default:
		f.calls = append(f.calls, "query")
		locs := repoField.FindAllStringSubmatchIndex(query, -1)
		for k, loc := range locs {
			alias := query[loc[2]:loc[3]]
			owner := vars[query[loc[4]:loc[5]]].(string)
			name := vars[query[loc[6]:loc[7]]].(string)
			end := len(query)
			if k+1 < len(locs) {
				end = locs[k+1][0]
			}
			body := query[loc[1]:end]
			r := f.repos[strings.ToLower(owner+"/"+name)]
			if r == nil {
				data[alias] = nil
				errs = append(errs, api.GraphQLErrorItem{Type: "NOT_FOUND", Message: "Could not resolve to a Repository", Path: []any{alias}})
				continue
			}
			rd := map[string]any{}
			if m := labelField.FindStringSubmatch(body); m != nil {
				if id, ok := r.labels[m[1]]; ok {
					rd["label"] = map[string]any{"id": id, "name": m[1]}
				} else {
					rd["label"] = nil
				}
			}
			for _, m := range issueField.FindAllStringSubmatch(body, -1) {
				n, _ := strconv.Atoi(m[2])
				i := r.issues[n]
				switch {
				case i == nil:
					rd[m[1]] = nil // missing: null without an error
				case i.pr:
					rd[m[1]] = nil
					errs = append(errs, api.GraphQLErrorItem{Type: "NOT_FOUND", Message: "Could not resolve to an Issue", Path: []any{alias, m[1]}})
				default:
					rd[m[1]] = map[string]any{"id": i.id, "url": i.url, "labels": map[string]any{"nodes": f.labelNodes(r, i)}}
				}
			}
			data[alias] = rd
		}
	}
	b, _ := json.Marshal(data)
	if err := json.Unmarshal(b, response); err != nil {
		return err
	}
	if len(errs) > 0 {
		return &api.GraphQLError{Errors: errs}
	}
	return nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func remove(s []string, v string) []string {
	out := []string{}
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func noConfirm() *fakeConfirmer { return &fakeConfirmer{err: fmt.Errorf("asked")} }

func TestParseIssueRefs(t *testing.T) {
	newFake(t)
	refs, err := parseIssueRefs("github.com/o/r", []string{
		"12", "#13", "https://github.com/O/R/issues/14/", "HTTPS://GitHub.com/a/b/issues/15#issuecomment-1",
		"https://github.com/a/b/issues/16?x=1", "#12", "https://github.com/o/r/issues/13",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, fmt.Sprintf("%s/%s/%s#%d", r.Host, r.Owner, r.Name, r.Number))
	}
	want := []string{"github.com/o/r#12", "github.com/o/r#13", "github.com/O/R#14", "github.com/a/b#15", "github.com/a/b#16"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}

	for _, tt := range []struct {
		repo string
		refs []string
	}{
		{"github.com/o/r", nil},
		{"github.com/o/r", []string{"0"}},
		{"github.com/o/r", []string{"-1"}},
		{"github.com/o/r", []string{"2147483648"}},
		{"github.com/o/r", []string{"99999999999999999999"}},
		{"github.com/o/r", []string{"abc"}},
		{"github.com/o/r", []string{"https://github.com/o/r/pull/1"}},
		{"github.com/o/r", []string{"https://github.com/o/r/issues/0"}},
		{"github.com/o/r", []string{"https://github.com/o/r/issues/1/x"}},
		{"github.com/o/r", []string{"https://github.com/o/r/issues/+1"}},
		{"github.com/o/r", []string{"https://github.com:8443/o/r/issues/1"}},
		{"github.com/o/r", []string{"https://u:p@github.com/o/r/issues/1"}},
		{"github.com/o/r", []string{"ftp://github.com/o/r/issues/1"}},
		{"", []string{"1"}},
		{"o/r", []string{"1"}},
		{"github.com/o/r", []string{"1", "https://ghe.example.com/o/r/issues/2"}},
		{"evil.example/o/r", []string{"1"}},
		{"github.com/o/r", []string{"https://evil.example/o/r/issues/1"}},
	} {
		if _, err := parseIssueRefs(tt.repo, tt.refs); Code(err) != CodeInvalidArgument {
			t.Errorf("%q %q: code %q, err %v", tt.repo, tt.refs, Code(err), err)
		}
	}
	if _, err := parseIssueRefs("", []string{"https://ghe.example.com/o/r/issues/2"}); err != nil {
		t.Errorf("known enterprise host: %v", err)
	}

	if !HasIssueNumber([]string{"https://github.com/o/r/issues/1", "#2"}) || HasIssueNumber([]string{"https://github.com/o/r/issues/1"}) {
		t.Error("HasIssueNumber")
	}
}

func TestPause(t *testing.T) {
	f := newFake(t)
	r := f.repo("o/r", "pause", "pending", "bug")
	r.issue("o/r", 1, "bug")
	r.issue("o/r", 2, "pause")
	b := f.repo("a/b", "pause")
	b.issue("a/b", 3)

	p, res, err := Run(context.Background(), Pause(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1", "#2", "https://github.com/a/b/issues/3"}}), noConfirm())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"query", "mutation", "readback"}) {
		t.Errorf("calls = %v", f.calls)
	}
	wantSteps := []Step{
		{ID: "issue:https://github.com/o/r/issues/1", Action: "add label", Target: "https://github.com/o/r/issues/1", Detail: "pause", Current: "no pause", Next: "pause"},
		{ID: "issue:https://github.com/o/r/issues/2", Action: "add label", Target: "https://github.com/o/r/issues/2", Detail: "already paused", Current: "pause", Next: "pause", NoChange: true},
		{ID: "issue:https://github.com/a/b/issues/3", Action: "add label", Target: "https://github.com/a/b/issues/3", Detail: "pause", Current: "no pause", Next: "pause"},
	}
	if p.Confirm || p.Operation != "pause" || !reflect.DeepEqual(p.Steps, wantSteps) {
		t.Errorf("plan = %+v", p)
	}
	want := Result{"https://github.com/o/r/issues/1": "paused", "https://github.com/o/r/issues/2": "paused", "https://github.com/a/b/issues/3": "paused"}
	if items := takeItems(t, res); len(items) != 3 || !items[0].Confirmed || !items[1].Confirmed || !items[2].Confirmed || items[1].State != "pause" {
		t.Errorf("items = %+v", items)
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("result = %v", res)
	}
	if got := r.issues[1].labels; !reflect.DeepEqual(got, []string{"bug", "pause"}) {
		t.Errorf("issue 1 labels = %v", got)
	}
	if !reflect.DeepEqual(f.hosts, []string{"github.com"}) {
		t.Errorf("hosts = %v", f.hosts)
	}
}

func TestResume(t *testing.T) {
	f := newFake(t)
	r := f.repo("o/r", "pause", "pending", "bug")
	r.issue("o/r", 1, "bug", "pause", "pending")
	r.issue("o/r", 2, "pending")
	r.issue("o/r", 3, "bug")

	p, res, err := Run(context.Background(), Resume(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1", "2", "3"}}), noConfirm())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"query", "mutation", "readback"}) {
		t.Errorf("calls = %v", f.calls)
	}
	wantSteps := []Step{
		{ID: "issue:https://github.com/o/r/issues/1", Action: "remove label", Target: "https://github.com/o/r/issues/1", Detail: "pause, pending", Current: "pause, pending", Next: "no pause or pending"},
		{ID: "issue:https://github.com/o/r/issues/2", Action: "remove label", Target: "https://github.com/o/r/issues/2", Detail: "pending", Current: "pending", Next: "no pause or pending"},
		{ID: "issue:https://github.com/o/r/issues/3", Action: "remove label", Target: "https://github.com/o/r/issues/3", Detail: "already resumed", Current: "no pause or pending", Next: "no pause or pending", NoChange: true},
	}
	if p.Confirm || !reflect.DeepEqual(p.Steps, wantSteps) {
		t.Errorf("plan = %+v", p)
	}
	if len(takeItems(t, res)) != 3 || len(res) != 3 || res["https://github.com/o/r/issues/3"] != "resumed" {
		t.Errorf("result = %v", res)
	}
	for n, want := range map[int][]string{1: {"bug"}, 2: {}, 3: {"bug"}} {
		if got := r.issues[n].labels; !reflect.DeepEqual(got, want) {
			t.Errorf("issue %d labels = %v, want %v", n, got, want)
		}
	}
}

func TestResumeWithoutLabelsInRepo(t *testing.T) {
	f := newFake(t)
	f.repo("o/r", "bug").issue("o/r", 1, "bug")
	_, res, err := Run(context.Background(), Resume(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1"}}), noConfirm())
	if err != nil || res["https://github.com/o/r/issues/1"] != "resumed" {
		t.Fatalf("res %v, err %v", res, err)
	}
	if !reflect.DeepEqual(f.calls, []string{"query", "readback"}) {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestNoChangeSendsNoMutation(t *testing.T) {
	f := newFake(t)
	f.repo("o/r", "pause").issue("o/r", 1, "pause")
	p, res, err := Run(context.Background(), Pause(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1"}}), noConfirm())
	if err != nil || len(p.Changes()) != 0 || len(p.Steps) != 1 || res["https://github.com/o/r/issues/1"] != "paused" {
		t.Fatalf("plan %+v, res %v, err %v", p, res, err)
	}
	if !reflect.DeepEqual(f.calls, []string{"query", "readback"}) {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestPlanFailuresWriteNothing(t *testing.T) {
	setup := func(t *testing.T) *fakeGitHub {
		f := newFake(t)
		r := f.repo("o/r", "pause")
		r.issue("o/r", 1)
		r.issue("o/r", 2).pr = true
		f.repo("n/l").issue("n/l", 1)
		return f
	}
	for _, tt := range []struct {
		name   string
		op     func(IssuesParams) Operation
		issues []string
		code   string
		msg    []string
	}{
		{"missing issue", Pause, []string{"1", "9"}, CodeNotFound, []string{`9`}},
		{"pull request", Resume, []string{"#2", "1"}, CodeNotFound, []string{"#2"}},
		{"missing repo", Pause, []string{"https://github.com/x/y/issues/1", "9"}, CodeNotFound, []string{"x/y/issues/1", "9"}},
		{"no pause label", Pause, []string{"https://github.com/n/l/issues/1"}, CodeLabelMissing, []string{"pause"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			_, _, err := Run(context.Background(), tt.op(IssuesParams{Repo: "github.com/o/r", Issues: tt.issues}), noConfirm())
			if Code(err) != tt.code {
				t.Fatalf("code %q, err %v", Code(err), err)
			}
			for _, m := range tt.msg {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("message %q lacks %q", err, m)
				}
			}
			if !reflect.DeepEqual(f.calls, []string{"query"}) {
				t.Errorf("calls = %v", f.calls)
			}
		})
	}
}

func TestPartialMutationFailure(t *testing.T) {
	f := newFake(t)
	r := f.repo("o/r", "pause")
	r.issue("o/r", 1)
	bad := r.issue("o/r", 2)
	f.failIDs[bad.id] = true
	o := Pause(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1", "2"}})
	_, _, err := Run(context.Background(), o, noConfirm())
	if Code(err) != CodeGitHubError || !strings.Contains(err.Error(), bad.url) || strings.Contains(err.Error(), "issues/1,") {
		t.Fatalf("code %q, err %v", Code(err), err)
	}
	if !reflect.DeepEqual(f.calls, []string{"query", "mutation"}) {
		t.Errorf("calls = %v", f.calls)
	}

	// A redo plans from the live labels and changes only what is left.
	delete(f.failIDs, bad.id)
	f.calls = nil
	p, _, err := Run(context.Background(), Pause(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1", "2"}}), noConfirm())
	if err != nil || len(p.Changes()) != 1 || p.Changes()[0].Target != bad.url {
		t.Fatalf("redo plan %+v, err %v", p, err)
	}
}

func TestReadBackMismatch(t *testing.T) {
	f := newFake(t)
	r := f.repo("o/r", "pause", "pending")
	i := r.issue("o/r", 1, "pause")
	f.afterMut = func() { i.labels = append(i.labels, "pending") } // Core re-adds pending
	_, _, err := Run(context.Background(), Resume(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1"}}), noConfirm())
	if Code(err) != CodeNotApplied || !strings.Contains(err.Error(), "[pending]") {
		t.Fatalf("code %q, err %v", Code(err), err)
	}
}

func TestTransportAndAuthErrors(t *testing.T) {
	f := newFake(t)
	f.transport = &api.HTTPError{StatusCode: 502, Message: "bad gateway"}
	_, _, err := Run(context.Background(), Pause(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1"}}), noConfirm())
	if Code(err) != CodeGitHubError || !strings.Contains(err.Error(), "bad gateway") {
		t.Errorf("http error: code %q, err %v", Code(err), err)
	}

	f.transport = &api.GraphQLError{Errors: []api.GraphQLErrorItem{{Type: "FORBIDDEN", Message: "no", Path: []any{"r0"}}}}
	_, _, err = Run(context.Background(), Pause(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1"}}), noConfirm())
	if Code(err) != CodeGitHubError {
		t.Errorf("graphql error: code %q, err %v", Code(err), err)
	}

	newGraphQL = func(string) (graphQL, error) {
		return nil, fmt.Errorf("authentication token not found for host github.com")
	}
	_, _, err = Run(context.Background(), Resume(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1"}}), noConfirm())
	if Code(err) != CodeAuthUnavailable || !strings.Contains(err.Error(), "token not found") {
		t.Errorf("auth: code %q, err %v", Code(err), err)
	}
	if Code(Wrap(CodeGitHubError, fmt.Errorf("ctx: %w", err))) != CodeAuthUnavailable {
		t.Error("wrapping lost the cause code")
	}
}

func TestApplyBeforePlan(t *testing.T) {
	f := newFake(t)
	o := Pause(IssuesParams{Repo: "github.com/o/r", Issues: []string{"1"}})
	if err := o.Apply(context.Background(), Plan{Steps: []Step{{Action: "add label"}}}); err == nil {
		t.Error("Apply before Plan succeeded")
	}
	if _, err := o.ReadBack(context.Background(), Plan{}); err == nil {
		t.Error("ReadBack before Plan succeeded")
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %v", f.calls)
	}
}

// takeItems removes the shared keys from r and returns its items.
func takeItems(t *testing.T, r Result) []Check {
	t.Helper()
	items, ok := r[ResultItems].([]Check)
	if _, notes := r[ResultNotes].([]string); !ok || !notes {
		t.Fatalf("result without items or notes: %v", r)
	}
	delete(r, ResultItems)
	delete(r, ResultNotes)
	return items
}

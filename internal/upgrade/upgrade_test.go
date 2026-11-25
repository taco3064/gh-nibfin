package upgrade

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
)

const (
	repo     = "github.com/o/r"
	imageRef = "ghcr.io/taco3064/nibfin@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	oldImage = "ghcr.io/taco3064/nibfin@sha256:" + "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	issueP   = ".github/workflows/nibfin-issue.yml"
	sharedP  = ".github/workflows/nibfin-shared-runner.yml"
	jobP     = ".github/workflows/nibfin-job.yml"
)

type fakePR struct {
	number int
	head   string
	base   string
	state  string
}

// fakeGitHub is an in-memory repository behind the REST paths upgrade uses.
type fakeGitHub struct {
	defaultBranch string
	commits       map[string]map[string]string // commit SHA -> files
	refs          map[string]string            // branch -> commit SHA
	prs           []*fakePR
	// fail makes the first call with this "METHOD path" key fail, after the
	// write took effect when failAfter is set (a crash after the write).
	fail      map[string]error
	failAfter bool
	calls     []string
	n         int
}

func newFake(files map[string]string) *fakeGitHub {
	return &fakeGitHub{
		defaultBranch: "main",
		commits:       map[string]map[string]string{"c0": files},
		refs:          map[string]string{"main": "c0"},
	}
}

func (f *fakeGitHub) writes() []string {
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "GET ") {
			out = append(out, c)
		}
	}
	return out
}

func notFound() error { return &api.HTTPError{StatusCode: 404, Message: "Not Found"} }

func (f *fakeGitHub) DoWithContext(_ context.Context, method, p string, body io.Reader, response any) (err error) {
	u, perr := url.Parse(p)
	if perr != nil {
		return perr
	}
	key := method + " " + u.Path
	f.calls = append(f.calls, key)
	failure := f.fail[key]
	if failure != nil {
		delete(f.fail, key)
		if !f.failAfter {
			return failure
		}
		defer func() {
			if err == nil {
				err = failure
			}
		}()
	}
	var in map[string]any
	if body != nil {
		if err := json.NewDecoder(body).Decode(&in); err != nil {
			return err
		}
	}
	out, err := f.serve(method, u, in)
	if err != nil {
		return err
	}
	if response == nil {
		return nil
	}
	b, _ := json.Marshal(out)
	return json.Unmarshal(b, response)
}

func (f *fakeGitHub) next(prefix string) string {
	f.n++
	return fmt.Sprintf("%s%d", prefix, f.n)
}

func (f *fakeGitHub) serve(method string, u *url.URL, in map[string]any) (any, error) {
	const base = "repos/o/r"
	p := u.Path
	switch {
	case method == http.MethodGet && p == base:
		return map[string]string{"default_branch": f.defaultBranch}, nil
	case method == http.MethodGet && strings.HasPrefix(p, base+"/contents/"):
		ref := u.Query().Get("ref")
		if ref == "" {
			ref = f.defaultBranch
		}
		sha, ok := f.refs[ref]
		if !ok {
			sha = ref
		}
		files, ok := f.commits[sha]
		if !ok {
			return nil, notFound()
		}
		content, ok := files[strings.TrimPrefix(p, base+"/contents/")]
		if !ok {
			return nil, notFound()
		}
		return map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))}, nil
	case method == http.MethodGet && strings.HasPrefix(p, base+"/git/ref/heads/"):
		sha, ok := f.refs[strings.TrimPrefix(p, base+"/git/ref/heads/")]
		if !ok {
			return nil, notFound()
		}
		return map[string]any{"object": map[string]string{"sha": sha}}, nil
	case method == http.MethodGet && strings.HasPrefix(p, base+"/git/commits/"):
		sha := strings.TrimPrefix(p, base+"/git/commits/")
		if _, ok := f.commits[sha]; !ok {
			return nil, notFound()
		}
		return map[string]any{"sha": sha, "tree": map[string]string{"sha": "tree-" + sha}}, nil
	case method == http.MethodPost && p == base+"/git/trees":
		baseTree, _ := in["base_tree"].(string)
		files := map[string]string{}
		for k, v := range f.commits[strings.TrimPrefix(baseTree, "tree-")] {
			files[k] = v
		}
		for _, e := range in["tree"].([]any) {
			e := e.(map[string]any)
			if e["mode"] != "100644" || e["type"] != "blob" {
				return nil, fmt.Errorf("bad tree entry %v", e)
			}
			files[e["path"].(string)] = e["content"].(string)
		}
		sha := f.next("t")
		f.commits["pending-"+sha] = files
		return map[string]string{"sha": sha}, nil
	case method == http.MethodPost && p == base+"/git/commits":
		files, ok := f.commits["pending-"+in["tree"].(string)]
		if !ok {
			return nil, &api.HTTPError{StatusCode: 422, Message: "bad tree"}
		}
		parents := in["parents"].([]any)
		if len(parents) != 1 || parents[0] != f.refs[f.defaultBranch] {
			return nil, fmt.Errorf("parents %v", parents)
		}
		if !strings.HasPrefix(in["message"].(string), "Upgrade Nibfin workflows to ") {
			return nil, fmt.Errorf("message %v", in["message"])
		}
		sha := f.next("c")
		f.commits[sha] = files
		return map[string]string{"sha": sha}, nil
	case method == http.MethodPost && p == base+"/git/refs":
		name := strings.TrimPrefix(in["ref"].(string), "refs/heads/")
		if _, ok := f.refs[name]; ok {
			return nil, &api.HTTPError{StatusCode: 422, Message: "Reference already exists"}
		}
		f.refs[name] = in["sha"].(string)
		return map[string]any{}, nil
	case method == http.MethodPatch && strings.HasPrefix(p, base+"/git/refs/heads/"):
		name := strings.TrimPrefix(p, base+"/git/refs/heads/")
		if _, ok := f.refs[name]; !ok {
			return nil, &api.HTTPError{StatusCode: 422, Message: "Reference does not exist"}
		}
		if in["force"] != true {
			return nil, errors.New("not forced")
		}
		f.refs[name] = in["sha"].(string)
		return map[string]any{}, nil
	case method == http.MethodGet && p == base+"/pulls":
		q := u.Query()
		if q.Get("state") != "open" || q.Get("head") != "o:"+Branch {
			return nil, fmt.Errorf("query %v", q)
		}
		out := []any{}
		for _, pr := range f.prs {
			if pr.state == "open" && pr.head == Branch {
				out = append(out, f.prJSON(pr))
			}
		}
		return out, nil
	case method == http.MethodGet && strings.HasPrefix(p, base+"/pulls/"):
		for _, pr := range f.prs {
			if p == fmt.Sprintf("%s/pulls/%d", base, pr.number) {
				return f.prJSON(pr), nil
			}
		}
		return nil, notFound()
	case method == http.MethodPost && p == base+"/pulls":
		head := in["head"].(string)
		if _, ok := f.refs[head]; !ok {
			return nil, &api.HTTPError{StatusCode: 422, Message: "no head"}
		}
		for _, pr := range f.prs {
			if pr.state == "open" && pr.head == head {
				return nil, &api.HTTPError{StatusCode: 422, Message: "A pull request already exists"}
			}
		}
		if in["title"] != "Upgrade Nibfin workflows" || !strings.Contains(in["body"].(string), "gh nibfin upgrade") {
			return nil, fmt.Errorf("PR %v", in)
		}
		pr := &fakePR{number: 10 + len(f.prs), head: head, base: in["base"].(string), state: "open"}
		f.prs = append(f.prs, pr)
		return f.prJSON(pr), nil
	}
	return nil, fmt.Errorf("unexpected %s %s", method, p)
}

func (f *fakeGitHub) prJSON(pr *fakePR) map[string]any {
	return map[string]any{
		"number":   pr.number,
		"html_url": fmt.Sprintf("https://github.com/o/r/pull/%d", pr.number),
		"state":    pr.state,
		"head":     map[string]string{"ref": pr.head, "sha": f.refs[pr.head]},
	}
}

var ciNames = []string{"CI"}

// stub replaces the client, the image resolver and the CI workflow reader.
func stub(t *testing.T, f *fakeGitHub) *string {
	t.Helper()
	img := imageRef
	oldClient, oldLatest, oldCI := newClient, latestImage, ciWorkflows
	newClient = func(host string) (templates.RESTClient, error) {
		if host != "github.com" {
			return nil, fmt.Errorf("host %q", host)
		}
		return f, nil
	}
	latestImage = func(context.Context) (string, error) { return img, nil }
	ciWorkflows = func(_ context.Context, c templates.RESTClient, r string) ([]string, error) {
		if c != f || r != repo {
			return nil, errors.New("wrong client or repo")
		}
		return ciNames, nil
	}
	t.Cleanup(func() { newClient, latestImage, ciWorkflows = oldClient, oldLatest, oldCI })
	return &img
}

func render(t *testing.T, set templates.Set, image string) map[string]string {
	return renderWith(t, set, image, ciNames)
}

// stale renders set as an earlier upgrade did: an older digest, and before the
// repository had its CI workflow.
func stale(t *testing.T, set templates.Set) map[string]string {
	return renderWith(t, set, oldImage, nil)
}

func renderWith(t *testing.T, set templates.Set, image string, workflows []string) map[string]string {
	t.Helper()
	out, err := templates.Render(set, templates.Params{Image: image, DefaultBranch: "main", Workflows: workflows})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for k, v := range out {
		files[k] = string(v)
	}
	return files
}

type confirm struct {
	asked bool
	plan  op.Plan
}

func (c *confirm) Confirm(p op.Plan) (bool, error) { c.asked, c.plan = true, p; return true, nil }

func run(t *testing.T) (*confirm, op.Plan, op.Result, error) {
	t.Helper()
	c := &confirm{}
	p, r, err := op.Run(context.Background(), New(op.RepoParams{Repo: repo}), c)
	return c, p, r, err
}

func actions(p op.Plan) []string {
	var out []string
	for _, s := range p.Changes() {
		out = append(out, s.Action+" "+s.Target)
	}
	return out
}

func TestCurrentRepoWritesNothing(t *testing.T) {
	for _, set := range []templates.Set{templates.Peer, templates.Execution} {
		files := render(t, set, imageRef)
		files[".github/workflows/ci.yml"] = "on: push\n"
		f := newFake(files)
		// A stale upgrade PR stays as it is.
		f.refs[Branch] = "c0"
		f.prs = []*fakePR{{number: 3, head: Branch, state: "open"}}
		stub(t, f)
		c, p, r, err := run(t)
		if err != nil {
			t.Fatalf("%v: %v", set, err)
		}
		if c.asked || !p.ReadOnly || len(p.Changes()) != 0 || len(f.writes()) != 0 {
			t.Errorf("%v: asked %v, plan %+v, writes %v", set, c.asked, p, f.writes())
		}
		items := checks(t, r)
		delete(r, op.ResultItems)
		delete(r, op.ResultNotes)
		want := op.Result{"repo": repo, "image": imageRef, "set": setName(set), "current": true}
		if len(items) != len(p.Steps) || slices.ContainsFunc(items, func(c op.Check) bool { return !c.Confirmed }) {
			t.Errorf("%v: items %+v", set, items)
		}
		if fmt.Sprint(r) != fmt.Sprint(want) {
			t.Errorf("%v: result %v, want %v", set, r, want)
		}
	}
}

func TestFirstRunOpensOnePR(t *testing.T) {
	cases := map[string]struct {
		have    map[string]string
		set     string
		actions []string
	}{
		"peer": {
			have:    stale(t, templates.Peer),
			set:     "peer",
			actions: []string{"update " + issueP, "write branch " + Branch, "open PR " + Branch + " into main"},
		},
		"execution": {
			have:    stale(t, templates.Execution),
			set:     "execution",
			actions: []string{"update " + issueP, "update " + jobP, "write branch " + Branch, "open PR " + Branch + " into main"},
		},
		"partial execution": {
			have:    map[string]string{issueP: "old\n", jobP: "old\n"},
			set:     "execution",
			actions: []string{"update " + issueP, "add " + sharedP, "update " + jobP, "write branch " + Branch, "open PR " + Branch + " into main"},
		},
	}
	for name, c := range cases {
		c.have["README.md"] = "keep\n"
		f := newFake(c.have)
		stub(t, f)
		conf, p, r, err := run(t)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !conf.asked || !p.Confirm || !slices.Equal(actions(p), c.actions) {
			t.Errorf("%s: asked %v, plan %q", name, conf.asked, actions(p))
		}
		if p.Changes()[0].Detail != imageRef {
			t.Errorf("%s: step detail %q", name, p.Changes()[0].Detail)
		}
		wantWrites := []string{"POST repos/o/r/git/trees", "POST repos/o/r/git/commits", "POST repos/o/r/git/refs", "POST repos/o/r/pulls"}
		if !slices.Equal(f.writes(), wantWrites) {
			t.Errorf("%s: writes %q", name, f.writes())
		}
		set := templates.Peer
		if c.set == "execution" {
			set = templates.Execution
		}
		want := render(t, set, imageRef)
		got := f.commits[f.refs[Branch]]
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: %s on the branch differs", name, k)
			}
		}
		if got["README.md"] != "keep\n" || len(got) != len(want)+1 {
			t.Errorf("%s: branch files %v", name, slices.Sorted(maps.Keys(got)))
		}
		if f.refs["main"] != "c0" {
			t.Errorf("%s: the default branch moved", name)
		}
		if len(f.prs) != 1 || f.prs[0].base != "main" {
			t.Errorf("%s: PRs %+v", name, f.prs)
		}
		if r["set"] != c.set || r["pr"] != "https://github.com/o/r/pull/10" || r["number"] != 10 || r["image"] != imageRef || r["repo"] != repo {
			t.Errorf("%s: result %v", name, r)
		}
	}
}

func TestNotInitialized(t *testing.T) {
	f := newFake(map[string]string{jobP: "x"})
	stub(t, f)
	if _, _, _, err := run(t); op.Code(err) != op.CodeNotInitialized || !strings.Contains(err.Error(), "gh nibfin init") {
		t.Errorf("err = %v", err)
	}
	if len(f.writes()) != 0 {
		t.Errorf("writes %v", f.writes())
	}
}

func TestImageFailureWritesNothing(t *testing.T) {
	f := newFake(map[string]string{issueP: "old"})
	stub(t, f)
	latestImage = func(context.Context) (string, error) {
		return "", op.Errorf(op.CodeImageUnverified, "verify failed")
	}
	if _, _, _, err := run(t); op.Code(err) != op.CodeImageUnverified {
		t.Errorf("err = %v", err)
	}
	if len(f.writes()) != 0 {
		t.Errorf("writes %v", f.writes())
	}
}

func TestReadFailuresKeepTheirCause(t *testing.T) {
	cause := &api.HTTPError{StatusCode: 403, Message: "Forbidden"}
	for _, key := range []string{"GET repos/o/r", "GET repos/o/r/contents/" + issueP, "GET repos/o/r/git/ref/heads/" + Branch, "GET repos/o/r/pulls"} {
		f := newFake(map[string]string{issueP: "old"})
		f.fail = map[string]error{key: cause}
		stub(t, f)
		_, _, _, err := run(t)
		if op.Code(err) != op.CodeRepoUnreadable || !errors.Is(err, cause) {
			t.Errorf("%s: %v (code %q)", key, err, op.Code(err))
		}
	}
	f := newFake(nil)
	stub(t, f)
	ciWorkflows = func(context.Context, templates.RESTClient, string) ([]string, error) {
		return nil, op.Errorf(op.CodeWorkflowsUnreadable, "list failed")
	}
	f.commits["c0"] = map[string]string{issueP: "old"}
	if _, _, _, err := run(t); op.Code(err) != op.CodeWorkflowsUnreadable {
		t.Errorf("CI workflows: %v", err)
	}
	if _, err := New(op.RepoParams{Repo: "o/r"}).Plan(context.Background()); op.Code(err) != op.CodeInvalidRepo {
		t.Errorf("bad repo: %v", err)
	}
}

// TestRerunAfterACrash crashes each write after it took effect, then reruns.
// The rerun converges on one PR holding the rendered files.
func TestRerunAfterACrash(t *testing.T) {
	for _, key := range []string{"POST repos/o/r/git/trees", "POST repos/o/r/git/commits", "POST repos/o/r/git/refs", "POST repos/o/r/pulls"} {
		f := newFake(stale(t, templates.Peer))
		f.fail = map[string]error{key: errors.New("connection reset")}
		f.failAfter = true
		stub(t, f)
		if _, _, _, err := run(t); op.Code(err) != op.CodeRepoWriteFailed || strings.Contains(err.Error(), "workflow scope") {
			t.Fatalf("%s: first run %v", key, err)
		}
		before := len(f.calls)
		_, p, r, err := run(t)
		if err != nil {
			t.Fatalf("%s: rerun %v", key, err)
		}
		if len(f.prs) != 1 || r["number"] != 10 {
			t.Errorf("%s: PRs %+v, result %v", key, f.prs, r)
		}
		if f.commits[f.refs[Branch]][issueP] != render(t, templates.Peer, imageRef)[issueP] {
			t.Errorf("%s: the branch does not hold the rendered file", key)
		}
		switch key {
		case "POST repos/o/r/git/refs":
			// The branch already holds the files: only the PR is opened.
			if !slices.Equal(actions(p), []string{"open PR " + Branch + " into main"}) {
				t.Errorf("%s: rerun plan %q", key, actions(p))
			}
		case "POST repos/o/r/pulls":
			if !p.ReadOnly || len(f.calls[before:]) == 0 || slices.ContainsFunc(f.calls[before:], func(c string) bool { return !strings.HasPrefix(c, "GET ") }) {
				t.Errorf("%s: rerun plan %+v, calls %q", key, p, f.calls[before:])
			}
		}
	}
}

func TestOpenPRWithEqualFilesIsReadOnly(t *testing.T) {
	f := newFake(stale(t, templates.Peer))
	stub(t, f)
	if _, _, _, err := run(t); err != nil {
		t.Fatal(err)
	}
	n := len(f.writes())
	c, p, r, err := run(t)
	if err != nil || c.asked || !p.ReadOnly || len(f.writes()) != n {
		t.Fatalf("asked %v, plan %+v, writes %q, err %v", c.asked, p, f.writes()[n:], err)
	}
	if r["pr"] != "https://github.com/o/r/pull/10" || !slices.Equal(r["files"].([]string), []string{issueP}) {
		t.Errorf("result %v", r)
	}
}

func TestStalePRIsUpdatedNotDuplicated(t *testing.T) {
	f := newFake(stale(t, templates.Execution))
	img := stub(t, f)
	*img = oldImage
	// The branch and PR from an upgrade to a digest that is no longer latest.
	f.commits["c0"][issueP] = "older\n"
	if _, _, _, err := run(t); err != nil {
		t.Fatal(err)
	}
	first := f.refs[Branch]
	// The default branch moved and a newer digest is out.
	f.commits["c9"] = f.commits["c0"]
	f.refs["main"] = "c9"
	*img = imageRef
	n := len(f.writes())
	_, p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	last := p.Steps[len(p.Steps)-1]
	if actions(p)[len(actions(p))-1] != "write branch "+Branch || p.Steps[len(p.Steps)-2].Detail != "replaces earlier contents of that branch" || last.Action+" "+last.Target != "keep PR #10" || !last.NoChange {
		t.Errorf("plan %q", actions(p))
	}
	want := []string{"POST repos/o/r/git/trees", "POST repos/o/r/git/commits", "PATCH repos/o/r/git/refs/heads/" + Branch}
	if !slices.Equal(f.writes()[n:], want) {
		t.Errorf("writes %q", f.writes()[n:])
	}
	if len(f.prs) != 1 || f.refs[Branch] == first || r["number"] != 10 {
		t.Errorf("PRs %+v, branch %s, result %v", f.prs, f.refs[Branch], r)
	}
	if f.commits[f.refs[Branch]][jobP] != render(t, templates.Execution, imageRef)[jobP] {
		t.Error("the branch does not hold the newest files")
	}
}

func TestWriteFailureNamesTheScope(t *testing.T) {
	for _, status := range []int{403, 404, 422} {
		f := newFake(stale(t, templates.Peer))
		f.fail = map[string]error{"POST repos/o/r/git/refs": &api.HTTPError{StatusCode: status, Message: "refused"}}
		stub(t, f)
		_, _, _, err := run(t)
		if op.Code(err) != op.CodeRepoWriteFailed || !strings.Contains(err.Error(), "refused") || !strings.Contains(err.Error(), "gh auth refresh -s workflow") {
			t.Errorf("%d: %v", status, err)
		}
		if len(f.prs) != 0 {
			t.Errorf("%d: a PR was opened", status)
		}
	}
}

func TestReadBackChecksTheHead(t *testing.T) {
	f := newFake(stale(t, templates.Peer))
	stub(t, f)
	o := New(op.RepoParams{Repo: repo})
	ctx := context.Background()
	p, err := o.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	written := f.refs[Branch]
	// Someone pushed to the branch after the write.
	f.commits["x1"] = f.commits[written]
	f.refs[Branch] = "x1"
	_, err = o.ReadBack(ctx, p)
	if op.Code(err) != op.CodeRepoWriteFailed || !strings.Contains(err.Error(), "x1") || !strings.Contains(err.Error(), written) {
		t.Errorf("mismatch: %v", err)
	}
	f.prs[0].state = "closed"
	if _, err := o.ReadBack(ctx, p); op.Code(err) != op.CodeRepoWriteFailed {
		t.Errorf("closed PR: %v", err)
	}
}

func TestApplyNeedsAPlan(t *testing.T) {
	o := New(op.RepoParams{Repo: repo})
	if err := o.Apply(context.Background(), op.Plan{}); op.Code(err) != op.CodeInvalidArgument {
		t.Errorf("Apply: %v", err)
	}
	if _, err := o.ReadBack(context.Background(), op.Plan{}); op.Code(err) != op.CodeInvalidArgument {
		t.Errorf("ReadBack: %v", err)
	}
}

func TestConcurrentCreateConverges(t *testing.T) {
	f := newFake(stale(t, templates.Peer))
	stub(t, f)
	ctx := context.Background()
	a, b := New(op.RepoParams{Repo: repo}), New(op.RepoParams{Repo: repo})
	pa, _ := a.Plan(ctx)
	pb, _ := b.Plan(ctx)
	if err := a.Apply(ctx, pa); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(ctx, pb); op.Code(err) != op.CodeRepoWriteFailed {
		t.Errorf("second create: %v", err)
	}
	if _, _, r, err := run(t); err != nil || r["number"] != 10 || len(f.prs) != 1 {
		t.Errorf("rerun %v, %v, PRs %d", r, err, len(f.prs))
	}
}

// checks returns the read-back items of a result.
func checks(t *testing.T, r op.Result) []op.Check {
	t.Helper()
	items, ok := r[op.ResultItems].([]op.Check)
	if !ok {
		t.Fatalf("result has no items: %v", r)
	}
	return items
}

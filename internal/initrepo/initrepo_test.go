package initrepo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	imageRef = "ghcr.io/taco3064/nibfin@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	token    = "gho_SECRETTOKEN123"
	issueP   = ".github/workflows/nibfin-issue.yml"
	sharedP  = ".github/workflows/nibfin-shared-runner.yml"
	jobP     = ".github/workflows/nibfin-job.yml"
)

type fakePR struct {
	number int
	head   string
	state  string
}

// fakeGitHub is an in-memory GitHub behind the REST paths init uses. The
// target is o/<name>; other repositories are listed in repos.
type fakeGitHub struct {
	name          string
	private       bool
	visibility    string
	admin         bool
	prPolicy      *string
	deleteOnMerge bool
	labels        []string // names as GitHub has them
	approval      string
	forks         privateForks
	forkIgnore    bool // a higher-level policy: the PUT succeeds without effect
	vars          map[string]string
	secret        bool
	scopes        *string // nil: no X-OAuth-Scopes header
	repos         map[string]string
	defaultBranch string
	commits       map[string]map[string]string
	refs          map[string]string
	prs           []*fakePR
	fail          map[string]error
	calls         []string
	n             int
}

func strp(s string) *string { return &s }

func newFake(name string) *fakeGitHub {
	return &fakeGitHub{
		name:          name,
		admin:         true,
		prPolicy:      strp("all"),
		deleteOnMerge: true,
		approval:      "first_time_contributors",
		vars:          map[string]string{},
		scopes:        strp("repo, workflow, read:org"),
		repos:         map[string]string{"repos/o/app": "o/app", "repos/o/lib": "o/lib"},
		defaultBranch: "main",
		commits:       map[string]map[string]string{"c0": {"README.md": "hi"}},
		refs:          map[string]string{"main": "c0"},
	}
}

func (f *fakeGitHub) base() string { return "repos/o/" + f.name }

func (f *fakeGitHub) writes() []string {
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "GET ") && c != "HOST start=false" {
			out = append(out, c)
		}
	}
	return out
}

func httpErr(code int, msg string) error { return &api.HTTPError{StatusCode: code, Message: msg} }

func (f *fakeGitHub) RequestWithContext(_ context.Context, method, p string, _ io.Reader) (*http.Response, error) {
	f.calls = append(f.calls, method+" "+p)
	if method != http.MethodGet || p != "user" {
		return nil, fmt.Errorf("unexpected request %s %s", method, p)
	}
	h := http.Header{}
	if f.scopes != nil {
		h.Set("X-OAuth-Scopes", *f.scopes)
	}
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func (f *fakeGitHub) DoWithContext(_ context.Context, method, p string, body io.Reader, response any) error {
	u, err := url.Parse(p)
	if err != nil {
		return err
	}
	key := method + " " + u.Path
	f.calls = append(f.calls, key)
	if e := f.fail[key]; e != nil {
		delete(f.fail, key)
		return e
	}
	var in map[string]any
	if body != nil {
		if err := json.NewDecoder(body).Decode(&in); err != nil {
			return err
		}
	}
	out, err := f.serve(method, u, in)
	if err != nil || response == nil {
		return err
	}
	b, _ := json.Marshal(out)
	return json.Unmarshal(b, response)
}

func (f *fakeGitHub) label(name string) int {
	for i, l := range f.labels {
		if strings.EqualFold(l, name) {
			return i
		}
	}
	return -1
}

func (f *fakeGitHub) serve(method string, u *url.URL, in map[string]any) (any, error) {
	base := f.base()
	p := u.Path
	switch {
	case method == http.MethodGet && p == base:
		r := map[string]any{
			"full_name": "o/" + f.name, "default_branch": f.defaultBranch, "private": f.private,
			"delete_branch_on_merge": f.deleteOnMerge, "permissions": map[string]bool{"admin": f.admin},
		}
		if f.visibility != "" {
			r["visibility"] = f.visibility
		}
		if f.prPolicy != nil {
			r["pull_request_creation_policy"] = *f.prPolicy
		}
		return r, nil
	case method == http.MethodGet && f.repos[strings.ToLower(p)] != "":
		return map[string]any{"full_name": f.repos[strings.ToLower(p)], "default_branch": "main"}, nil
	case method == http.MethodPatch && p == base:
		for k, v := range in {
			switch k {
			case "pull_request_creation_policy":
				f.prPolicy = strp(v.(string))
			case "delete_branch_on_merge":
				f.deleteOnMerge = v.(bool)
			default:
				return nil, fmt.Errorf("patch field %s", k)
			}
		}
		return map[string]any{}, nil
	case method == http.MethodGet && strings.HasPrefix(p, base+"/labels/"):
		if i := f.label(strings.TrimPrefix(p, base+"/labels/")); i >= 0 {
			return map[string]string{"name": f.labels[i]}, nil
		}
		return nil, httpErr(404, "Not Found")
	case method == http.MethodPost && p == base+"/labels":
		if f.label(in["name"].(string)) >= 0 {
			return nil, httpErr(422, "Validation Failed")
		}
		if in["color"] == "" || len(in["description"].(string)) > 100 {
			return nil, fmt.Errorf("label %v", in)
		}
		f.labels = append(f.labels, in["name"].(string))
		return map[string]any{}, nil
	case method == http.MethodPatch && strings.HasPrefix(p, base+"/labels/"):
		i := f.label(strings.TrimPrefix(p, base+"/labels/"))
		if i < 0 {
			return nil, httpErr(404, "Not Found")
		}
		f.labels[i] = in["new_name"].(string)
		return map[string]any{}, nil
	case p == base+"/actions/permissions/fork-pr-contributor-approval":
		if f.private {
			return nil, fmt.Errorf("public route on a private repository")
		}
		if method == http.MethodGet {
			return map[string]string{"approval_policy": f.approval}, nil
		}
		if !f.forkIgnore {
			f.approval = in["approval_policy"].(string)
		}
		return nil, nil
	case p == base+"/actions/permissions/fork-pr-workflows-private-repos":
		if !f.private {
			return nil, fmt.Errorf("private route on a public repository")
		}
		if method == http.MethodGet {
			return f.forks, nil
		}
		if len(in) != 4 {
			return nil, fmt.Errorf("private forks body %v", in)
		}
		if !f.forkIgnore {
			b, _ := json.Marshal(in)
			_ = json.Unmarshal(b, &f.forks)
		}
		return nil, nil
	case method == http.MethodGet && strings.HasPrefix(p, base+"/actions/variables/"):
		name := strings.TrimPrefix(p, base+"/actions/variables/")
		if v, ok := f.vars[name]; ok {
			return map[string]string{"name": name, "value": v}, nil
		}
		return nil, httpErr(404, "Not Found")
	case method == http.MethodPost && p == base+"/actions/variables":
		name := in["name"].(string)
		if _, ok := f.vars[name]; ok {
			return nil, httpErr(409, "Already exists")
		}
		f.vars[name] = in["value"].(string)
		return map[string]any{}, nil
	case method == http.MethodPatch && strings.HasPrefix(p, base+"/actions/variables/"):
		name := strings.TrimPrefix(p, base+"/actions/variables/")
		if _, ok := f.vars[name]; !ok {
			return nil, httpErr(404, "Not Found")
		}
		f.vars[name] = in["value"].(string)
		return map[string]any{}, nil
	case method == http.MethodGet && p == base+"/actions/secrets/"+secretName:
		if f.secret {
			return map[string]string{"name": secretName}, nil
		}
		return nil, httpErr(404, "Not Found")
	case method == http.MethodGet && strings.HasPrefix(p, base+"/contents/"):
		ref := u.Query().Get("ref")
		if ref == "" {
			ref = f.defaultBranch
		}
		sha, ok := f.refs[ref]
		if !ok {
			sha = ref
		}
		content, ok := f.commits[sha][strings.TrimPrefix(p, base+"/contents/")]
		if !ok {
			return nil, httpErr(404, "Not Found")
		}
		return map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))}, nil
	case method == http.MethodGet && strings.HasPrefix(p, base+"/git/ref/heads/"):
		sha, ok := f.refs[strings.TrimPrefix(p, base+"/git/ref/heads/")]
		if !ok {
			return nil, httpErr(404, "Not Found")
		}
		return map[string]any{"object": map[string]string{"sha": sha}}, nil
	case method == http.MethodGet && strings.HasPrefix(p, base+"/git/commits/"):
		sha := strings.TrimPrefix(p, base+"/git/commits/")
		return map[string]any{"sha": sha, "tree": map[string]string{"sha": "tree-" + sha}}, nil
	case method == http.MethodPost && p == base+"/git/trees":
		files := map[string]string{}
		for k, v := range f.commits[strings.TrimPrefix(in["base_tree"].(string), "tree-")] {
			files[k] = v
		}
		for _, e := range in["tree"].([]any) {
			e := e.(map[string]any)
			files[e["path"].(string)] = e["content"].(string)
		}
		f.n++
		sha := fmt.Sprintf("t%d", f.n)
		f.commits["pending-"+sha] = files
		return map[string]string{"sha": sha}, nil
	case method == http.MethodPost && p == base+"/git/commits":
		if !strings.HasPrefix(in["message"].(string), "Set up Nibfin workflows (") {
			return nil, fmt.Errorf("message %v", in["message"])
		}
		f.n++
		sha := fmt.Sprintf("c%d", f.n)
		f.commits[sha] = f.commits["pending-"+in["tree"].(string)]
		return map[string]string{"sha": sha}, nil
	case method == http.MethodPost && p == base+"/git/refs":
		name := strings.TrimPrefix(in["ref"].(string), "refs/heads/")
		if _, ok := f.refs[name]; ok {
			return nil, httpErr(422, "Reference already exists")
		}
		f.refs[name] = in["sha"].(string)
		return map[string]any{}, nil
	case method == http.MethodPatch && strings.HasPrefix(p, base+"/git/refs/heads/"):
		f.refs[strings.TrimPrefix(p, base+"/git/refs/heads/")] = in["sha"].(string)
		return map[string]any{}, nil
	case method == http.MethodGet && p == base+"/pulls":
		if u.Query().Get("head") != "o:"+Branch {
			return nil, fmt.Errorf("query %v", u.Query())
		}
		out := []any{}
		for _, pr := range f.prs {
			if pr.state == "open" {
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
		return nil, httpErr(404, "Not Found")
	case method == http.MethodPost && p == base+"/pulls":
		if in["title"] != "Set up Nibfin" || in["head"] != Branch || !strings.Contains(in["body"].(string), "Collaborators only") {
			return nil, fmt.Errorf("PR %v", in)
		}
		pr := &fakePR{number: 10 + len(f.prs), head: Branch, state: "open"}
		f.prs = append(f.prs, pr)
		return f.prJSON(pr), nil
	}
	return nil, fmt.Errorf("unexpected %s %s", method, p)
}

func (f *fakeGitHub) prJSON(pr *fakePR) map[string]any {
	return map[string]any{
		"number": pr.number, "html_url": fmt.Sprintf("https://github.com/%s/pull/%d", f.base()[6:], pr.number),
		"state": pr.state, "head": map[string]string{"ref": pr.head, "sha": f.refs[pr.head]},
	}
}

// fakeRunners is this host's Runners.
type fakeRunners struct {
	stopped   bool
	repo      string
	providers []string
	err       error
	starts    int
}

type ghCall struct {
	args  []string
	stdin string
}

type fixture struct {
	gh      *fakeGitHub
	runners *fakeRunners
	secrets *[]ghCall
	ghErr   *string
}

func stub(t *testing.T, f *fakeGitHub, r *fakeRunners) fixture {
	t.Helper()
	oldClient, oldRunners, oldLatest, oldCI, oldToken, oldGH := newClient, hostRunners, latestImage, ciWorkflows, tokenForHost, runGH
	t.Cleanup(func() {
		newClient, hostRunners, latestImage, ciWorkflows, tokenForHost, runGH = oldClient, oldRunners, oldLatest, oldCI, oldToken, oldGH
	})
	newClient = func(host string) (Client, error) {
		if host != "github.com" {
			return nil, fmt.Errorf("host %q", host)
		}
		return f, nil
	}
	hostRunners = func(_ context.Context, start bool) (op.RunnerHostInfo, error) {
		f.calls = append(f.calls, fmt.Sprintf("HOST start=%v", start))
		info := op.RunnerHostInfo{Describe: "fake", Providers: []string{}}
		if r.err != nil {
			return info, r.err
		}
		if r.stopped {
			if !start {
				info.Stopped = true
				return info, nil
			}
			r.stopped = false
			r.starts++
		}
		info.Repo, info.Providers = r.repo, r.providers
		return info, nil
	}
	latestImage = func(context.Context) (string, error) { return imageRef, nil }
	ciWorkflows = func(context.Context, templates.RESTClient, string) ([]string, error) { return []string{"CI"}, nil }
	tokenForHost = func(host string) (string, string) {
		if host != "github.com" {
			return "", "default"
		}
		return token, "keyring"
	}
	calls := &[]ghCall{}
	ghErr := new(string)
	runGH = func(_ context.Context, args []string, stdin io.Reader) (string, error) {
		b, _ := io.ReadAll(stdin)
		*calls = append(*calls, ghCall{args, string(b)})
		f.calls = append(f.calls, "GH "+strings.Join(args[:2], " "))
		if *ghErr != "" {
			return *ghErr, errors.New("exit status 1")
		}
		f.secret = true
		return "", nil
	}
	return fixture{f, r, calls, ghErr}
}

type yes struct{}

func (yes) Confirm(op.Plan) (bool, error) { return true, nil }

func run(t *testing.T) (op.Plan, op.Result, error) {
	t.Helper()
	return op.Run(context.Background(), New(op.RepoParams{Repo: "github.com/o/app"}), yes{})
}

func runOn(name string) (op.Plan, op.Result, error) {
	return op.Run(context.Background(), New(op.RepoParams{Repo: "github.com/o/" + name}), yes{})
}

// actions lists the changing items; allActions every item.
func actions(p op.Plan) []string { return names(p.Changes()) }

func allActions(p op.Plan) []string { return names(p.Steps) }

func names(steps []op.Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.Action+" "+s.Target)
	}
	return out
}

func execRunners() *fakeRunners {
	return &fakeRunners{repo: "github.com/O/App", providers: []string{"claude"}}
}

func TestInitExecutionSet(t *testing.T) {
	f := newFake("app")
	fx := stub(t, f, execRunners())
	p, r, err := run(t)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	want := []string{
		"create label scope", "create label shaped", "create label delivered", "create label pause", "create label pending",
		"set pull requests collaborators_only", "set delete head branches false",
		"set fork PR approval all external contributors",
		"create variable NIBFIN_EXECUTION_REPO", "create variable NIBFIN_PROVIDER",
		"set secret NIBFIN_GITHUB_TOKEN",
		"add " + issueP, "add " + sharedP, "add " + jobP,
		"write branch " + Branch, "open PR " + Branch + " into main",
	}
	if !slices.Equal(actions(p), want) || !p.Confirm || !slices.Equal(p.Notes, []string{"set in the web UI https://github.com/o/app/settings: " + issuesManual}) {
		t.Errorf("plan = %q", actions(p))
	}
	if f.vars[varExecRepo] != "o/app" || f.vars[varProvider] != "claude" || *f.prPolicy != policyCollab || f.deleteOnMerge || f.approval != allExternal {
		t.Errorf("state: vars %v, policy %v, delete %v, approval %v", f.vars, *f.prPolicy, f.deleteOnMerge, f.approval)
	}
	if r["set"] != "execution" || r["executionRepo"] != "o/app" || r["provider"] != "claude" || r["pr"] != "https://github.com/o/app/pull/10" ||
		r["secret"] != "set" || r["image"] != imageRef || r["visibility"] != "public" || r["scopes"] != nil {
		t.Errorf("result = %v", r)
	}
	if fa := r["forkPrApproval"].(map[string]any); fa["route"] != routePublic || fa["approvalPolicy"] != allExternal {
		t.Errorf("fork = %v", fa)
	}
	if m := r["manual"].(map[string]string); m["url"] != "https://github.com/o/app/settings" || !strings.Contains(m["instruction"], "Collaborators only") {
		t.Errorf("manual = %v", m)
	}
	files := f.commits[f.refs[Branch]]
	for _, p := range []string{issueP, sharedP, jobP} {
		if !strings.HasPrefix(files[p], templates.GeneratedPrefix) {
			t.Errorf("%s on the branch: %.40q", p, files[p])
		}
	}
	// The token goes on stdin only.
	if len(*fx.secrets) != 1 || (*fx.secrets)[0].stdin != token ||
		!slices.Equal((*fx.secrets)[0].args, []string{"secret", "set", "NIBFIN_GITHUB_TOKEN", "--app", "actions", "--repo", "github.com/o/app"}) {
		t.Errorf("gh = %+v", *fx.secrets)
	}
	for _, s := range p.Steps {
		if strings.Contains(s.Target+s.Detail, token) {
			t.Errorf("token in plan step %+v", s)
		}
	}
	if b, _ := json.Marshal(r); strings.Contains(string(b), token) {
		t.Error("token in result")
	}

	// Order: the host, labels, settings, fork approval, variables, secret, PR.
	order := []string{"HOST start=false", "POST repos/o/app/labels", "PATCH repos/o/app",
		"PUT repos/o/app/actions/permissions/fork-pr-contributor-approval", "POST repos/o/app/actions/variables",
		"GH secret set", "POST repos/o/app/git/trees"}
	at := 0
	for _, c := range f.calls[slices.Index(f.calls, "GET user")+1:] {
		if at < len(order) && c == order[at] {
			at++
		}
	}
	if at != len(order) {
		t.Errorf("order stops at %q in %q", order[min(at, len(order)-1)], f.calls)
	}

	// A rerun keeps everything; only the secret is set again.
	f.calls = nil
	p, r, err = run(t)
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	want = []string{"set secret NIBFIN_GITHUB_TOKEN"}
	if !slices.Equal(actions(p), want) || !strings.Contains(p.Changes()[0].Detail, "replaces the existing value") || !slices.Contains(allActions(p), "keep PR #10") {
		t.Errorf("rerun plan = %q", actions(p))
	}
	if w := f.writes(); !slices.Equal(w, []string{"GH secret set"}) {
		t.Errorf("rerun writes = %q", w)
	}
	if r["number"] != 10 || len(f.prs) != 1 {
		t.Errorf("rerun result = %v", r)
	}
}

func TestInitPeerSet(t *testing.T) {
	f := newFake("lib")
	f.vars[varProvider] = "codex" // left alone
	stub(t, f, execRunners())
	p, r, err := runOn("lib")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if slices.Contains(actions(p), "add "+sharedP) || !slices.Contains(actions(p), "add "+issueP) || slices.Contains(actions(p), "create variable NIBFIN_PROVIDER") {
		t.Errorf("plan = %q", actions(p))
	}
	files := f.commits[f.refs[Branch]]
	if _, ok := files[sharedP]; ok || files[issueP] == "" {
		t.Errorf("branch files = %v", files)
	}
	if f.vars[varExecRepo] != "o/app" || r["set"] != "peer" || r["provider"] != nil || f.vars[varProvider] != "codex" {
		t.Errorf("vars %v, result %v", f.vars, r)
	}
	if !slices.ContainsFunc(f.calls, func(c string) bool { return strings.EqualFold(c, "GET repos/o/app") }) {
		t.Errorf("execution repository not read: %q", f.calls)
	}
}

func TestInitNoRunners(t *testing.T) {
	for _, r := range []*fakeRunners{
		{},
		{repo: "ghe.example.com/o/app", providers: []string{"claude"}},
		{err: op.Errorf(op.CodeRunnerRepoConflict, "disagree")},
		{err: op.Errorf(op.CodePodmanMissing, "no podman")},
	} {
		f := newFake("app")
		stub(t, f, r)
		_, _, err := run(t)
		want := op.CodeRunnerRepoConflict
		switch {
		case r.err != nil:
			want = op.Code(r.err)
		case r.repo == "":
			want = op.CodeNoRunners
			if !strings.Contains(err.Error(), "gh nibfin runner add") {
				t.Errorf("no runners: %v", err)
			}
		}
		if op.Code(err) != want || len(f.writes()) != 0 {
			t.Errorf("%+v: %v, writes %q", r, err, f.writes())
		}
	}
}

func TestInitStoppedMachine(t *testing.T) {
	f := newFake("app")
	fx := stub(t, f, &fakeRunners{stopped: true, repo: "github.com/o/app", providers: []string{"codex"}})
	p, r, err := run(t)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if acts := actions(p); acts[0] != "start fake" || !slices.Contains(acts, actDerive+" github.com/o/app") || slices.Contains(acts, "create variable NIBFIN_EXECUTION_REPO") {
		t.Errorf("plan = %q", acts)
	}
	if fx.runners.starts != 1 || f.vars[varProvider] != "codex" || r["set"] != "execution" || r["pr"] == nil {
		t.Errorf("starts %d, vars %v, result %v", fx.runners.starts, f.vars, r)
	}
	if i := slices.Index(f.calls, "HOST start=true"); i < 0 || i > slices.Index(f.calls, "POST repos/o/app/labels") {
		t.Errorf("start not first: %q", f.calls)
	}

	// No runners after the start: nothing on the repository is written.
	f = newFake("app")
	stub(t, f, &fakeRunners{stopped: true})
	if _, _, err = run(t); op.Code(err) != op.CodeNoRunners || !slices.Equal(f.writes(), []string{"HOST start=true"}) {
		t.Errorf("stopped, none: %v, %q", err, f.writes())
	}
}

func TestInitRunnersChangedSincePlan(t *testing.T) {
	f := newFake("app")
	fx := stub(t, f, execRunners())
	o := New(op.RepoParams{Repo: "github.com/o/app"})
	p, err := o.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fx.runners.repo = "github.com/o/lib"
	if err := o.Apply(context.Background(), p); op.Code(err) != op.CodeRunnerRepoConflict || !strings.Contains(err.Error(), "plan again") || len(f.writes()) != 0 {
		t.Errorf("changed: %v, %q", err, f.writes())
	}
}

func TestInitLabels(t *testing.T) {
	f := newFake("app")
	f.labels = []string{"Scope", "pause", "shaped"}
	f.fail = map[string]error{}
	stub(t, f, execRunners())
	o := New(op.RepoParams{Repo: "github.com/o/app"})
	p, err := o.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range actions(p) {
		if strings.Contains(a, "label") {
			got = append(got, a)
		}
	}
	if !slices.Equal(got, []string{"rename label Scope", "create label delivered", "create label pending"}) {
		t.Errorf("label steps = %q", got)
	}
	// "pending" is created by someone else between Plan and Apply.
	f.labels = append(f.labels, "pending")
	if err := o.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	r, err := o.ReadBack(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r["labels"].([]string), []string{"scope", "shaped", "delivered", "pause", "pending"}) || slices.Contains(f.labels, "Scope") {
		t.Errorf("labels = %v, %v", r["labels"], f.labels)
	}
}

func TestInitSettings(t *testing.T) {
	f := newFake("app")
	f.prPolicy = nil // not reported
	f.deleteOnMerge = false
	f.approval = allExternal
	var patch map[string]any
	stub(t, f, execRunners())
	client := newClient
	newClient = func(host string) (Client, error) {
		c, err := client(host)
		return patchSpy{c, &patch}, err
	}
	p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(actions(p), "set pull requests collaborators_only") || slices.Contains(actions(p), "set delete head branches false") ||
		slices.Contains(actions(p), "set fork PR approval all external contributors") {
		t.Errorf("plan = %q", actions(p))
	}
	if len(patch) != 1 || patch["pull_request_creation_policy"] != policyCollab {
		t.Errorf("patch = %v", patch)
	}
	if slices.Contains(f.calls, "PUT repos/o/app/actions/permissions/fork-pr-contributor-approval") {
		t.Error("an equal policy is written")
	}
	if r["deleteBranchOnMerge"] != false || r["pullRequestCreation"] != policyCollab {
		t.Errorf("result = %v", r)
	}

	// A repository that never reports the field reads back as not reported.
	f2 := newFake("app")
	stub(t, f2, execRunners())
	newClient = func(string) (Client, error) { return dropPolicy{f2}, nil }
	if _, r, err = run(t); err != nil || r["pullRequestCreation"] != "not reported by GitHub" {
		t.Errorf("unreported: %v, %v", r, err)
	}
}

// patchSpy records the repository PATCH body.
type patchSpy struct {
	Client
	body *map[string]any
}

func (s patchSpy) DoWithContext(ctx context.Context, method, p string, body io.Reader, response any) error {
	if method == http.MethodPatch && p == "repos/o/app" {
		b, _ := io.ReadAll(body)
		_ = json.Unmarshal(b, s.body)
		body = strings.NewReader(string(b))
	}
	return s.Client.DoWithContext(ctx, method, p, body, response)
}

// dropPolicy hides pull_request_creation_policy, as a GitHub that does not
// report it.
type dropPolicy struct{ *fakeGitHub }

func (d dropPolicy) DoWithContext(ctx context.Context, method, p string, body io.Reader, response any) error {
	if method != http.MethodGet || p != "repos/o/app" {
		return d.fakeGitHub.DoWithContext(ctx, method, p, body, response)
	}
	var m map[string]any
	if err := d.fakeGitHub.DoWithContext(ctx, method, p, body, &m); err != nil {
		return err
	}
	delete(m, "pull_request_creation_policy")
	b, _ := json.Marshal(m)
	return json.Unmarshal(b, response)
}

func TestInitForkPrivate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		forks privateForks
		step  string
		write bool
	}{
		{"approval off", privateForks{Run: true, WriteTokens: true}, "require fork PR approval github.com/o/app", true},
		{"forks off", privateForks{SecretsAndVar: true}, "keep fork PR workflows off github.com/o/app", false},
		{"both on", privateForks{Run: true, Require: true}, "", false},
	} {
		f := newFake("app")
		f.private, f.forks = true, tc.forks
		stub(t, f, execRunners())
		p, r, err := run(t)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.step != "" && !slices.Contains(allActions(p), tc.step) {
			t.Errorf("%s: plan = %q", tc.name, allActions(p))
		}
		put := slices.Contains(f.calls, "PUT repos/o/app/actions/permissions/fork-pr-workflows-private-repos")
		if put != tc.write || slices.ContainsFunc(f.calls, func(c string) bool { return strings.Contains(c, "contributor-approval") }) {
			t.Errorf("%s: calls %q", tc.name, f.calls)
		}
		want := tc.forks
		if tc.write {
			want.Require = true
		}
		if f.forks != want {
			t.Errorf("%s: forks = %+v, want %+v", tc.name, f.forks, want)
		}
		if fa := r["forkPrApproval"].(map[string]any); fa["route"] != routePrivate || fa["runWorkflowsFromForks"] != want.Run || r["visibility"] != "private" {
			t.Errorf("%s: result %v", tc.name, r)
		}
	}
}

func TestInitForkFailures(t *testing.T) {
	for _, route := range []string{"fork-pr-contributor-approval", "fork-pr-workflows-private-repos"} {
		private := strings.Contains(route, "private")
		endpoint := "repos/o/app/actions/permissions/" + route
		for _, code := range []int{403, 404} {
			f := newFake("app")
			f.private, f.forks = private, privateForks{Run: true}
			f.fail = map[string]error{"GET " + endpoint: httpErr(code, "nope")}
			stub(t, f, execRunners())
			if _, _, err := run(t); op.Code(err) != op.CodeGitHubAPIFailed || !strings.Contains(err.Error(), endpoint) || len(f.writes()) != 0 {
				t.Errorf("GET %s %d: %v, %q", route, code, err, f.writes())
			}
		}
		for _, code := range []int{403, 404, 409, 422} {
			f := newFake("app")
			f.private, f.forks = private, privateForks{Run: true}
			f.fail = map[string]error{"PUT " + endpoint: httpErr(code, "managed by the enterprise")}
			fx := stub(t, f, execRunners())
			_, _, err := run(t)
			if op.Code(err) != op.CodeGitHubAPIFailed || !strings.Contains(err.Error(), "managed by the enterprise") || !strings.Contains(err.Error(), "organization or enterprise") {
				t.Errorf("PUT %s %d: %v", route, code, err)
			}
			// Labels and settings stay; no variable, Secret or branch follows.
			if len(f.labels) != 5 || f.deleteOnMerge || len(f.vars) != 0 || len(*fx.secrets) != 0 || f.refs[Branch] != "" {
				t.Errorf("PUT %s %d: labels %v, vars %v, secrets %d, branch %q", route, code, f.labels, f.vars, len(*fx.secrets), f.refs[Branch])
			}
		}
		// A higher-level policy that silently wins reads back as not applied.
		f := newFake("app")
		f.private, f.forks, f.forkIgnore = private, privateForks{Run: true}, true
		stub(t, f, execRunners())
		if _, _, err := run(t); op.Code(err) != op.CodeNotApplied || !strings.Contains(err.Error(), "fork PR") {
			t.Errorf("ignored %s: %v", route, err)
		}
	}
}

func TestInitVariables(t *testing.T) {
	// An existing value is updated; a valid provider is kept even with two
	// providers.
	f := newFake("app")
	f.vars = map[string]string{varExecRepo: "o/old", varProvider: "codex"}
	stub(t, f, &fakeRunners{repo: "github.com/o/app", providers: []string{"claude", "codex"}})
	p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(actions(p), "update variable NIBFIN_EXECUTION_REPO") || slices.ContainsFunc(actions(p), func(a string) bool { return strings.HasSuffix(a, varProvider) }) {
		t.Errorf("plan = %q", actions(p))
	}
	if f.vars[varExecRepo] != "o/app" || f.vars[varProvider] != "codex" || r["provider"] != "codex" {
		t.Errorf("vars = %v", f.vars)
	}

	// Two providers and no valid value: init does not choose.
	f = newFake("app")
	f.vars = map[string]string{varProvider: "gemini"}
	stub(t, f, &fakeRunners{repo: "github.com/o/app", providers: []string{"claude", "codex"}})
	if _, _, err := run(t); op.Code(err) != op.CodeProviderAmbiguous || !strings.Contains(err.Error(), "gh variable set NIBFIN_PROVIDER") || len(f.writes()) != 0 {
		t.Errorf("ambiguous: %v, %q", err, f.writes())
	}

	// A variable created between Plan and Apply is updated.
	f = newFake("app")
	stub(t, f, execRunners())
	o := New(op.RepoParams{Repo: "github.com/o/app"})
	plan, err := o.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.vars[varExecRepo] = "o/x"
	if err := o.Apply(context.Background(), plan); err != nil || f.vars[varExecRepo] != "o/app" {
		t.Errorf("409: %v, %v", err, f.vars)
	}
}

func TestInitScopeAndAdmin(t *testing.T) {
	f := newFake("app")
	f.scopes = strp("repo, read:org")
	stub(t, f, execRunners())
	if _, _, err := run(t); op.Code(err) != op.CodeScopeMissing || !strings.Contains(err.Error(), "gh auth refresh -s workflow") || len(f.writes()) != 0 {
		t.Errorf("scope: %v, %q", err, f.writes())
	}
	f = newFake("app")
	f.scopes = nil
	stub(t, f, execRunners())
	if _, r, err := run(t); err != nil || r["scopes"] != "unread" {
		t.Errorf("no header: %v, %v", r, err)
	}
	f = newFake("app")
	f.admin = false
	stub(t, f, execRunners())
	if _, _, err := run(t); op.Code(err) != op.CodeRepoWriteFailed || !strings.Contains(err.Error(), "admin") || len(f.calls) != 1 {
		t.Errorf("admin: %v, %q", err, f.calls)
	}
}

func TestInitSecretFailures(t *testing.T) {
	f := newFake("app")
	fx := stub(t, f, execRunners())
	*fx.ghErr = "failed to set secret " + token + ": HTTP 403"
	_, _, err := run(t)
	if op.Code(err) != op.CodeRepoWriteFailed || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[token]") || f.refs[Branch] != "" {
		t.Errorf("gh failure: %v", err)
	}
	f = newFake("app")
	stub(t, f, execRunners())
	tokenForHost = func(string) (string, string) { return "", "default" }
	if _, _, err := run(t); op.Code(err) != op.CodeAuthUnavailable {
		t.Errorf("no token: %v", err)
	}
}

func TestInitAlreadyInitialized(t *testing.T) {
	f := newFake("app")
	f.commits["c0"][issueP] = templates.GeneratedPrefix + "\n"
	stub(t, f, execRunners())
	p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(allActions(p), "keep "+issueP) || slices.Contains(allActions(p), "open PR "+Branch+" into main") || slices.Contains(actions(p), "keep "+issueP) {
		t.Errorf("plan = %q", actions(p))
	}
	if r["initialized"] != true || r["next"] != "gh nibfin upgrade" || r["pr"] != nil || len(f.prs) != 0 || f.vars[varExecRepo] != "o/app" {
		t.Errorf("result = %v", r)
	}
}

func TestInitBranchWithoutPR(t *testing.T) {
	// A crash after the branch write: the rerun only opens the PR.
	f := newFake("app")
	f.fail = map[string]error{"POST repos/o/app/pulls": httpErr(502, "boom")}
	stub(t, f, execRunners())
	if _, _, err := run(t); op.Code(err) != op.CodeRepoWriteFailed {
		t.Fatalf("first: %v", err)
	}
	head := f.refs[Branch]
	p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(actions(p), "write branch "+Branch) || !slices.Contains(actions(p), "open PR "+Branch+" into main") || f.refs[Branch] != head || r["number"] != 10 {
		t.Errorf("rerun plan = %q, result %v", actions(p), r)
	}
}

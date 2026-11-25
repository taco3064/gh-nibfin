// Package initrepo is the init operation of the shared operation layer. It
// prepares one repository for Nibfin (labels, settings, fork PR approval,
// variables and the NIBFIN_GITHUB_TOKEN Secret) and opens one PR that
// imports Nibfin's workflows: the execution set when this host's Runners
// serve the repository, the peer set otherwise. It lives outside internal/op
// because templates and image import op for their error codes.
package initrepo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"slices"
	"strings"

	gh "github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"

	"github.com/taco3064/gh-nibfin/internal/image"
	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
	"github.com/taco3064/gh-nibfin/internal/workflowpr"
)

// Branch is the branch the init PR comes from.
const Branch = "nibfin/init"

// Client is the part of go-gh's api.RESTClient init uses. It carries the
// current gh login; the token never leaves it.
type Client interface {
	templates.RESTClient
	RequestWithContext(ctx context.Context, method, path string, body io.Reader) (*http.Response, error)
}

// Tests replace these.
var (
	newClient = func(host string) (Client, error) {
		return api.NewRESTClient(api.ClientOptions{Host: host})
	}
	hostRunners  = op.HostRunners
	latestImage  = image.Latest
	ciWorkflows  = templates.CIWorkflows
	tokenForHost = auth.TokenForHost
	// runGH runs the user's gh with args and stdin; it returns gh's stderr.
	runGH = func(ctx context.Context, args []string, stdin io.Reader) (string, error) {
		exe, err := gh.Path()
		if err != nil {
			return "", err
		}
		var stderr bytes.Buffer
		c := exec.CommandContext(ctx, exe, args...)
		c.Stdin = stdin
		c.Stdout = io.Discard
		c.Stderr = &stderr
		err = c.Run()
		return stderr.String(), err
	}
)

const (
	secretName   = "NIBFIN_GITHUB_TOKEN"
	varExecRepo  = "NIBFIN_EXECUTION_REPO"
	varProvider  = "NIBFIN_PROVIDER"
	issueFile    = "nibfin-issue.yml"
	policyCollab = "collaborators_only"
	allExternal  = "all_external_contributors"
	routePublic  = "contributor-approval"
	routePrivate = "private-repos"
	issuesManual = "Issues → Creation allowed by: Collaborators only (GitHub has no API for it)"
	forkHint     = "fork PR approval may be managed by the organization or enterprise; require approval there, then run gh nibfin init again"
)

// labels are the labels Nibfin uses, with the colour and description a new
// one gets. An existing label keeps its own.
var labels = []struct{ name, color, description string }{
	{"scope", "1d76db", "Nibfin works on this Issue"},
	{"shaped", "0e8a16", "Nibfin: the Issue has an approved Shape"},
	{"delivered", "5319e7", "Nibfin: the Issue's work is delivered"},
	{"pause", "fbca04", "Nibfin: paused until resumed"},
	{"pending", "d93f0b", "Nibfin: waiting for a person"},
}

// providers are the values NIBFIN_PROVIDER may hold.
var providers = []string{"claude", "codex"}

// The plan steps whose Apply is derived from state rather than performed
// one by one.
const (
	actStart  = "start"
	actDerive = "choose set and write workflows"
)

// New is the entry point of init.
func New(p op.RepoParams) op.Operation { return &initRepo{p: p} }

type repoInfo struct {
	FullName      string  `json:"full_name"`
	DefaultBranch string  `json:"default_branch"`
	Private       bool    `json:"private"`
	Visibility    string  `json:"visibility"`
	PRPolicy      *string `json:"pull_request_creation_policy"`
	DeleteOnMerge bool    `json:"delete_branch_on_merge"`
	Permissions   struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
}

type privateForks struct {
	Run           bool `json:"run_workflows_from_fork_pull_requests"`
	WriteTokens   bool `json:"send_write_tokens_to_workflows"`
	SecretsAndVar bool `json:"send_secrets_and_variables"`
	Require       bool `json:"require_approval_for_fork_pr_workflows"`
}

type labelPlan struct {
	name  string
	found string // the existing name when it differs in case
	color string
	desc  string
}

type variable struct {
	name, want, old string
	exists          bool
}

// initRepo keeps what Plan read for Apply and ReadBack on the same value.
type initRepo struct {
	p op.RepoParams

	planned    bool
	repo       workflowpr.Repo
	client     Client
	host       string
	info       repoInfo
	scopesRead bool

	stopped bool
	runners op.RunnerHostInfo

	labels   []labelPlan
	settings map[string]any

	forkPolicy  string       // public route: the current approval_policy
	forkPrivate privateForks // private route: the current settings
	forkWrite   bool

	// Derived from the Runners (after a start when the machine is stopped).
	set          templates.Set
	execRepo     string // OWNER/REPO
	vars         []variable
	secretExists bool
	initialized  bool
	image        string
	files        map[string][]byte
	onDefault    map[string][]byte
	pr           *workflowpr.PR
}

func setName(s templates.Set) string {
	if s == templates.Execution {
		return "execution"
	}
	return "peer"
}

// apiErr is a failed GitHub request; a coded cause keeps its code.
func apiErr(what string, err error) error {
	return op.Wrap(op.CodeGitHubAPIFailed, fmt.Errorf("%s: %w", what, err))
}

func (i *initRepo) get(ctx context.Context, p string, out any) error {
	return i.client.DoWithContext(ctx, http.MethodGet, p, nil, out)
}

func (i *initRepo) send(ctx context.Context, method, p string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return i.client.DoWithContext(ctx, method, p, bytes.NewReader(b), out)
}

func (i *initRepo) Plan(ctx context.Context) (op.Plan, error) {
	plan := op.Plan{Operation: "init", Repo: i.p.Repo, Steps: []op.Step{}}
	*i = initRepo{p: i.p}
	if err := i.connect(); err != nil {
		return plan, err
	}
	if err := i.readRepo(ctx); err != nil {
		return plan, err
	}
	if !i.info.Permissions.Admin {
		return plan, op.Errorf(op.CodeRepoWriteFailed, "init needs admin access to %s", i.p.Repo)
	}
	if err := i.checkScope(ctx); err != nil {
		return plan, err
	}

	runners, err := hostRunners(ctx, false)
	if err != nil {
		return plan, err
	}
	i.stopped = runners.Stopped
	if i.stopped {
		plan.Steps = append(plan.Steps, op.Step{Action: actStart, Target: runners.Describe})
	} else if err := i.derive(ctx, runners); err != nil {
		return plan, err
	}

	if err := i.planLabels(ctx); err != nil {
		return plan, err
	}
	i.planSettings()
	if err := i.planFork(ctx); err != nil {
		return plan, err
	}
	if i.secretExists, err = i.exists(ctx, i.repo.Base+"/actions/secrets/"+secretName); err != nil {
		return plan, apiErr("read the secret "+secretName+" of "+i.p.Repo, err)
	}

	for _, l := range i.labels {
		if l.found != "" {
			plan.Steps = append(plan.Steps, op.Step{Action: "rename label", Target: l.found, Detail: "to " + l.name})
		} else {
			plan.Steps = append(plan.Steps, op.Step{Action: "create label", Target: l.name, Detail: l.desc})
		}
	}
	if _, ok := i.settings["pull_request_creation_policy"]; ok {
		from := "not reported by GitHub"
		if i.info.PRPolicy != nil {
			from = "from " + *i.info.PRPolicy
		}
		plan.Steps = append(plan.Steps, op.Step{Action: "set pull requests", Target: policyCollab, Detail: from})
	}
	if _, ok := i.settings["delete_branch_on_merge"]; ok {
		plan.Steps = append(plan.Steps, op.Step{Action: "set delete head branches", Target: "false", Detail: "from true"})
	}
	switch {
	case !i.info.Private && i.forkWrite:
		plan.Steps = append(plan.Steps, op.Step{Action: "set fork PR approval", Target: "all external contributors", Detail: "from " + i.forkPolicy})
	case i.info.Private && i.forkWrite:
		plan.Steps = append(plan.Steps, op.Step{Action: "require fork PR approval", Target: i.p.Repo, Detail: "private repository: Require approval for fork pull request workflows"})
	case i.info.Private && !i.forkPrivate.Run:
		plan.Steps = append(plan.Steps, op.Step{Action: "keep fork PR workflows off", Target: i.p.Repo, Detail: "private repository: fork pull request workflows do not run"})
	}

	if i.stopped {
		plan.Steps = append(plan.Steps, op.Step{Action: actDerive, Target: i.p.Repo, Detail: "after start, from the Runners on " + runners.Describe})
	} else {
		for _, v := range i.vars {
			switch {
			case !v.exists:
				plan.Steps = append(plan.Steps, op.Step{Action: "create variable", Target: v.name, Detail: v.want})
			case v.old != v.want:
				plan.Steps = append(plan.Steps, op.Step{Action: "update variable", Target: v.name, Detail: v.want + " from " + v.old})
			}
		}
	}
	detail := "from the current gh login of " + i.host
	if i.secretExists {
		detail += "; replaces the existing value"
	}
	plan.Steps = append(plan.Steps, op.Step{Action: "set secret", Target: secretName, Detail: detail})
	if !i.stopped {
		plan.Steps = append(plan.Steps, i.workflowSteps()...)
	}
	plan.Steps = append(plan.Steps, op.Step{Action: "set in the web UI", Target: i.settingsURL(), Detail: issuesManual})
	i.planned = true
	plan.Confirm = true
	return plan, nil
}

// connect parses HOST/OWNER/REPO and builds the client for HOST.
func (i *initRepo) connect() error {
	repo, host, err := workflowpr.NewRepo(i.p.Repo, func(host string) (templates.RESTClient, error) {
		c, err := newClient(host)
		i.client = c
		return c, err
	})
	if err != nil {
		return err
	}
	i.repo, i.host = repo, host
	return nil
}

func (i *initRepo) readRepo(ctx context.Context) error {
	var info repoInfo
	if err := i.get(ctx, i.repo.Base, &info); err != nil {
		return workflowpr.ReadErr("read the repository "+i.p.Repo, err)
	}
	if info.DefaultBranch == "" || info.FullName == "" {
		return op.Errorf(op.CodeRepoUnreadable, "read the repository %s: no default branch or name", i.p.Repo)
	}
	i.info = info
	return nil
}

// checkScope reads the scopes of the gh login from X-OAuth-Scopes. A login
// whose scopes GitHub does not report (a fine-grained token) continues.
func (i *initRepo) checkScope(ctx context.Context) error {
	resp, err := i.client.RequestWithContext(ctx, http.MethodGet, "user", nil)
	if err != nil {
		return apiErr("read the gh login of "+i.host, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	vals, ok := resp.Header[http.CanonicalHeaderKey("X-OAuth-Scopes")]
	if !ok {
		return nil
	}
	i.scopesRead = true
	for _, v := range vals {
		for _, s := range strings.Split(v, ",") {
			if strings.TrimSpace(s) == "workflow" {
				return nil
			}
		}
	}
	return op.Errorf(op.CodeScopeMissing, "the gh login of %s lacks the workflow scope; run gh auth refresh -s workflow", i.host)
}

// derive chooses the set from the Runners and plans the variables and the
// workflows.
func (i *initRepo) derive(ctx context.Context, runners op.RunnerHostInfo) error {
	i.runners = runners
	if runners.Repo == "" {
		return op.Errorf(op.CodeNoRunners, "no runners on %s; add them first: gh nibfin runner add <claude|codex>", runners.Describe)
	}
	parts := strings.Split(runners.Repo, "/")
	if len(parts) != 3 {
		return op.Errorf(op.CodeRunnerRepoConflict, "the runners on %s serve %q, which is not HOST/OWNER/REPO", runners.Describe, runners.Repo)
	}
	if !strings.EqualFold(parts[0], i.host) {
		return op.Errorf(op.CodeRunnerRepoConflict, "the runners on %s serve %s, on another host than %s", runners.Describe, runners.Repo, i.p.Repo)
	}
	i.vars = nil
	if strings.EqualFold(runners.Repo, i.p.Repo) {
		i.set = templates.Execution
		i.execRepo = i.info.FullName
	} else {
		i.set = templates.Peer
		var exec repoInfo
		p := "repos/" + url.PathEscape(parts[1]) + "/" + url.PathEscape(parts[2])
		if err := i.get(ctx, p, &exec); err != nil {
			return workflowpr.ReadErr("read the execution repository "+runners.Repo, err)
		}
		if exec.FullName == "" {
			return op.Errorf(op.CodeRepoUnreadable, "read the execution repository %s: no name", runners.Repo)
		}
		i.execRepo = exec.FullName
	}
	v, err := i.readVariable(ctx, varExecRepo)
	if err != nil {
		return err
	}
	v.want = i.execRepo
	i.vars = append(i.vars, v)
	if i.set == templates.Execution {
		v, err := i.readVariable(ctx, varProvider)
		if err != nil {
			return err
		}
		switch {
		case v.exists && slices.Contains(providers, v.old):
			v.want = v.old
		case len(runners.Providers) == 1:
			v.want = runners.Providers[0]
		default:
			return op.Errorf(op.CodeProviderAmbiguous, "the runners on %s serve %s with more than one provider (%s); set NIBFIN_PROVIDER with gh variable set NIBFIN_PROVIDER -R %s --body <claude|codex> and run init again",
				runners.Describe, i.p.Repo, strings.Join(runners.Providers, ", "), i.p.Repo)
		}
		i.vars = append(i.vars, v)
	}
	return i.planWorkflows(ctx)
}

func (i *initRepo) readVariable(ctx context.Context, name string) (variable, error) {
	var got struct {
		Value string `json:"value"`
	}
	err := i.get(ctx, i.repo.Base+"/actions/variables/"+name, &got)
	if workflowpr.IsStatus(err, http.StatusNotFound) {
		return variable{name: name}, nil
	}
	if err != nil {
		return variable{}, apiErr("read the variable "+name+" of "+i.p.Repo, err)
	}
	return variable{name: name, old: got.Value, exists: true}, nil
}

// exists reports whether a GET of p finds something.
func (i *initRepo) exists(ctx context.Context, p string) (bool, error) {
	err := i.get(ctx, p, &json.RawMessage{})
	if workflowpr.IsStatus(err, http.StatusNotFound) {
		return false, nil
	}
	return err == nil, err
}

// readLabel returns the label's name as GitHub has it, "" when missing.
func (i *initRepo) readLabel(ctx context.Context, name string) (string, error) {
	var got struct {
		Name string `json:"name"`
	}
	err := i.get(ctx, i.repo.Base+"/labels/"+url.PathEscape(name), &got)
	if workflowpr.IsStatus(err, http.StatusNotFound) {
		return "", nil
	}
	if err != nil {
		return "", apiErr("read the label "+name+" of "+i.p.Repo, err)
	}
	return got.Name, nil
}

func (i *initRepo) planLabels(ctx context.Context) error {
	for _, l := range labels {
		found, err := i.readLabel(ctx, l.name)
		if err != nil {
			return err
		}
		switch {
		case found == l.name:
		case found == "":
			i.labels = append(i.labels, labelPlan{name: l.name, color: l.color, desc: l.description})
		default:
			i.labels = append(i.labels, labelPlan{name: l.name, found: found})
		}
	}
	return nil
}

func (i *initRepo) planSettings() {
	i.settings = map[string]any{}
	if i.info.PRPolicy == nil || *i.info.PRPolicy != policyCollab {
		i.settings["pull_request_creation_policy"] = policyCollab
	}
	if i.info.DeleteOnMerge {
		i.settings["delete_branch_on_merge"] = false
	}
}

func (i *initRepo) forkPath() string {
	if i.info.Private {
		return i.repo.Base + "/actions/permissions/fork-pr-workflows-private-repos"
	}
	return i.repo.Base + "/actions/permissions/fork-pr-contributor-approval"
}

// readFork reads fork PR approval from the route of the repository's kind.
func (i *initRepo) readFork(ctx context.Context) (policy string, private privateForks, err error) {
	if i.info.Private {
		err = i.get(ctx, i.forkPath(), &private)
	} else {
		var got struct {
			Policy string `json:"approval_policy"`
		}
		err = i.get(ctx, i.forkPath(), &got)
		policy = got.Policy
	}
	if err != nil {
		err = apiErr("read fork PR approval of "+i.p.Repo+" ("+i.forkPath()+")", err)
	}
	return policy, private, err
}

func (i *initRepo) planFork(ctx context.Context) error {
	policy, private, err := i.readFork(ctx)
	if err != nil {
		return err
	}
	i.forkPolicy, i.forkPrivate = policy, private
	if i.info.Private {
		i.forkWrite = private.Run && !private.Require
	} else {
		i.forkWrite = policy != allExternal
	}
	return nil
}

// planWorkflows reads the default branch and, unless the repository is
// initialized, renders the set and reads the init branch and PR.
func (i *initRepo) planWorkflows(ctx context.Context) error {
	all := []string{path.Join(templates.Dir, issueFile), path.Join(templates.Dir, "nibfin-shared-runner.yml"), path.Join(templates.Dir, "nibfin-job.yml")}
	onDefault, err := i.repo.ReadFiles(ctx, all, "", i.info.DefaultBranch)
	if err != nil {
		return err
	}
	i.onDefault = onDefault
	i.pr = nil
	if onDefault[all[0]] != nil {
		i.initialized = true
		return nil
	}
	i.initialized = false
	if i.image, err = latestImage(ctx); err != nil {
		return err
	}
	workflows, err := ciWorkflows(ctx, i.client, i.p.Repo)
	if err != nil {
		return err
	}
	if i.files, err = templates.Render(i.set, templates.Params{Image: i.image, DefaultBranch: i.info.DefaultBranch, Workflows: workflows}); err != nil {
		return err
	}
	var paths []string
	for _, p := range all {
		if _, ok := i.files[p]; ok {
			paths = append(paths, p)
		}
	}
	i.pr = &workflowpr.PR{
		Repo:          i.repo,
		DefaultBranch: i.info.DefaultBranch,
		Branch:        Branch,
		Files:         i.files,
		Paths:         paths,
		Message:       fmt.Sprintf("Set up Nibfin workflows (%s set, %s)", setName(i.set), i.image),
		Title:         "Set up Nibfin",
		Body:          i.prBody(paths),
	}
	return i.pr.Read(ctx)
}

func (i *initRepo) workflowSteps() []op.Step {
	if i.initialized {
		return []op.Step{{Action: "keep workflows", Target: path.Join(templates.Dir, issueFile), Detail: "already initialized; use gh nibfin upgrade"}}
	}
	if i.pr.Done() {
		return []op.Step{{Action: "keep PR", Target: fmt.Sprintf("#%d", i.pr.Open.Number), Detail: i.pr.Open.HTMLURL}}
	}
	var steps []op.Step
	if !i.pr.BranchCurrent {
		for _, p := range i.pr.Paths {
			action := "add"
			if i.onDefault[p] != nil {
				action = "update"
			}
			steps = append(steps, op.Step{Action: action, Target: p, Detail: setName(i.set) + " set, " + i.image})
		}
	}
	return append(steps, i.pr.Steps()...)
}

func (i *initRepo) prBody(paths []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Imports Nibfin's workflows (the %s set) into this repository, pinned to the latest attested image:\n\n", setName(i.set))
	fmt.Fprintf(&b, "`%s`\n\nFiles:\n\n", i.image)
	for _, p := range paths {
		fmt.Fprintf(&b, "- `%s`\n", p)
	}
	fmt.Fprintf(&b, "\nGitHub offers one prerequisite only in the web UI. In %s set %s.\n", i.settingsURL(), "Issues → Creation allowed by: Collaborators only")
	b.WriteString("\nGenerated by `gh nibfin init`.\n")
	return b.String()
}

func (i *initRepo) settingsURL() string {
	name := i.info.FullName
	if name == "" {
		name = i.repo.Owner + "/" + path.Base(i.p.Repo)
	}
	return "https://" + i.host + "/" + name + "/settings"
}

func (i *initRepo) Apply(ctx context.Context, _ op.Plan) error {
	if !i.planned {
		return op.Errorf(op.CodeInvalidArgument, "init: Apply needs a Plan on the same operation")
	}
	// The host comes first, so no_runners stops before any repository write.
	if i.stopped {
		runners, err := hostRunners(ctx, true)
		if err != nil {
			return err
		}
		if err := i.derive(ctx, runners); err != nil {
			return err
		}
		i.stopped = false
	} else {
		runners, err := hostRunners(ctx, false)
		if err != nil {
			return err
		}
		if !strings.EqualFold(runners.Repo, i.runners.Repo) {
			return op.Errorf(op.CodeRunnerRepoConflict, "the plan used the runners of %q but the runners on %s now serve %q; plan again", i.runners.Repo, runners.Describe, runners.Repo)
		}
	}

	for _, l := range i.labels {
		if err := i.applyLabel(ctx, l); err != nil {
			return err
		}
	}
	if len(i.settings) > 0 {
		if err := i.send(ctx, http.MethodPatch, i.repo.Base, i.settings, nil); err != nil {
			return apiErr("update the settings of "+i.p.Repo, err)
		}
	}
	if i.forkWrite {
		var body any = map[string]any{"approval_policy": allExternal}
		if i.info.Private {
			body = privateForks{Run: true, Require: true, WriteTokens: i.forkPrivate.WriteTokens, SecretsAndVar: i.forkPrivate.SecretsAndVar}
		}
		if err := i.client.DoWithContext(ctx, http.MethodPut, i.forkPath(), jsonBody(body), nil); err != nil {
			return op.Wrap(op.CodeGitHubAPIFailed, fmt.Errorf("require fork PR approval on %s (PUT %s): %w; %s", i.p.Repo, i.forkPath(), err, forkHint))
		}
	}
	for _, v := range i.vars {
		if err := i.applyVariable(ctx, v); err != nil {
			return err
		}
	}
	if err := i.setSecret(ctx); err != nil {
		return err
	}
	if i.initialized || i.pr.Done() {
		return nil
	}
	return i.pr.Apply(ctx)
}

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func (i *initRepo) applyLabel(ctx context.Context, l labelPlan) error {
	if l.found != "" {
		if err := i.send(ctx, http.MethodPatch, i.repo.Base+"/labels/"+url.PathEscape(l.found), map[string]any{"new_name": l.name}, nil); err != nil {
			return apiErr("rename the label "+l.found+" of "+i.p.Repo+" to "+l.name, err)
		}
		return nil
	}
	err := i.send(ctx, http.MethodPost, i.repo.Base+"/labels", map[string]any{"name": l.name, "color": l.color, "description": l.desc}, nil)
	if workflowpr.IsStatus(err, http.StatusUnprocessableEntity) {
		// Created meanwhile: success when it now exists.
		if found, rerr := i.readLabel(ctx, l.name); rerr == nil && strings.EqualFold(found, l.name) {
			return nil
		}
	}
	if err != nil {
		return apiErr("create the label "+l.name+" of "+i.p.Repo, err)
	}
	return nil
}

func (i *initRepo) applyVariable(ctx context.Context, v variable) error {
	if v.exists && v.old == v.want {
		return nil
	}
	body := map[string]any{"name": v.name, "value": v.want}
	patch := func() error {
		return i.send(ctx, http.MethodPatch, i.repo.Base+"/actions/variables/"+v.name, body, nil)
	}
	var err error
	if v.exists {
		err = patch()
	} else {
		err = i.send(ctx, http.MethodPost, i.repo.Base+"/actions/variables", body, nil)
		if workflowpr.IsStatus(err, http.StatusConflict) {
			err = patch()
		}
	}
	if err != nil {
		return apiErr("set the variable "+v.name+" of "+i.p.Repo, err)
	}
	return nil
}

// secretArgs are the arguments of the gh secret set that stores the login.
// The value goes on stdin, never into the arguments.
func (i *initRepo) secretArgs() []string {
	return []string{"secret", "set", secretName, "--app", "actions", "--repo", i.p.Repo}
}

// setSecret stores the current gh login of the host as the Secret. gh
// encrypts it with the repository's key; the token is only on gh's stdin.
func (i *initRepo) setSecret(ctx context.Context) error {
	token, _ := tokenForHost(i.host)
	if token == "" {
		return op.Errorf(op.CodeAuthUnavailable, "no gh login for %s; run gh auth login -h %s", i.host, i.host)
	}
	stderr, err := runGH(ctx, i.secretArgs(), strings.NewReader(token))
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return op.Errorf(op.CodeCancelled, "cancelled")
	}
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = err.Error()
	}
	msg = strings.ReplaceAll(msg, token, "[token]")
	return op.Errorf(op.CodeRepoWriteFailed, "set the secret %s of %s with gh secret set: %s", secretName, i.p.Repo, msg)
}

func (i *initRepo) ReadBack(ctx context.Context, _ op.Plan) (op.Result, error) {
	if !i.planned || i.stopped {
		return nil, op.Errorf(op.CodeInvalidArgument, "init: ReadBack needs a Plan and an Apply on the same operation")
	}
	if err := i.readRepo(ctx); err != nil {
		return nil, err
	}
	visibility := i.info.Visibility
	if visibility == "" {
		visibility = "public"
		if i.info.Private {
			visibility = "private"
		}
	}
	r := op.Result{
		"repo":          i.p.Repo,
		"visibility":    visibility,
		"set":           setName(i.set),
		"executionRepo": i.execRepo,
		"manual":        map[string]string{"url": i.settingsURL(), "instruction": issuesManual},
	}
	if !i.scopesRead {
		r["scopes"] = "unread"
	}
	var notApplied []string

	got := []string{}
	for _, l := range labels {
		found, err := i.readLabel(ctx, l.name)
		if err != nil {
			return nil, err
		}
		if found != l.name {
			notApplied = append(notApplied, "label "+l.name)
			continue
		}
		got = append(got, found)
	}
	r["labels"] = got

	if i.info.PRPolicy == nil {
		r["pullRequestCreation"] = "not reported by GitHub"
	} else {
		r["pullRequestCreation"] = *i.info.PRPolicy
		if *i.info.PRPolicy != policyCollab {
			notApplied = append(notApplied, "pull_request_creation_policy is "+*i.info.PRPolicy)
		}
	}
	r["deleteBranchOnMerge"] = i.info.DeleteOnMerge
	if i.info.DeleteOnMerge {
		notApplied = append(notApplied, "delete_branch_on_merge is true")
	}

	policy, private, err := i.readFork(ctx)
	if err != nil {
		return nil, err
	}
	if i.info.Private {
		r["forkPrApproval"] = map[string]any{"route": routePrivate, "runWorkflowsFromForks": private.Run, "requireApproval": private.Require}
		if private.Run && !private.Require {
			notApplied = append(notApplied, "fork PR workflows run without approval")
		}
	} else {
		r["forkPrApproval"] = map[string]any{"route": routePublic, "approvalPolicy": policy}
		if policy != allExternal {
			notApplied = append(notApplied, "fork PR approval policy is "+policy)
		}
	}

	vars := map[string]string{}
	for _, want := range i.vars {
		v, err := i.readVariable(ctx, want.name)
		if err != nil {
			return nil, err
		}
		if !v.exists || v.old != want.want {
			notApplied = append(notApplied, "variable "+want.name)
		}
		vars[v.name] = v.old
		if v.name == varProvider {
			r["provider"] = v.old
		}
	}
	r["variables"] = vars

	has, err := i.exists(ctx, i.repo.Base+"/actions/secrets/"+secretName)
	if err != nil {
		return nil, apiErr("read the secret "+secretName+" of "+i.p.Repo, err)
	}
	if !has {
		notApplied = append(notApplied, "secret "+secretName)
	} else {
		r["secret"] = "set"
	}

	if len(notApplied) > 0 {
		return nil, op.Errorf(op.CodeNotApplied, "init of %s reads back different: %s", i.p.Repo, strings.Join(notApplied, "; "))
	}

	if i.initialized {
		r["initialized"] = true
		r["next"] = "gh nibfin upgrade"
		return r, nil
	}
	pr, err := i.pr.ReadBack(ctx)
	if err != nil {
		return nil, err
	}
	r["image"] = i.image
	r["files"] = slices.Clone(i.pr.Paths)
	r["pr"] = pr.HTMLURL
	r["number"] = pr.Number
	return r, nil
}

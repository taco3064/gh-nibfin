// Package upgrade is the upgrade operation of the shared operation layer. It
// renders a repository's Nibfin workflow set from the current templates,
// pinned to the latest attested image digest, and opens one PR when the
// result differs from the default branch. It lives outside internal/op
// because templates and image import op for their error codes.
package upgrade

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/image"
	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
	"github.com/taco3064/gh-nibfin/internal/workflowpr"
)

// Branch is the branch every upgrade PR comes from. A fixed name lets a rerun
// find and update the PR it opened before.
const Branch = "nibfin/upgrade"

// Tests replace these.
var (
	// newClient returns a client for host that carries the current gh login.
	newClient = func(host string) (templates.RESTClient, error) {
		return api.NewRESTClient(api.ClientOptions{Host: host})
	}
	latestImage = image.Latest
	ciWorkflows = templates.CIWorkflows
)

const issueFile = "nibfin-issue.yml"

// executionOnly are the files only the execution set has.
var executionOnly = []string{"nibfin-shared-runner.yml", "nibfin-job.yml"}

// New is the entry point of upgrade.
func New(p op.RepoParams) op.Operation { return &upgrade{p: p} }

// upgrade keeps what Plan read for Apply and ReadBack on the same value.
type upgrade struct {
	p op.RepoParams

	planned       bool
	repo          workflowpr.Repo
	image         string
	set           templates.Set
	defaultBranch string
	files         map[string][]byte // the rendered set, keyed by path
	changed       []string          // paths that differ from the default branch
	current       bool              // the default branch holds the rendered set
	pr            *workflowpr.PR    // the branch and its PR, when the set differs
	steps         []op.Step         // the plan items
}

func setName(s templates.Set) string {
	if s == templates.Execution {
		return "execution"
	}
	return "peer"
}

func (u *upgrade) Plan(ctx context.Context) (op.Plan, error) {
	plan := op.Plan{Operation: "upgrade", Repo: u.p.Repo, Steps: []op.Step{}}
	*u = upgrade{p: u.p}
	repo, _, err := workflowpr.NewRepo(u.p.Repo, newClient)
	if err != nil {
		return plan, err
	}
	u.repo = repo

	var info struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := u.repo.Client.DoWithContext(ctx, http.MethodGet, u.repo.Base, nil, &info); err != nil {
		return plan, workflowpr.ReadErr("read the repository "+u.p.Repo, err)
	}
	if info.DefaultBranch == "" {
		return plan, op.Errorf(op.CodeRepoUnreadable, "read the repository %s: no default branch", u.p.Repo)
	}
	u.defaultBranch = info.DefaultBranch

	onDefault, err := u.repo.ReadFiles(ctx, u.paths(), "", u.defaultBranch)
	if err != nil {
		return plan, err
	}
	if onDefault[path.Join(templates.Dir, issueFile)] == nil {
		return plan, op.Errorf(op.CodeNotInitialized, "%s has no %s/%s on %s; run gh nibfin init", u.p.Repo, templates.Dir, issueFile, u.defaultBranch)
	}
	u.set = templates.Peer
	for _, f := range executionOnly {
		if onDefault[path.Join(templates.Dir, f)] != nil {
			u.set = templates.Execution
		}
	}

	if u.image, err = latestImage(ctx); err != nil {
		return plan, err
	}
	workflows, err := ciWorkflows(ctx, u.repo.Client, u.p.Repo)
	if err != nil {
		return plan, err
	}
	if u.files, err = templates.Render(u.set, templates.Params{Image: u.image, DefaultBranch: u.defaultBranch, Workflows: workflows}); err != nil {
		return plan, err
	}
	for _, p := range u.paths() {
		if !bytes.Equal(onDefault[p], u.files[p]) {
			u.changed = append(u.changed, p)
		}
	}
	u.planned = true
	if len(u.changed) == 0 {
		u.current = true
		u.steps = workflowpr.KeptSteps(u.paths(), Branch, "the default branch holds the current set")
		plan.Steps = u.steps
		plan.ReadOnly = true
		return plan, nil
	}

	u.pr = &workflowpr.PR{
		Repo:          u.repo,
		DefaultBranch: u.defaultBranch,
		Branch:        Branch,
		Files:         u.files,
		Paths:         u.paths(),
		Message:       "Upgrade Nibfin workflows to " + u.image,
		Title:         "Upgrade Nibfin workflows",
		Body:          u.prBody(),
		OnDefault:     onDefault,
		FileDetail:    u.image,
	}
	if err := u.pr.Read(ctx); err != nil {
		return plan, err
	}
	u.steps = u.pr.Steps()
	plan.Steps = u.steps
	if u.pr.Done() {
		plan.ReadOnly = true
		return plan, nil
	}
	plan.Confirm = true
	return plan, nil
}

// paths are the paths of every file a set may hold, in a fixed order.
func (u *upgrade) paths() []string {
	all := append([]string{issueFile}, executionOnly...)
	out := make([]string, 0, len(all))
	for _, f := range all {
		p := path.Join(templates.Dir, f)
		if _, ok := u.files[p]; ok || u.files == nil {
			out = append(out, p)
		}
	}
	return out
}

func (u *upgrade) Apply(ctx context.Context, _ op.Plan) error {
	if !u.planned {
		return op.Errorf(op.CodeInvalidArgument, "upgrade: Apply needs a Plan on the same operation")
	}
	if u.current {
		return nil
	}
	return u.pr.Apply(ctx)
}

func (u *upgrade) prBody() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Moves this repository's Nibfin workflows (the %s set) to the current templates and the latest attested image:\n\n", setName(u.set))
	fmt.Fprintf(&b, "`%s`\n\nChanged files:\n\n", u.image)
	for _, p := range u.changed {
		fmt.Fprintf(&b, "- `%s`\n", p)
	}
	b.WriteString("\nGenerated by `gh nibfin upgrade`.\n")
	return b.String()
}

// ReadBack reads every item back: the files of a current set on the
// default branch, or the branch and the PR. The items are returned also
// with an error.
func (u *upgrade) ReadBack(ctx context.Context, _ op.Plan) (op.Result, error) {
	if !u.planned {
		return nil, op.Errorf(op.CodeInvalidArgument, "upgrade: ReadBack needs a Plan on the same operation")
	}
	r := op.Result{"repo": u.p.Repo, "image": u.image, "set": setName(u.set)}
	var checks op.Checks
	if u.current {
		r["current"] = true
		err := u.repo.ReadBackKept(ctx, &checks, u.steps, u.defaultBranch, u.files)
		return checks.Fill(r, nil, err), err
	}
	pr, err := u.pr.ReadBackItems(ctx, &checks, u.steps)
	if err != nil {
		return checks.Fill(r, nil, err), err
	}
	r["files"] = slices.Clone(u.changed)
	r["pr"] = pr.HTMLURL
	r["number"] = pr.Number
	return checks.Fill(r, nil, nil), nil
}

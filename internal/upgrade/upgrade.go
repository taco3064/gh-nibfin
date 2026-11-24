// Package upgrade is the upgrade operation of the shared operation layer. It
// renders a repository's Nibfin workflow set from the current templates,
// pinned to the latest attested image digest, and opens one PR when the
// result differs from the default branch. It lives outside internal/op
// because templates and image import op for their error codes.
package upgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/image"
	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
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

const (
	issueFile = "nibfin-issue.yml"
	scopeHint = "writing .github/workflows needs the workflow scope; run gh auth refresh -s workflow"
)

// executionOnly are the files only the execution set has.
var executionOnly = []string{"nibfin-shared-runner.yml", "nibfin-job.yml"}

// New is the entry point of upgrade.
func New(p op.RepoParams) op.Operation { return &upgrade{p: p} }

type pullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

// upgrade keeps what Plan read for Apply and ReadBack on the same value.
type upgrade struct {
	p op.RepoParams

	planned       bool
	client        templates.RESTClient
	base          string // repos/OWNER/REPO
	owner         string
	image         string
	set           templates.Set
	defaultBranch string
	files         map[string][]byte // the rendered set, keyed by path
	changed       []string          // paths that differ from the default branch
	current       bool              // the default branch holds the rendered set
	branchSHA     string            // head of Branch, "" when it does not exist
	branchCurrent bool              // Branch holds the rendered set
	pr            *pullRequest      // the open PR from Branch
	written       string            // the commit Apply wrote, "" when none
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
	if err := u.connect(); err != nil {
		return plan, err
	}

	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := u.client.DoWithContext(ctx, http.MethodGet, u.base, nil, &repo); err != nil {
		return plan, readErr("read the repository "+u.p.Repo, err)
	}
	if repo.DefaultBranch == "" {
		return plan, op.Errorf(op.CodeRepoUnreadable, "read the repository %s: no default branch", u.p.Repo)
	}
	u.defaultBranch = repo.DefaultBranch

	onDefault, err := u.readSet(ctx, "")
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
	workflows, err := ciWorkflows(ctx, u.client, u.p.Repo)
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
		plan.ReadOnly = true
		return plan, nil
	}

	if err := u.readBranch(ctx); err != nil {
		return plan, err
	}
	if u.branchCurrent && u.pr != nil {
		plan.ReadOnly = true
		return plan, nil
	}

	for _, p := range u.changed {
		action := "update"
		if onDefault[p] == nil {
			action = "add"
		}
		plan.Steps = append(plan.Steps, op.Step{Action: action, Target: p, Detail: u.image})
	}
	switch {
	case u.branchCurrent:
	case u.branchSHA != "":
		plan.Steps = append(plan.Steps, op.Step{Action: "write branch", Target: Branch, Detail: "replaces earlier contents of that branch"})
	default:
		plan.Steps = append(plan.Steps, op.Step{Action: "write branch", Target: Branch, Detail: "new branch from " + u.defaultBranch})
	}
	if u.pr != nil {
		plan.Steps = append(plan.Steps, op.Step{Action: "keep PR", Target: fmt.Sprintf("#%d", u.pr.Number), Detail: u.pr.HTMLURL})
	} else {
		plan.Steps = append(plan.Steps, op.Step{Action: "open PR", Target: Branch + " into " + u.defaultBranch})
	}
	plan.Confirm = true
	return plan, nil
}

// connect parses HOST/OWNER/REPO and builds the client for HOST.
func (u *upgrade) connect() error {
	parts := strings.Split(u.p.Repo, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return op.Errorf(op.CodeInvalidRepo, "repository %q is not HOST/OWNER/REPO", u.p.Repo)
	}
	c, err := newClient(parts[0])
	if err != nil {
		return op.Wrap(op.CodeRepoUnreadable, fmt.Errorf("connect to %s: %w", parts[0], err))
	}
	u.client = c
	u.owner = parts[1]
	u.base = "repos/" + url.PathEscape(parts[1]) + "/" + url.PathEscape(parts[2])
	return nil
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

// readSet reads every Nibfin workflow file at ref. A missing file has no
// entry.
func (u *upgrade) readSet(ctx context.Context, ref string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, p := range u.paths() {
		data, found, err := templates.ReadFile(ctx, u.client, u.p.Repo, p, ref)
		if err != nil {
			where := ref
			if where == "" {
				where = u.defaultBranch
			}
			return nil, readErr(fmt.Sprintf("read %s at %s of %s", p, where, u.p.Repo), err)
		}
		if found {
			out[p] = data
		}
	}
	return out, nil
}

// readBranch reads Branch, its files and the open PR from it.
func (u *upgrade) readBranch(ctx context.Context) error {
	sha, err := u.refSHA(ctx, Branch)
	if err != nil {
		return readErr(fmt.Sprintf("read the branch %s of %s", Branch, u.p.Repo), err)
	}
	u.branchSHA = sha
	if sha != "" {
		onBranch, err := u.readSet(ctx, sha)
		if err != nil {
			return err
		}
		u.branchCurrent = true
		for _, p := range u.paths() {
			if !bytes.Equal(onBranch[p], u.files[p]) {
				u.branchCurrent = false
			}
		}
	}
	u.pr, err = u.openPR(ctx)
	return err
}

// refSHA returns the commit a branch points to, or "" when it does not exist.
func (u *upgrade) refSHA(ctx context.Context, branch string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err := u.client.DoWithContext(ctx, http.MethodGet, u.base+"/git/ref/heads/"+escapeRef(branch), nil, &ref)
	if isStatus(err, http.StatusNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if ref.Object.SHA == "" {
		return "", fmt.Errorf("the ref %s has no commit", branch)
	}
	return ref.Object.SHA, nil
}

// openPR returns the open PR from Branch, or nil.
func (u *upgrade) openPR(ctx context.Context) (*pullRequest, error) {
	q := url.Values{"head": {u.owner + ":" + Branch}, "state": {"open"}}
	var prs []pullRequest
	if err := u.client.DoWithContext(ctx, http.MethodGet, u.base+"/pulls?"+q.Encode(), nil, &prs); err != nil {
		return nil, readErr(fmt.Sprintf("read the open PRs from %s of %s", Branch, u.p.Repo), err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

func (u *upgrade) Apply(ctx context.Context, _ op.Plan) error {
	if !u.planned {
		return op.Errorf(op.CodeInvalidArgument, "upgrade: Apply needs a Plan on the same operation")
	}
	u.written = ""
	if u.current {
		return nil
	}
	if !u.branchCurrent {
		head, err := u.refSHA(ctx, u.defaultBranch)
		if err == nil && head == "" {
			err = fmt.Errorf("the branch %s does not exist", u.defaultBranch)
		}
		if err != nil {
			return readErr(fmt.Sprintf("read the branch %s of %s", u.defaultBranch, u.p.Repo), err)
		}
		var commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		}
		if err := u.client.DoWithContext(ctx, http.MethodGet, u.base+"/git/commits/"+url.PathEscape(head), nil, &commit); err != nil {
			return readErr(fmt.Sprintf("read the commit %s of %s", head, u.p.Repo), err)
		}

		type entry struct {
			Path    string `json:"path"`
			Mode    string `json:"mode"`
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		entries := []entry{}
		for _, p := range u.paths() {
			entries = append(entries, entry{Path: p, Mode: "100644", Type: "blob", Content: string(u.files[p])})
		}
		var tree struct {
			SHA string `json:"sha"`
		}
		if err := u.write(ctx, http.MethodPost, "/git/trees", map[string]any{"base_tree": commit.Tree.SHA, "tree": entries}, &tree, "write the tree"); err != nil {
			return err
		}
		var created struct {
			SHA string `json:"sha"`
		}
		msg := "Upgrade Nibfin workflows to " + u.image
		if err := u.write(ctx, http.MethodPost, "/git/commits", map[string]any{"message": msg, "tree": tree.SHA, "parents": []string{head}}, &created, "write the commit"); err != nil {
			return err
		}
		if created.SHA == "" {
			return op.Errorf(op.CodeRepoWriteFailed, "write the commit to %s: GitHub returned no commit", u.p.Repo)
		}
		if u.branchSHA != "" {
			err = u.write(ctx, http.MethodPatch, "/git/refs/heads/"+escapeRef(Branch), map[string]any{"sha": created.SHA, "force": true}, nil, "update the branch "+Branch)
		} else {
			err = u.write(ctx, http.MethodPost, "/git/refs", map[string]any{"ref": "refs/heads/" + Branch, "sha": created.SHA}, nil, "create the branch "+Branch)
		}
		if err != nil {
			return err
		}
		u.written = created.SHA
	}
	if u.pr == nil {
		var pr pullRequest
		body := map[string]any{"title": "Upgrade Nibfin workflows", "head": Branch, "base": u.defaultBranch, "body": u.prBody()}
		if err := u.write(ctx, http.MethodPost, "/pulls", body, &pr, "open the PR from "+Branch); err != nil {
			return err
		}
		u.pr = &pr
	}
	return nil
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

// write sends one changing request. A failure is repo_write_failed with
// go-gh's message; a 403, 404 or 422 also names the workflow scope.
func (u *upgrade) write(ctx context.Context, method, p string, body, response any, what string) error {
	b, err := json.Marshal(body)
	if err != nil {
		return op.Wrap(op.CodeRepoWriteFailed, err)
	}
	err = u.client.DoWithContext(ctx, method, u.base+p, bytes.NewReader(b), response)
	if err == nil {
		return nil
	}
	if isStatus(err, http.StatusForbidden) || isStatus(err, http.StatusNotFound) || isStatus(err, http.StatusUnprocessableEntity) {
		return op.Errorf(op.CodeRepoWriteFailed, "%s of %s: %w; %s", what, u.p.Repo, err, scopeHint)
	}
	return op.Errorf(op.CodeRepoWriteFailed, "%s of %s: %w", what, u.p.Repo, err)
}

func (u *upgrade) ReadBack(ctx context.Context, _ op.Plan) (op.Result, error) {
	if !u.planned {
		return nil, op.Errorf(op.CodeInvalidArgument, "upgrade: ReadBack needs a Plan on the same operation")
	}
	r := op.Result{"repo": u.p.Repo, "image": u.image, "set": setName(u.set)}
	if u.current {
		r["current"] = true
		return r, nil
	}
	pr, err := u.readPR(ctx)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, op.Errorf(op.CodeRepoWriteFailed, "no open PR from %s in %s", Branch, u.p.Repo)
	}
	want := u.written
	if want == "" {
		want = u.branchSHA
	}
	if pr.Head.SHA != want {
		return nil, op.Errorf(op.CodeRepoWriteFailed, "PR #%d of %s has head %s, want %s", pr.Number, u.p.Repo, pr.Head.SHA, want)
	}
	r["files"] = slices.Clone(u.changed)
	r["pr"] = pr.HTMLURL
	r["number"] = pr.Number
	return r, nil
}

// readPR re-reads the open PR from Branch: by number when Plan found it or
// Apply opened it, since a just-opened PR may not be listed yet.
func (u *upgrade) readPR(ctx context.Context) (*pullRequest, error) {
	if u.pr == nil {
		return u.openPR(ctx)
	}
	var pr pullRequest
	if err := u.client.DoWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/pulls/%d", u.base, u.pr.Number), nil, &pr); err != nil {
		return nil, readErr(fmt.Sprintf("read PR #%d of %s", u.pr.Number, u.p.Repo), err)
	}
	if pr.State != "open" || pr.Head.Ref != Branch {
		return nil, nil
	}
	return &pr, nil
}

// readErr gives a read failure context. A coded cause keeps its code; any
// other is repo_unreadable.
func readErr(what string, err error) error {
	return op.Wrap(op.CodeRepoUnreadable, fmt.Errorf("%s: %w", what, err))
}

func isStatus(err error, status int) bool {
	var h *api.HTTPError
	return errors.As(err, &h) && h.StatusCode == status
}

// escapeRef escapes each segment of a branch name for a URL path.
func escapeRef(branch string) string {
	segs := strings.Split(branch, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// Package workflowpr writes a rendered Nibfin workflow set to one fixed
// branch and keeps one PR open from it into the default branch. A rerun finds
// the branch and the PR it wrote before: a current branch is not rewritten
// and an open PR is kept, not duplicated. upgrade and init share it.
package workflowpr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
)

// ScopeHint is added to a write GitHub refused with 403, 404 or 422.
const ScopeHint = "writing .github/workflows needs the workflow scope; run gh auth refresh -s workflow"

// PullRequest is the part of a GitHub PR the operations read.
type PullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

// Repo is a repository on its host's client.
type Repo struct {
	Client templates.RESTClient
	// Name is HOST/OWNER/REPO, used in messages.
	Name  string
	Owner string
	// Base is repos/OWNER/REPO.
	Base string
}

// NewRepo parses HOST/OWNER/REPO and builds the client for HOST with
// newClient.
func NewRepo(name string, newClient func(host string) (templates.RESTClient, error)) (Repo, string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Repo{}, "", op.Errorf(op.CodeInvalidRepo, "repository %q is not HOST/OWNER/REPO", name)
	}
	c, err := newClient(parts[0])
	if err != nil {
		return Repo{}, "", op.Wrap(op.CodeRepoUnreadable, fmt.Errorf("connect to %s: %w", parts[0], err))
	}
	return Repo{Client: c, Name: name, Owner: parts[1], Base: "repos/" + url.PathEscape(parts[1]) + "/" + url.PathEscape(parts[2])}, parts[0], nil
}

// ReadFiles reads each path at ref ("" is the default branch, named where in
// errors). A missing file has no entry.
func (r Repo) ReadFiles(ctx context.Context, paths []string, ref, where string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, p := range paths {
		data, found, err := templates.ReadFile(ctx, r.Client, r.Name, p, ref)
		if err != nil {
			if ref != "" {
				where = ref
			}
			return nil, ReadErr(fmt.Sprintf("read %s at %s of %s", p, where, r.Name), err)
		}
		if found {
			out[p] = data
		}
	}
	return out, nil
}

// RefSHA returns the commit a branch points to, or "" when it does not
// exist.
func (r Repo) RefSHA(ctx context.Context, branch string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err := r.Client.DoWithContext(ctx, http.MethodGet, r.Base+"/git/ref/heads/"+EscapeRef(branch), nil, &ref)
	if IsStatus(err, http.StatusNotFound) {
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

// Write sends one changing request to Base+p. A failure is
// repo_write_failed with go-gh's message; a 403, 404 or 422 also names the
// workflow scope.
func (r Repo) Write(ctx context.Context, method, p string, body, response any, what string) error {
	b, err := json.Marshal(body)
	if err != nil {
		return op.Wrap(op.CodeRepoWriteFailed, err)
	}
	err = r.Client.DoWithContext(ctx, method, r.Base+p, bytes.NewReader(b), response)
	if err == nil {
		return nil
	}
	if IsStatus(err, http.StatusForbidden) || IsStatus(err, http.StatusNotFound) || IsStatus(err, http.StatusUnprocessableEntity) {
		return op.Errorf(op.CodeRepoWriteFailed, "%s of %s: %w; %s", what, r.Name, err, ScopeHint)
	}
	return op.Errorf(op.CodeRepoWriteFailed, "%s of %s: %w", what, r.Name, err)
}

// PR is one fixed-branch PR of a rendered set. Plan fills the inputs and
// calls Read; Apply writes; ReadBack checks.
type PR struct {
	Repo          Repo
	DefaultBranch string
	Branch        string
	// Files is the rendered set; Paths are the paths compared and written,
	// in a fixed order. A path without a file is written empty.
	Files map[string][]byte
	Paths []string
	// Message is the commit message; Title and Body are the PR's.
	Message, Title, Body string

	// BranchSHA is the head of Branch, "" when it does not exist.
	BranchSHA string
	// BranchCurrent is true when Branch holds Files.
	BranchCurrent bool
	// Open is the open PR from Branch, or nil.
	Open *PullRequest
	// Written is the commit Apply wrote, "" when none.
	Written string
}

// Read reads Branch, its files and the open PR from it.
func (w *PR) Read(ctx context.Context) error {
	sha, err := w.Repo.RefSHA(ctx, w.Branch)
	if err != nil {
		return ReadErr(fmt.Sprintf("read the branch %s of %s", w.Branch, w.Repo.Name), err)
	}
	w.BranchSHA = sha
	w.BranchCurrent = false
	if sha != "" {
		onBranch, err := w.Repo.ReadFiles(ctx, w.Paths, sha, w.Branch)
		if err != nil {
			return err
		}
		w.BranchCurrent = true
		for _, p := range w.Paths {
			if !bytes.Equal(onBranch[p], w.Files[p]) {
				w.BranchCurrent = false
			}
		}
	}
	w.Open, err = w.openPR(ctx)
	return err
}

// Done reports whether the branch holds the set and its PR is open.
func (w *PR) Done() bool { return w.BranchCurrent && w.Open != nil }

// Steps are the plan steps after the file steps: the branch write, when
// needed, and the PR.
func (w *PR) Steps() []op.Step {
	var steps []op.Step
	switch {
	case w.BranchCurrent:
	case w.BranchSHA != "":
		steps = append(steps, op.Step{Action: "write branch", Target: w.Branch, Detail: "replaces earlier contents of that branch"})
	default:
		steps = append(steps, op.Step{Action: "write branch", Target: w.Branch, Detail: "new branch from " + w.DefaultBranch})
	}
	if w.Open != nil {
		steps = append(steps, op.Step{Action: "keep PR", Target: fmt.Sprintf("#%d", w.Open.Number), Detail: w.Open.HTMLURL})
	} else {
		steps = append(steps, op.Step{Action: "open PR", Target: w.Branch + " into " + w.DefaultBranch})
	}
	return steps
}

// openPR returns the open PR from Branch, or nil.
func (w *PR) openPR(ctx context.Context) (*PullRequest, error) {
	q := url.Values{"head": {w.Repo.Owner + ":" + w.Branch}, "state": {"open"}}
	var prs []PullRequest
	if err := w.Repo.Client.DoWithContext(ctx, http.MethodGet, w.Repo.Base+"/pulls?"+q.Encode(), nil, &prs); err != nil {
		return nil, ReadErr(fmt.Sprintf("read the open PRs from %s of %s", w.Branch, w.Repo.Name), err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

// Apply writes one commit on the default branch's head to Branch, unless
// it is current, and opens the PR, unless one is open.
func (w *PR) Apply(ctx context.Context) error {
	w.Written = ""
	r := w.Repo
	if !w.BranchCurrent {
		head, err := r.RefSHA(ctx, w.DefaultBranch)
		if err == nil && head == "" {
			err = fmt.Errorf("the branch %s does not exist", w.DefaultBranch)
		}
		if err != nil {
			return ReadErr(fmt.Sprintf("read the branch %s of %s", w.DefaultBranch, r.Name), err)
		}
		var commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		}
		if err := r.Client.DoWithContext(ctx, http.MethodGet, r.Base+"/git/commits/"+url.PathEscape(head), nil, &commit); err != nil {
			return ReadErr(fmt.Sprintf("read the commit %s of %s", head, r.Name), err)
		}

		type entry struct {
			Path    string `json:"path"`
			Mode    string `json:"mode"`
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		entries := []entry{}
		for _, p := range w.Paths {
			entries = append(entries, entry{Path: p, Mode: "100644", Type: "blob", Content: string(w.Files[p])})
		}
		var tree struct {
			SHA string `json:"sha"`
		}
		if err := r.Write(ctx, http.MethodPost, "/git/trees", map[string]any{"base_tree": commit.Tree.SHA, "tree": entries}, &tree, "write the tree"); err != nil {
			return err
		}
		var created struct {
			SHA string `json:"sha"`
		}
		if err := r.Write(ctx, http.MethodPost, "/git/commits", map[string]any{"message": w.Message, "tree": tree.SHA, "parents": []string{head}}, &created, "write the commit"); err != nil {
			return err
		}
		if created.SHA == "" {
			return op.Errorf(op.CodeRepoWriteFailed, "write the commit to %s: GitHub returned no commit", r.Name)
		}
		if w.BranchSHA != "" {
			err = r.Write(ctx, http.MethodPatch, "/git/refs/heads/"+EscapeRef(w.Branch), map[string]any{"sha": created.SHA, "force": true}, nil, "update the branch "+w.Branch)
		} else {
			err = r.Write(ctx, http.MethodPost, "/git/refs", map[string]any{"ref": "refs/heads/" + w.Branch, "sha": created.SHA}, nil, "create the branch "+w.Branch)
		}
		if err != nil {
			return err
		}
		w.Written = created.SHA
	}
	if w.Open == nil {
		var pr PullRequest
		body := map[string]any{"title": w.Title, "head": w.Branch, "base": w.DefaultBranch, "body": w.Body}
		if err := r.Write(ctx, http.MethodPost, "/pulls", body, &pr, "open the PR from "+w.Branch); err != nil {
			return err
		}
		w.Open = &pr
	}
	return nil
}

// ReadBack re-reads the open PR from Branch and checks that its head is the
// commit Apply wrote, or the branch head Read found.
func (w *PR) ReadBack(ctx context.Context) (*PullRequest, error) {
	pr, err := w.readPR(ctx)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, op.Errorf(op.CodeRepoWriteFailed, "no open PR from %s in %s", w.Branch, w.Repo.Name)
	}
	want := w.Written
	if want == "" {
		want = w.BranchSHA
	}
	if pr.Head.SHA != want {
		return nil, op.Errorf(op.CodeRepoWriteFailed, "PR #%d of %s has head %s, want %s", pr.Number, w.Repo.Name, pr.Head.SHA, want)
	}
	return pr, nil
}

// readPR re-reads the open PR from Branch: by number when Read found it or
// Apply opened it, since a just-opened PR may not be listed yet.
func (w *PR) readPR(ctx context.Context) (*PullRequest, error) {
	if w.Open == nil {
		return w.openPR(ctx)
	}
	var pr PullRequest
	if err := w.Repo.Client.DoWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/pulls/%d", w.Repo.Base, w.Open.Number), nil, &pr); err != nil {
		return nil, ReadErr(fmt.Sprintf("read PR #%d of %s", w.Open.Number, w.Repo.Name), err)
	}
	if pr.State != "open" || pr.Head.Ref != w.Branch {
		return nil, nil
	}
	return &pr, nil
}

// ReadErr gives a read failure context. A coded cause keeps its code; any
// other is repo_unreadable.
func ReadErr(what string, err error) error {
	return op.Wrap(op.CodeRepoUnreadable, fmt.Errorf("%s: %w", what, err))
}

// IsStatus reports whether err is a GitHub HTTP error with status.
func IsStatus(err error, status int) bool {
	var h *api.HTTPError
	return errors.As(err, &h) && h.StatusCode == status
}

// EscapeRef escapes each segment of a branch name for a URL path.
func EscapeRef(branch string) string {
	segs := strings.Split(branch, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

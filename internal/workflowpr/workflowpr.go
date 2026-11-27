// Package workflowpr writes a rendered Nibfin workflow set to one fixed
// branch and keeps one PR open from it into the default branch. A rerun finds
// the branch and the PR it wrote before: a current branch (one commit holding
// the set on the default branch's head) is not rewritten and an open PR is
// kept, not duplicated. upgrade and init share it.
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

	"github.com/taco3064/gh-nibfin/internal/ghretry"
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

// Commit reads one commit object: its tree and its parents.
func (r Repo) Commit(ctx context.Context, sha string) (tree string, parents []string, err error) {
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := r.Client.DoWithContext(ctx, http.MethodGet, r.Base+"/git/commits/"+url.PathEscape(sha), nil, &commit); err != nil {
		return "", nil, ReadErr(fmt.Sprintf("read the commit %s of %s", sha, r.Name), err)
	}
	for _, p := range commit.Parents {
		parents = append(parents, p.SHA)
	}
	return commit.Tree.SHA, parents, nil
}

// Write sends one changing request to Base+p. A failure is
// repo_write_failed with go-gh's message; a 403, 404 or 422 also names the
// workflow scope.
func (r Repo) Write(ctx context.Context, method, p string, body, response any, what string) error {
	return r.WriteOnce(ctx, method, p, body, response, what, nil)
}

// WriteOnce is Write for a create: with done set, a failed send is not
// repeated blindly; done re-reads whether it took effect first (ghretry.Do).
func (r Repo) WriteOnce(ctx context.Context, method, p string, body, response any, what string, done func(context.Context) (bool, error)) error {
	b, err := json.Marshal(body)
	if err != nil {
		return op.Wrap(op.CodeRepoWriteFailed, err)
	}
	send := func(ctx context.Context) error {
		return r.Client.DoWithContext(ctx, method, r.Base+p, bytes.NewReader(b), response)
	}
	if done == nil {
		err = send(ctx)
	} else {
		err = ghretry.Do(ctx, method, r.Base+p, send, done)
	}
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
	// BranchCurrent is true when Branch holds Files as one commit on the
	// default branch's head. A branch based on an older head is not current:
	// a rebase merge replays only its own commit and would leave the files
	// that commit does not change behind.
	BranchCurrent bool
	// Open is the open PR from Branch, or nil.
	Open *PullRequest
	// Written is the commit Apply wrote, "" when none.
	Written string
	// OnDefault are the files of Paths on the default branch, and FileDetail
	// the detail of a changed file item; Steps reads both.
	OnDefault  map[string][]byte
	FileDetail string
}

// Read reads Branch, its files, whether it is one commit on the default
// branch's head, and the open PR from it.
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
		equal := true
		for _, p := range w.Paths {
			if !bytes.Equal(onBranch[p], w.Files[p]) {
				equal = false
			}
		}
		// A branch whose files differ is rewritten anyway; only an equal one
		// needs its base checked.
		if equal {
			head, err := w.defaultHead(ctx)
			if err != nil {
				return err
			}
			_, parents, err := w.Repo.Commit(ctx, sha)
			if err != nil {
				return err
			}
			w.BranchCurrent = len(parents) == 1 && parents[0] == head
		}
	}
	w.Open, err = w.openPR(ctx)
	return err
}

// Done reports whether the branch holds the set as one commit on the
// default branch's head and its PR is open.
func (w *PR) Done() bool { return w.BranchCurrent && w.Open != nil }

// PRItem is the ID of the PR item.
const PRItem = "pr"

// FileItem and BranchItem are the IDs of a file item and the branch item.
func FileItem(p string) string        { return "file:" + p }
func BranchItem(branch string) string { return "branch:" + branch }

func fileAction(onDefault []byte) string {
	if onDefault == nil {
		return "add"
	}
	return "update"
}

// Steps are the plan items: one per path, the branch and the PR. A file
// already current on the default branch, or already on a current branch,
// needs no change; so does a current branch and an open PR.
func (w *PR) Steps() []op.Step {
	var steps []op.Step
	for _, p := range w.Paths {
		def, ok := w.OnDefault[p]
		s := op.Step{ID: FileItem(p), Action: fileAction(def), Target: p, Detail: w.FileDetail, Current: "absent", Next: "current"}
		if ok {
			s.Current = "differs"
		}
		switch {
		case ok && bytes.Equal(def, w.Files[p]):
			s.Action, s.Current, s.NoChange, s.Detail = "keep", "current", true, "already current on "+w.DefaultBranch
		case w.BranchCurrent:
			s.NoChange, s.Detail = true, "already on "+w.Branch
		}
		steps = append(steps, s)
	}
	b := op.Step{ID: BranchItem(w.Branch), Action: "write branch", Target: w.Branch, Next: "holds the set"}
	switch {
	case w.BranchCurrent:
		b.Current, b.NoChange, b.Detail = "holds the set", true, "already holds the set"
	case w.BranchSHA != "":
		b.Current, b.Detail = "differs", "replaces earlier contents of that branch"
	default:
		b.Current, b.Detail = "absent", "new branch from "+w.DefaultBranch
	}
	steps = append(steps, b)
	if w.Open != nil {
		steps = append(steps, op.Step{ID: PRItem, Action: "keep PR", Target: fmt.Sprintf("#%d", w.Open.Number), Detail: w.Open.HTMLURL, Current: "open", Next: "open", NoChange: true})
	} else {
		steps = append(steps, op.Step{ID: PRItem, Action: "open PR", Target: w.Branch + " into " + w.DefaultBranch, Current: "none", Next: "open"})
	}
	return steps
}

// ReadBackItems reads the PR back as ReadBack does and records the items
// of steps (the plan's Steps) in c: each confirmed when the open PR's head
// is the expected commit, none when it is not.
func (w *PR) ReadBackItems(ctx context.Context, c *op.Checks, steps []op.Step) (*PullRequest, error) {
	c.Expect(steps...)
	pr, err := w.ReadBack(ctx)
	if err != nil {
		for _, s := range steps {
			c.Set(s, false, "", op.AsError(err).Error())
		}
		return nil, err
	}
	for _, s := range steps {
		state := fmt.Sprintf("in PR #%d", pr.Number)
		switch s.ID {
		case BranchItem(w.Branch):
			state = "head " + pr.Head.SHA
		case PRItem:
			state = "open: " + pr.HTMLURL
		}
		c.Set(s, true, state, "")
	}
	return pr, nil
}

// KeptSteps are the items of a set that needs no branch and no PR: each
// path kept as it is on the default branch, and no branch write or PR, for
// the reason why.
func KeptSteps(paths []string, branch, why string) []op.Step {
	var steps []op.Step
	for _, p := range paths {
		steps = append(steps, op.Step{ID: FileItem(p), Action: "keep", Target: p, Detail: why, Current: "current", Next: "current", NoChange: true})
	}
	return append(steps,
		op.Step{ID: BranchItem(branch), Action: "write branch", Target: branch, Detail: "not needed: " + why, NoChange: true},
		op.Step{ID: PRItem, Action: "open PR", Target: branch, Detail: "not needed: " + why, NoChange: true},
	)
}

// ReadBackKept re-reads the kept files on the default branch and records
// the items of steps (from KeptSteps) in c. A file is confirmed when it is
// present and, when want holds it, equal to it. The branch and PR items are
// confirmed as not needed.
func (r Repo) ReadBackKept(ctx context.Context, c *op.Checks, steps []op.Step, defaultBranch string, want map[string][]byte) error {
	c.Expect(steps...)
	var paths []string
	for _, s := range steps {
		if s.ID == FileItem(s.Target) {
			paths = append(paths, s.Target)
		}
	}
	got, err := r.ReadFiles(ctx, paths, "", defaultBranch)
	if err != nil {
		return err
	}
	for _, s := range steps {
		if s.ID != FileItem(s.Target) {
			c.Set(s, true, "not needed", "")
			continue
		}
		data, ok := got[s.Target]
		w, check := want[s.Target]
		switch {
		case !ok:
			c.Set(s, false, "absent", "absent on "+defaultBranch)
		case check && !bytes.Equal(data, w):
			c.Set(s, false, "differs", "differs on "+defaultBranch)
		default:
			c.Set(s, true, "current on "+defaultBranch, "")
		}
	}
	return nil
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
// it is current (holds the set as one commit on that head), and opens the
// PR, unless one is open.
func (w *PR) Apply(ctx context.Context) error {
	w.Written = ""
	steps := w.Steps()
	if !w.BranchCurrent {
		// The changed files and the branch are written as one commit.
		var items []op.Step
		for _, s := range steps {
			if s.ID != PRItem && !s.NoChange {
				items = append(items, s)
			}
		}
		if err := op.TrackAll(ctx, items, func() error { return w.writeBranch(ctx) }); err != nil {
			return err
		}
	}
	if w.Open == nil {
		return op.Track(ctx, steps[len(steps)-1], func() error {
			var pr PullRequest
			body := map[string]any{"title": w.Title, "head": w.Branch, "base": w.DefaultBranch, "body": w.Body}
			// A PR whose answer was lost is found by its head before sending again.
			found := func(ctx context.Context) (bool, error) {
				var open []PullRequest
				q := w.Repo.Base + "/pulls?state=open&head=" + url.QueryEscape(w.Repo.Owner+":"+w.Branch)
				if err := w.Repo.Client.DoWithContext(ctx, http.MethodGet, q, nil, &open); err != nil || len(open) == 0 {
					return false, err
				}
				pr = open[0]
				return true, nil
			}
			if err := w.Repo.WriteOnce(ctx, http.MethodPost, "/pulls", body, &pr, "open the PR from "+w.Branch, found); err != nil {
				return err
			}
			w.Open = &pr
			return nil
		})
	}
	return nil
}

// defaultHead returns the head of the default branch; a missing branch is an
// error.
func (w *PR) defaultHead(ctx context.Context) (string, error) {
	head, err := w.Repo.RefSHA(ctx, w.DefaultBranch)
	if err == nil && head == "" {
		err = fmt.Errorf("the branch %s does not exist", w.DefaultBranch)
	}
	if err != nil {
		return "", ReadErr(fmt.Sprintf("read the branch %s of %s", w.DefaultBranch, w.Repo.Name), err)
	}
	return head, nil
}

// writeBranch writes one commit with every path on the default branch's
// head and points Branch to it.
func (w *PR) writeBranch(ctx context.Context) error {
	r := w.Repo
	head, err := w.defaultHead(ctx)
	if err != nil {
		return err
	}
	baseTree, _, err := r.Commit(ctx, head)
	if err != nil {
		return err
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
	if err := r.Write(ctx, http.MethodPost, "/git/trees", map[string]any{"base_tree": baseTree, "tree": entries}, &tree, "write the tree"); err != nil {
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
		// A branch whose answer was lost is found at the commit before sending again.
		found := func(ctx context.Context) (bool, error) {
			sha, err := r.RefSHA(ctx, w.Branch)
			return err == nil && sha == created.SHA, err
		}
		err = r.WriteOnce(ctx, http.MethodPost, "/git/refs", map[string]any{"ref": "refs/heads/" + w.Branch, "sha": created.SHA}, nil, "create the branch "+w.Branch, found)
	}
	if err != nil {
		return err
	}
	w.Written = created.SHA
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

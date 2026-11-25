// Package status is the status operation of the shared operation layer: a
// read-only report of the runner host, the providers' logins and the Issues
// Nibfin works on. Labels, comments, workflow files and runs are reported as
// GitHub returns them; nothing is derived, diagnosed or repaired. It lives
// outside internal/op because image and templates import op for their error
// codes.
package status

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"

	"github.com/taco3064/gh-nibfin/internal/image"
	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
)

// Tests replace these.
var (
	readHost    = op.ReadHost
	latestImage = image.Latest
	// newREST returns a client for host that carries the current gh login;
	// only go-gh handles its token.
	newREST = func(host string) (templates.RESTClient, error) {
		return api.NewRESTClient(api.ClientOptions{Host: host})
	}
	defaultHost = auth.DefaultHost
)

const (
	perPage = 100
	// maxResults is the most a search or a run list returns.
	maxResults = 1000

	jobFile   = templates.Dir + "/nibfin-job.yml"
	issueFile = templates.Dir + "/nibfin-issue.yml"

	pendingMarker = "<!-- nibfin:pending -->"
	runPrefix     = "Nibfin · "
)

var (
	imageRefLine = regexp.MustCompile(`(?m)^\s*IMAGE_REF:\s*(\S+)\s*$`)
	imagePattern = regexp.MustCompile(`^ghcr\.io/taco3064/nibfin@sha256:[0-9a-f]{64}$`)
)

// New is the entry point of status. An empty Repo is the person view: the
// current gh user's open scope Issues across repositories. A Repo,
// HOST/OWNER/REPO, is the repository view.
func New(p op.RepoParams) op.Operation { return &status{p: p} }

type status struct {
	p op.RepoParams
}

// LatestImage is the latest attested image, or why it is unknown.
type LatestImage struct {
	Ref   string    `json:"ref,omitempty"`
	Error *op.Error `json:"error,omitempty"`
}

// RepoImage is the image a repository's Nibfin workflows pin.
type RepoImage struct {
	// Set is execution (nibfin-job.yml exists), peer (only
	// nibfin-issue.yml) or none.
	Set string `json:"set,omitempty"`
	// Ref is the pinned image; nil for a peer set, a missing or invalid pin.
	Ref *string `json:"ref"`
	// Latest is whether Ref is the latest image; nil when either is unknown.
	Latest *bool     `json:"latest"`
	Error  *op.Error `json:"error,omitempty"`
}

// Issue is one open Issue as read.
type Issue struct {
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	URL     string   `json:"url"`
	Labels  []string `json:"labels"`
	Pause   bool     `json:"pause"`
	Pending bool     `json:"pending"`
	// PendingComment links the pending comment of the account that added
	// the pending label last.
	PendingComment      string    `json:"pendingComment,omitempty"`
	PendingCommentError *op.Error `json:"pendingCommentError,omitempty"`
	// CreatedAt is GitHub's creation time of the Issue (RFC 3339).
	CreatedAt string `json:"createdAt"`
	// BlockedBy is every Issue GitHub lists as blocking this one, open and
	// closed, in any repository; BlockedByError says why it is not known.
	BlockedBy      []Blocker `json:"blockedBy"`
	BlockedByError *op.Error `json:"blockedByError,omitempty"`
	// Runs are the Nibfin runs of the Issue in its own repository and in the
	// execution repository, newest first. RunsError says why some may be
	// missing; RunsTruncated that a scan reached GitHub's 1,000-run window.
	Runs          []Run     `json:"runs"`
	RunsError     *op.Error `json:"runsError,omitempty"`
	RunsTruncated bool      `json:"runsTruncated,omitempty"`
}

// Blocker is an Issue that blocks another, as GitHub lists it.
type Blocker struct {
	// Repo is OWNER/REPO.
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	// State is GitHub's OPEN or CLOSED.
	State string `json:"state"`
	URL   string `json:"url"`
}

// Run is one Actions run whose title names an Issue.
type Run struct {
	// Repo is the OWNER/REPO the run belongs to.
	Repo string `json:"repo"`
	// Name is the run's title (display_title).
	Name string `json:"name"`
	// Workflow is the workflow's name.
	Workflow   string  `json:"workflow"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
	CreatedAt  string  `json:"createdAt"`
	URL        string  `json:"url"`
}

// NibfinRepo is a repository of the current gh account whose default branch
// has nibfin-issue.yml.
type NibfinRepo struct {
	// Repo is OWNER/REPO.
	Repo       string `json:"repo"`
	URL        string `json:"url"`
	Visibility string `json:"visibility"`
	// Permission is the account's permission as GitHub names it; nil when
	// GitHub returns none.
	Permission *string `json:"permission"`
	// Execution is whether this host's Runners serve the repository; nil
	// when the host's runners cannot be read.
	Execution *bool `json:"execution"`
}

// RepoIssues is one repository of the person view.
type RepoIssues struct {
	Repo   string    `json:"repo"`
	Image  RepoImage `json:"image"`
	Issues []Issue   `json:"issues"`
	// Permission is the current gh account's permission on Repo as GitHub
	// names it; nil when unknown, with PermissionError when unread.
	Permission      *string   `json:"permission"`
	PermissionError *op.Error `json:"permissionError,omitempty"`
}

// PullRequest names a pull request.
type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// NibfinRun is whether a Nibfin run succeeded after the init PR merged.
type NibfinRun struct {
	// Initialized is true when the default branch's history has a
	// nibfin-issue.yml that gh nibfin generated.
	Initialized bool         `json:"initialized"`
	InitCommit  string       `json:"initCommit,omitempty"`
	InitPR      *PullRequest `json:"initPR,omitempty"`
	MergedAt    string       `json:"mergedAt,omitempty"`
	Succeeded   bool         `json:"succeeded"`
	Run         string       `json:"run,omitempty"`
	At          string       `json:"at,omitempty"`
	Error       *op.Error    `json:"error,omitempty"`
}

func (s *status) Plan(context.Context) (op.Plan, error) {
	plan := op.Plan{Operation: "status", Repo: s.p.Repo, Steps: []op.Step{}, ReadOnly: true}
	if s.p.Repo != "" {
		if _, err := splitRepo(s.p.Repo); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

// Apply does nothing: status only reads.
func (s *status) Apply(context.Context, op.Plan) error { return nil }

func (s *status) ReadBack(ctx context.Context, _ op.Plan) (op.Result, error) {
	var parts []string
	var host string
	if s.p.Repo != "" {
		var err error
		if parts, err = splitRepo(s.p.Repo); err != nil {
			return nil, err
		}
		host = parts[0]
	} else if host, _ = defaultHost(); host == "" {
		return nil, op.Errorf(op.CodeAuthUnavailable, "gh has no default host; run gh auth login")
	}
	c, err := newREST(host)
	if err != nil {
		return nil, op.Errorf(op.CodeAuthUnavailable, "cannot use the gh login for %s: %v", host, err)
	}
	hr := readHost(ctx)
	r := &reader{c: c, host: host, runners: hr.Runners}

	res := op.Result{"host": hr}
	latest := LatestImage{}
	if ref, err := latestImage(ctx); err != nil {
		latest.Error = op.AsError(err)
	} else {
		latest.Ref = ref
	}
	res["latestImage"] = latest

	if s.p.Repo == "" {
		r.person(ctx, res, latest)
		return res, nil
	}
	repo := parts[1] + "/" + parts[2]
	res["repo"] = s.p.Repo
	res["image"] = r.image(ctx, repo, latest)
	issues, err := r.repoIssues(ctx, repo)
	if err != nil {
		res["issuesError"] = op.AsError(err)
		issues = []Issue{}
	}
	g := newGroup(repo, issues)
	r.links(ctx, []*repoGroup{g})
	res["issues"] = issues
	res["nibfinRun"] = r.nibfinRun(ctx, repo)
	res["permission"] = g.perm
	if g.permErr != nil {
		res["permissionError"] = g.permErr
	}
	r.setNibfinRepos(ctx, res)
	return res, nil
}

// splitRepo checks HOST/OWNER/REPO.
func splitRepo(repo string) ([]string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, op.Errorf(op.CodeInvalidRepo, "repository %q is not HOST/OWNER/REPO", repo)
	}
	return parts, nil
}

// reader reads one GitHub host. Repositories are OWNER/REPO on that host.
type reader struct {
	c    templates.RESTClient
	host string
	// runners is this host's runner report, the source of the execution
	// repository.
	runners op.RunnersReport

	gql    graphQL
	gqlErr *op.Error
}

func (r *reader) get(ctx context.Context, p string, out any) error {
	return r.c.DoWithContext(ctx, http.MethodGet, p, nil, out)
}

func base(repo string) string {
	owner, name, _ := strings.Cut(repo, "/")
	return "repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

// apiErr keeps go-gh's status and message; a token never appears in either.
func apiErr(code, what string, err error) *op.Error {
	var h *api.HTTPError
	if errors.As(err, &h) {
		msg := h.Message
		if msg == "" {
			msg = http.StatusText(h.StatusCode)
		}
		return op.AsError(op.Errorf(code, "%s: HTTP %d: %s", what, h.StatusCode, msg))
	}
	if op.Code(err) != "" {
		return op.AsError(err)
	}
	return op.AsError(op.Errorf(code, "%s: %v", what, err))
}

type labelJSON struct {
	Name string `json:"name"`
}

type issueJSON struct {
	Number        int         `json:"number"`
	Title         string      `json:"title"`
	HTMLURL       string      `json:"html_url"`
	RepositoryURL string      `json:"repository_url"`
	Labels        []labelJSON `json:"labels"`
	PullRequest   *struct{}   `json:"pull_request"`
	CreatedAt     string      `json:"created_at"`
}

func toIssue(j issueJSON) Issue {
	is := Issue{Number: j.Number, Title: j.Title, URL: j.HTMLURL, Labels: []string{},
		CreatedAt: j.CreatedAt, BlockedBy: []Blocker{}, Runs: []Run{}}
	for _, l := range j.Labels {
		is.Labels = append(is.Labels, l.Name)
		is.Pause = is.Pause || strings.EqualFold(l.Name, "pause")
		is.Pending = is.Pending || strings.EqualFold(l.Name, "pending")
	}
	return is
}

// person fills the person view: the open scope Issues assigned to the
// current gh user, found by GitHub's search and grouped by repository.
func (r *reader) person(ctx context.Context, res op.Result, latest LatestImage) {
	res["githubHost"] = r.host
	var user struct {
		Login string `json:"login"`
	}
	if err := r.get(ctx, "user", &user); err != nil {
		res["userError"] = apiErr(op.CodeGitHubAPIFailed, "get the gh login's user", err)
	} else {
		res["user"] = user.Login
	}

	repos := []RepoIssues{}
	items, truncated, err := r.search(ctx)
	if err != nil {
		res["issuesError"] = err
	}
	byRepo := map[string][]Issue{}
	for _, it := range items {
		repo, ok := repoOfURL(it.RepositoryURL)
		if !ok {
			continue
		}
		byRepo[repo] = append(byRepo[repo], r.withPending(ctx, repo, toIssue(it)))
	}
	names := make([]string, 0, len(byRepo))
	for n := range byRepo {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	var groups []*repoGroup
	for _, n := range names {
		issues := byRepo[n]
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })
		repos = append(repos, RepoIssues{Repo: n, Image: r.image(ctx, n, latest), Issues: issues})
		groups = append(groups, newGroup(n, issues))
	}
	r.links(ctx, groups)
	for i, g := range groups {
		repos[i].Permission, repos[i].PermissionError = g.perm, g.permErr
	}
	res["repos"] = repos
	res["truncated"] = truncated
	r.setNibfinRepos(ctx, res)
}

// search pages through GitHub's issue search, up to its 1,000 results.
func (r *reader) search(ctx context.Context) ([]issueJSON, bool, *op.Error) {
	var all []issueJSON
	total := 0
	for page := 1; len(all) < maxResults; page++ {
		q := url.Values{
			"q":        {"is:issue is:open label:scope assignee:@me"},
			"per_page": {fmt.Sprint(perPage)},
			"page":     {fmt.Sprint(page)},
		}
		var body struct {
			TotalCount int         `json:"total_count"`
			Items      []issueJSON `json:"items"`
		}
		if err := r.get(ctx, "search/issues?"+q.Encode(), &body); err != nil {
			return all, total > len(all), apiErr(op.CodeGitHubAPIFailed, "search the open scope Issues assigned to you", err)
		}
		total = body.TotalCount
		all = append(all, body.Items...)
		if len(body.Items) < perPage {
			break
		}
	}
	return all, total > len(all), nil
}

// repoOfURL takes OWNER/REPO from an API repository URL, which ends in
// /repos/OWNER/REPO.
func repoOfURL(s string) (string, bool) {
	u, err := url.Parse(s)
	if err != nil {
		return "", false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	n := len(segs)
	if n < 3 || segs[n-3] != "repos" || segs[n-2] == "" || segs[n-1] == "" {
		return "", false
	}
	return segs[n-2] + "/" + segs[n-1], true
}

// repoIssues lists every open Issue of repo; the Issues list includes PRs,
// which are skipped.
func (r *reader) repoIssues(ctx context.Context, repo string) ([]Issue, error) {
	out := []Issue{}
	for page := 1; ; page++ {
		var items []issueJSON
		p := fmt.Sprintf("%s/issues?state=open&per_page=%d&page=%d", base(repo), perPage, page)
		if err := r.get(ctx, p, &items); err != nil {
			return nil, apiErr(op.CodeGitHubAPIFailed, "list the open Issues of "+repo, err)
		}
		for _, it := range items {
			if it.PullRequest == nil {
				out = append(out, r.withPending(ctx, repo, toIssue(it)))
			}
		}
		if len(items) < perPage {
			return out, nil
		}
	}
}

type eventJSON struct {
	Event     string `json:"event"`
	CreatedAt string `json:"created_at"`
	Actor     *struct {
		ID int64 `json:"id"`
	} `json:"actor"`
	Label *labelJSON `json:"label"`
}

type commentJSON struct {
	ID   int64 `json:"id"`
	User *struct {
		ID int64 `json:"id"`
	} `json:"user"`
	Body      string `json:"body"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
}

// withPending links the pending comment of a pending Issue: the oldest
// comment starting with the pending marker by the account that added the
// pending label last. That is Core's own rule, with Core's account taken
// from the label event. Nothing found is no link and no error.
func (r *reader) withPending(ctx context.Context, repo string, is Issue) Issue {
	if !is.Pending {
		return is
	}
	issue := fmt.Sprintf("%s/issues/%d", base(repo), is.Number)
	what := fmt.Sprintf("read %s#%d", repo, is.Number)

	var actor int64
	var at time.Time
	for page := 1; ; page++ {
		var events []eventJSON
		if err := r.get(ctx, fmt.Sprintf("%s/events?per_page=%d&page=%d", issue, perPage, page), &events); err != nil {
			is.PendingCommentError = apiErr(op.CodeGitHubAPIFailed, what+" events", err)
			return is
		}
		for _, e := range events {
			if e.Event != "labeled" || e.Label == nil || !strings.EqualFold(e.Label.Name, "pending") || e.Actor == nil {
				continue
			}
			t, err := time.Parse(time.RFC3339, e.CreatedAt)
			if err != nil {
				continue
			}
			if actor == 0 || !t.Before(at) {
				actor, at = e.Actor.ID, t
			}
		}
		if len(events) < perPage {
			break
		}
	}
	if actor == 0 {
		return is
	}

	var best *commentJSON
	var bestAt time.Time
	for page := 1; ; page++ {
		var comments []commentJSON
		if err := r.get(ctx, fmt.Sprintf("%s/comments?per_page=%d&page=%d", issue, perPage, page), &comments); err != nil {
			is.PendingCommentError = apiErr(op.CodeGitHubAPIFailed, what+" comments", err)
			return is
		}
		for i := range comments {
			c := &comments[i]
			if c.User == nil || c.User.ID != actor || !strings.HasPrefix(c.Body, pendingMarker) {
				continue
			}
			t, err := time.Parse(time.RFC3339, c.CreatedAt)
			if err != nil {
				continue
			}
			if best == nil || t.Before(bestAt) || (t.Equal(bestAt) && c.ID < best.ID) {
				cc := *c
				best, bestAt = &cc, t
			}
		}
		if len(comments) < perPage {
			break
		}
	}
	if best != nil {
		is.PendingComment = best.HTMLURL
	}
	return is
}

// image reads the image repo's workflows pin on the default branch.
func (r *reader) image(ctx context.Context, repo string, latest LatestImage) RepoImage {
	full := r.host + "/" + repo
	fail := func(p string, err error) RepoImage {
		return RepoImage{Error: op.AsError(op.Wrap(op.CodeRepoUnreadable, fmt.Errorf("read %s of %s: %w", p, full, err)))}
	}
	job, found, err := templates.ReadFile(ctx, r.c, full, jobFile, "")
	if err != nil {
		return fail(jobFile, err)
	}
	if !found {
		_, found, err := templates.ReadFile(ctx, r.c, full, issueFile, "")
		if err != nil {
			return fail(issueFile, err)
		}
		if found {
			return RepoImage{Set: "peer"}
		}
		return RepoImage{Set: "none"}
	}
	img := RepoImage{Set: "execution"}
	if m := imageRefLine.FindSubmatch(job); m != nil && imagePattern.Match(m[1]) {
		ref := string(m[1])
		img.Ref = &ref
		if latest.Ref != "" {
			same := ref == latest.Ref
			img.Latest = &same
		}
	}
	return img
}

type commitJSON struct {
	SHA    string `json:"sha"`
	Commit struct {
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// nibfinRun finds the init commit (the oldest commit whose nibfin-issue.yml
// gh nibfin generated), the PR that merged it, and a successful Nibfin run
// created at or after that merge.
func (r *reader) nibfinRun(ctx context.Context, repo string) NibfinRun {
	out := NibfinRun{}
	fail := func(what string, err error) NibfinRun {
		out.Error = apiErr(op.CodeGitHubAPIFailed, what+" of "+repo, err)
		return out
	}
	var commits []commitJSON
	for page := 1; ; page++ {
		var list []commitJSON
		q := url.Values{"path": {issueFile}, "per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(page)}}
		if err := r.get(ctx, base(repo)+"/commits?"+q.Encode(), &list); err != nil {
			return fail("list the commits of "+issueFile, err)
		}
		commits = append(commits, list...)
		if len(list) < perPage {
			break
		}
	}
	type dated struct {
		sha string
		at  time.Time
	}
	var ds []dated
	for _, c := range commits {
		if t, err := time.Parse(time.RFC3339, c.Commit.Committer.Date); err == nil && c.SHA != "" {
			ds = append(ds, dated{c.SHA, t})
		}
	}
	sort.SliceStable(ds, func(i, j int) bool { return ds[i].at.Before(ds[j].at) })
	var init *dated
	for i := range ds {
		data, found, err := templates.ReadFile(ctx, r.c, r.host+"/"+repo, issueFile, ds[i].sha)
		if err != nil {
			return fail("read "+issueFile+" at "+ds[i].sha, err)
		}
		if found && strings.HasPrefix(string(data), templates.GeneratedPrefix) {
			init = &ds[i]
			break
		}
	}
	if init == nil {
		return out
	}
	out.Initialized = true
	out.InitCommit = init.sha

	merged := init.at
	var prs []struct {
		Number   int     `json:"number"`
		HTMLURL  string  `json:"html_url"`
		MergedAt *string `json:"merged_at"`
	}
	if err := r.get(ctx, base(repo)+"/commits/"+url.PathEscape(init.sha)+"/pulls", &prs); err != nil {
		return fail("list the PRs of "+init.sha, err)
	}
	for _, pr := range prs {
		if pr.MergedAt == nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339, *pr.MergedAt); err == nil {
			out.InitPR = &PullRequest{Number: pr.Number, URL: pr.HTMLURL}
			merged = t
			break
		}
	}
	out.MergedAt = merged.UTC().Format(time.RFC3339)

	created := ">=" + merged.UTC().Format("2006-01-02T15:04:05") + "+00:00"
	seen := 0
	for page := 1; seen < maxResults; page++ {
		q := url.Values{"status": {"success"}, "created": {created}, "per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(page)}}
		var body struct {
			Runs []struct {
				Name      string `json:"name"`
				HTMLURL   string `json:"html_url"`
				CreatedAt string `json:"created_at"`
			} `json:"workflow_runs"`
		}
		if err := r.get(ctx, base(repo)+"/actions/runs?"+q.Encode(), &body); err != nil {
			return fail("list the successful runs", err)
		}
		for _, run := range body.Runs {
			t, err := time.Parse(time.RFC3339, run.CreatedAt)
			if err != nil || t.Before(merged) || !strings.HasPrefix(run.Name, runPrefix) {
				continue
			}
			out.Succeeded, out.Run, out.At = true, run.HTMLURL, run.CreatedAt
			return out
		}
		seen += len(body.Runs)
		if len(body.Runs) < perPage {
			break
		}
	}
	return out
}

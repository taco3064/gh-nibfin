// Package status is the status operation of the shared operation layer: a
// read-only report of the runner host, the providers' logins and the Issues
// Nibfin works on. Labels, comments, workflow files and runs are reported as
// GitHub returns them, and the repair state of a pending Issue by Core's
// reader rule (internal/repair); nothing is diagnosed or repaired. It lives
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
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
	"github.com/taco3064/gh-nibfin/internal/image"
	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/repair"
	"github.com/taco3064/gh-nibfin/internal/templates"
)

// Tests replace these.
var (
	readHost    = op.ReadHost
	latestImage = image.Latest
	// newREST returns a client for host that carries the current gh login;
	// only go-gh handles its token.
	newREST = func(host string) (templates.RESTClient, error) {
		return api.NewRESTClient(api.ClientOptions{Host: host, Transport: ghretry.Transport(nil)})
	}
	defaultHost = auth.DefaultHost
)

const (
	perPage = 100
	// maxResults is the most a search or a run list returns.
	maxResults = 1000

	jobFile   = templates.Dir + "/nibfin-job.yml"
	issueFile = templates.Dir + "/nibfin-issue.yml"

	runPrefix = "Nibfin · "
)

var (
	imageRefLine = regexp.MustCompile(`(?m)^\s*IMAGE_REF:\s*(\S+)\s*$`)
	imagePattern = regexp.MustCompile(`^ghcr\.io/taco3064/nibfin@sha256:[0-9a-f]{64}$`)
)

// New is the entry point of status. An empty Repo is the person view: the
// current gh user's open scope or request Issues across repositories. A Repo,
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
	// Repair is the repair state of a pending Issue whose events and
	// comments were read: what Core's repair stage says about the stop.
	Repair *Repair `json:"repair,omitempty"`
	// NeedsPerson are the parts of a repair decision Core applied without
	// them, left for a person (R4.51), while Core's choice comment lists
	// them.
	NeedsPerson *NeedsPerson `json:"needsPerson,omitempty"`
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

	// comments is GitHub's comment count, which says whether a choice
	// comment can exist; it is not printed.
	comments int
}

// NeedsPerson is what Core's choice comment lists under "Not applied":
// the parts of a decision on a repair record that Core did not apply
// itself (such as a label, CI or closing the Issue) and leaves for a
// person.
type NeedsPerson struct {
	// Comment links Core's choice comment.
	Comment string `json:"comment"`
	// Items are the parts, one line each, as Core wrote them.
	Items []string `json:"items"`
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

// NibfinRun is whether a Nibfin run succeeded after nibfin-issue.yml last
// changed on the default branch.
type NibfinRun struct {
	// UsesNibfin is true when the default branch has nibfin-issue.yml,
	// whether gh nibfin generated it or the repository maintains it.
	UsesNibfin bool `json:"usesNibfin"`
	// Commit is the newest commit that changed nibfin-issue.yml on the
	// default branch, and PR the merged PR of that commit, when there is
	// one.
	Commit string       `json:"commit,omitempty"`
	PR     *PullRequest `json:"pr,omitempty"`
	// ChangedAt is PR's merge time, or else Commit's committer time.
	ChangedAt string `json:"changedAt,omitempty"`
	// Succeeded is true when a Nibfin · run succeeded at or after
	// ChangedAt; Run is that run and At its creation time.
	Succeeded bool      `json:"succeeded"`
	Run       string    `json:"run,omitempty"`
	At        string    `json:"at,omitempty"`
	Error     *op.Error `json:"error,omitempty"`
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
	r := &reader{c: c, host: host}

	var view builder
	var pr *progress
	var github func(wait func() op.RunnersReport)
	if s.p.Repo == "" {
		d := &personRead{ghHost: host}
		pr = newProgress(ctx, d.result, "host", "latestImage", "user", "repos", "pendingComment", "image",
			"blockedBy", "permission", "nibfinRepos", "runs")
		github = func(wait func() op.RunnersReport) { r.person(ctx, d, pr, wait) }
		view = func(h *op.HostReport, l *LatestImage) op.Result { return d.result(h, l) }
	} else {
		d := &repoRead{ghHost: host, repo: s.p.Repo}
		pr = newProgress(ctx, d.result, "host", "latestImage", "issues", "pendingComment", "blockedBy",
			"permission", "image", "nibfinRun", "nibfinRepos", "runs")
		repo := parts[1] + "/" + parts[2]
		github = func(wait func() op.RunnersReport) { r.repoView(ctx, repo, d, pr, wait) }
		view = func(h *op.HostReport, l *LatestImage) op.Result { return d.result(h, l) }
	}

	// The host and the latest image are read beside the GitHub reads, which
	// stay serial in this goroutine; only the runs and the execution flag
	// wait for the host.
	var hr op.HostReport
	var latest LatestImage
	hostDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer close(hostDone)
		hr = readHost(ctx)
		pr.setHost(hr)
	}()
	go func() {
		defer wg.Done()
		if ref, err := latestImage(ctx); err != nil {
			latest.Error = op.AsError(err)
		} else {
			latest.Ref = ref
		}
		pr.setLatest(latest)
	}()
	github(func() op.RunnersReport { <-hostDone; return hr.Runners })
	wg.Wait()
	return view(&hr, &latest), nil
}

// repoView reads the GitHub part of the repository view into d, the
// sections the page shows first before the others.
func (r *reader) repoView(ctx context.Context, repo string, d *repoRead, pr *progress, wait func() op.RunnersReport) {
	issues, err := r.repoIssues(ctx, repo)
	if err != nil {
		d.issuesErr = op.AsError(err)
		issues = []Issue{}
	}
	d.issues, d.listed = issues, true
	pr.step(d.clone, "issues")

	for i := range issues {
		issues[i] = r.withPending(ctx, repo, issues[i])
	}
	pr.step(d.clone, "pendingComment")

	g := newGroup(repo, issues)
	r.issueGraph(ctx, []*repoGroup{g})
	d.perm, d.permErr, d.permRead = g.perm, g.permErr, true
	pr.step(d.clone, "blockedBy", "permission")

	img := r.image(ctx, repo, LatestImage{})
	d.image = &img
	pr.step(d.clone, "image")

	run := r.nibfinRun(ctx, repo)
	d.run = &run
	pr.step(d.clone, "nibfinRun")

	d.repos, d.reposErr = r.nibfinRepos(ctx)
	d.reposRead = true
	pr.step(d.clone, "nibfinRepos")

	r.runners = wait()
	r.issueRuns(ctx, []*repoGroup{g})
	pr.step(d.clone, "runs")
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
	Comments      int         `json:"comments"`
}

func toIssue(j issueJSON) Issue {
	is := Issue{Number: j.Number, Title: j.Title, URL: j.HTMLURL, Labels: []string{},
		CreatedAt: j.CreatedAt, BlockedBy: []Blocker{}, Runs: []Run{}, comments: j.Comments}
	for _, l := range j.Labels {
		is.Labels = append(is.Labels, l.Name)
		is.Pause = is.Pause || strings.EqualFold(l.Name, "pause")
		is.Pending = is.Pending || strings.EqualFold(l.Name, "pending")
	}
	return is
}

// person reads the person view into d: the open scope or request Issues
// assigned to the current gh user, found by GitHub's search (label:scope,request
// matches either label) and grouped by repository.
// The Issues come first; their other sections follow.
func (r *reader) person(ctx context.Context, d *personRead, pr *progress, wait func() op.RunnersReport) {
	var user struct {
		Login string `json:"login"`
	}
	if err := r.get(ctx, "user", &user); err != nil {
		d.userErr = apiErr(op.CodeGitHubAPIFailed, "get the gh login's user", err)
	} else {
		d.user = user.Login
	}
	d.userRead = true
	pr.step(d.clone, "user")

	items, truncated, err := r.search(ctx)
	if err != nil {
		d.issuesErr = err
	}
	byRepo := map[string][]Issue{}
	for _, it := range items {
		repo, ok := repoOfURL(it.RepositoryURL)
		if !ok {
			continue
		}
		byRepo[repo] = append(byRepo[repo], toIssue(it))
	}
	names := make([]string, 0, len(byRepo))
	for n := range byRepo {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	repos := []RepoIssues{}
	var groups []*repoGroup
	for _, n := range names {
		issues := byRepo[n]
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })
		repos = append(repos, RepoIssues{Repo: n, Issues: issues})
		groups = append(groups, newGroup(n, issues))
	}
	d.repos, d.truncated, d.listed = repos, truncated, true
	pr.step(d.clone, "repos")

	for _, rp := range repos {
		for i := range rp.Issues {
			rp.Issues[i] = r.withPending(ctx, rp.Repo, rp.Issues[i])
		}
	}
	pr.step(d.clone, "pendingComment")

	for i := range repos {
		repos[i].Image = r.image(ctx, repos[i].Repo, LatestImage{})
	}
	pr.step(d.clone, "image")

	r.issueGraph(ctx, groups)
	for i, g := range groups {
		repos[i].Permission, repos[i].PermissionError = g.perm, g.permErr
	}
	pr.step(d.clone, "blockedBy", "permission")

	d.nibfin, d.nibfinErr = r.nibfinRepos(ctx)
	d.reposRead = true
	pr.step(d.clone, "nibfinRepos")

	r.runners = wait()
	r.issueRuns(ctx, groups)
	pr.step(d.clone, "runs")
}

// search pages through GitHub's issue search, up to its 1,000 results.
func (r *reader) search(ctx context.Context) ([]issueJSON, bool, *op.Error) {
	var all []issueJSON
	total := 0
	for page := 1; len(all) < maxResults; page++ {
		q := url.Values{
			"q":        {"is:issue is:open label:scope,request assignee:@me"},
			"per_page": {fmt.Sprint(perPage)},
			"page":     {fmt.Sprint(page)},
		}
		var body struct {
			TotalCount int         `json:"total_count"`
			Items      []issueJSON `json:"items"`
		}
		if err := r.get(ctx, "search/issues?"+q.Encode(), &body); err != nil {
			return all, total > len(all), apiErr(op.CodeGitHubAPIFailed, "search the open scope or request Issues assigned to you", err)
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

// repoIssues lists every open Issue of repo, without its pending comment;
// the Issues list includes PRs, which are skipped.
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
				out = append(out, toIssue(it))
			}
		}
		if len(items) < perPage {
			return out, nil
		}
	}
}

// withPending reads the repair state of an Issue (repair.Read). For a
// pending Issue it links the pending comment, the oldest comment starting
// with the pending marker by the account that added the pending label last
// (Core's own rule, with Core's account taken from the label event), and
// reports the repair record that covers the stop. Nothing found is no link
// and the act state; a failed read is PendingCommentError and no Repair.
//
// For every Issue it read, pending or not, it reports the parts Core's
// choice comment leaves for a person (NeedsPerson). An Issue without
// pending is read only when it has comments, and a failed read of one
// shows nothing.
func (r *reader) withPending(ctx context.Context, repo string, is Issue) Issue {
	if !is.Pending && is.comments == 0 {
		return is
	}
	issue := fmt.Sprintf("%s/issues/%d", base(repo), is.Number)
	what := fmt.Sprintf("read %s#%d", repo, is.Number)
	s, err := repair.Read(ctx, r.get, issue)
	if err == nil {
		is.NeedsPerson = needsPerson(s)
	}
	if !is.Pending {
		return is
	}
	if err != nil {
		var re *repair.ReadError
		if errors.As(err, &re) {
			is.PendingCommentError = apiErr(op.CodeGitHubAPIFailed, what+" "+re.Part, re.Err)
		} else {
			is.PendingCommentError = apiErr(op.CodeGitHubAPIFailed, what, err)
		}
		return is
	}
	if s.Pending != nil {
		is.PendingComment = s.Pending.URL
	}
	if rep, ok := s.Evaluate(true); ok {
		is.Repair = toRepair(rep)
	}
	return is
}

// needsPerson is what Core's choice comment in s leaves for a person: its
// Not applied parts while it is applying or applied; nil without such
// parts.
func needsPerson(s repair.Snapshot) *NeedsPerson {
	if s.Choice == nil {
		return nil
	}
	ch, ok := repair.ParseChoice(s.Choice.Body)
	if !ok || (ch.Status != repair.ChoiceApplying && ch.Status != repair.ChoiceApplied) || len(ch.NotApplied) == 0 {
		return nil
	}
	return &NeedsPerson{Comment: s.Choice.URL, Items: ch.NotApplied}
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

// nibfinRun reads whether the default branch has nibfin-issue.yml, the
// newest commit that changed it there, the PR that merged that commit, and
// a successful Nibfin run created at or after that change. It never walks
// the file's history: about four serial reads for any repository.
func (r *reader) nibfinRun(ctx context.Context, repo string) NibfinRun {
	out := NibfinRun{}
	fail := func(what string, err error) NibfinRun {
		out.Error = apiErr(op.CodeGitHubAPIFailed, what+" of "+repo, err)
		return out
	}
	_, found, err := templates.ReadFile(ctx, r.c, r.host+"/"+repo, issueFile, "")
	if err != nil {
		return fail("read "+issueFile, err)
	}
	if !found {
		return out
	}
	out.UsesNibfin = true

	// Without sha, GitHub lists the default branch's commits, newest first.
	var list []commitJSON
	q := url.Values{"path": {issueFile}, "per_page": {"1"}}
	if err := r.get(ctx, base(repo)+"/commits?"+q.Encode(), &list); err != nil {
		return fail("list the commits of "+issueFile, err)
	}
	if len(list) == 0 || list[0].SHA == "" {
		return out
	}
	last := list[0]
	out.Commit = last.SHA
	changed, err := time.Parse(time.RFC3339, last.Commit.Committer.Date)
	if err != nil {
		return fail("read the date of commit "+last.SHA, err)
	}

	// A merge commit flow dates the listed commit before its merge; the
	// merged PR's time is the change on the default branch.
	var prs []struct {
		Number   int     `json:"number"`
		HTMLURL  string  `json:"html_url"`
		MergedAt *string `json:"merged_at"`
	}
	if err := r.get(ctx, base(repo)+"/commits/"+url.PathEscape(last.SHA)+"/pulls", &prs); err != nil {
		return fail("list the PRs of "+last.SHA, err)
	}
	for _, pr := range prs {
		if pr.MergedAt == nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339, *pr.MergedAt); err == nil {
			out.PR = &PullRequest{Number: pr.Number, URL: pr.HTMLURL}
			changed = t
			break
		}
	}
	out.ChangedAt = changed.UTC().Format(time.RFC3339)

	created := ">=" + changed.UTC().Format("2006-01-02T15:04:05") + "+00:00"
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
			if err != nil || t.Before(changed) || !strings.HasPrefix(run.Name, runPrefix) {
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

package status

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// The Dashboard's Runner activity (#418): what each Runner on this host is
// doing and its busy periods over the last 24 hours, read from the
// execution repository's shared-runner runs and their jobs, with the
// running Job containers as the fallback. It is a Dashboard-only read:
// status and its schema do not carry it. Nothing is written to disk; the
// reader keeps completed runs' jobs in memory for its own lifetime.

// activityWindow is how far back the busy periods go.
const activityWindow = 24 * time.Hour

// sharedRunner is the workflow whose Jobs run on the self-hosted Runners.
const sharedRunner = "nibfin-shared-runner.yml"

var readHostSlots = op.ReadHostSlots

// ownerRepoPattern is the shape of an OWNER/REPO that may form a link.
var ownerRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Activity is the Runner view's activity read.
type Activity struct {
	// From and To bound the window, RFC 3339.
	From string `json:"from"`
	To   string `json:"to"`
	// Repo is the execution repository as HOST/OWNER/REPO.
	Repo    string         `json:"repo,omitempty"`
	Runners []SlotActivity `json:"runners"`
	// SlotsError is set when the Runner slots could not be read.
	SlotsError *op.Error `json:"slotsError,omitempty"`
	// JobsError is set when the jobs could not be read; the periods are
	// then unknown and the currents come from the containers only.
	JobsError *op.Error `json:"jobsError,omitempty"`
	// ContainersError is set when the Job containers could not be read.
	ContainersError *op.Error `json:"containersError,omitempty"`
	// Unattributed are running Job containers matched to no slot.
	Unattributed []ContainerJob `json:"unattributed"`
}

// SlotActivity is one Runner slot's current Job and busy periods.
type SlotActivity struct {
	Slot     string      `json:"slot"`
	Provider string      `json:"provider"`
	Name     string      `json:"name,omitempty"`
	Current  *CurrentJob `json:"current,omitempty"`
	Periods  []Period    `json:"periods"`
}

// CurrentJob is the Job a Runner runs now. Repo, Issue and IssueURL are set
// only for a valid OWNER/REPO and number; Stage only from a run title.
type CurrentJob struct {
	Repo      string `json:"repo,omitempty"`
	Issue     int    `json:"issue,omitempty"`
	IssueURL  string `json:"issueUrl,omitempty"`
	Stage     string `json:"stage,omitempty"`
	URL       string `json:"url,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	// Source is "github" or "container".
	Source string `json:"source"`
}

// Period is one job's busy time on a Runner, clipped to the window. End is
// empty while the job runs.
type Period struct {
	Start    string `json:"start"`
	End      string `json:"end,omitempty"`
	Failed   bool   `json:"failed"`
	Repo     string `json:"repo,omitempty"`
	Issue    int    `json:"issue,omitempty"`
	IssueURL string `json:"issueUrl,omitempty"`
	Stage    string `json:"stage,omitempty"`
	URL      string `json:"url,omitempty"`
}

// ContainerJob is a running Job container no slot was matched to.
type ContainerJob struct {
	Repo      string `json:"repo,omitempty"`
	Issue     int    `json:"issue,omitempty"`
	IssueURL  string `json:"issueUrl,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
}

// ActivityReader reads the activity and keeps each completed run's jobs,
// keyed by run id and attempt, for its own lifetime.
type ActivityReader struct {
	mu    sync.Mutex
	cache map[runKey][]jobJSON
}

type runKey struct {
	id      int64
	attempt int
}

// NewActivityReader returns a reader with an empty cache.
func NewActivityReader() *ActivityReader {
	return &ActivityReader{cache: map[runKey][]jobJSON{}}
}

// Read reads the activity of the window that ends now.
func (a *ActivityReader) Read(ctx context.Context) (Activity, error) {
	return a.ReadActivity(ctx, time.Now())
}

// ReadActivity reads the activity of the window that ends at now. It only
// reads; every failure is reported in the result.
func (a *ActivityReader) ReadActivity(ctx context.Context, now time.Time) (Activity, error) {
	now = now.UTC()
	from := now.Add(-activityWindow)
	act := Activity{From: from.Format(time.RFC3339), To: now.Format(time.RFC3339), Runners: []SlotActivity{}, Unattributed: []ContainerJob{}}

	hs := readHostSlots(ctx)
	act.Repo, act.SlotsError, act.ContainersError = hs.Repo, hs.Error, hs.JobsError
	byName := map[string]*SlotActivity{}
	for _, s := range hs.Slots {
		act.Runners = append(act.Runners, SlotActivity{Slot: s.Slot, Provider: s.Provider, Name: s.Name, Periods: []Period{}})
	}
	for i, s := range hs.Slots {
		if s.Name != "" {
			byName[strings.ToLower(s.Name)] = &act.Runners[i]
		}
	}

	execHost := ""
	switch {
	case hs.Error != nil:
		act.JobsError = hs.Error
	case hs.Repo == "":
		act.JobsError = op.AsError(op.Errorf(op.CodeNoRunners, "no Runner on this host is configured, so there is no execution repository to read jobs from"))
	default:
		parts, err := splitRepo(hs.Repo)
		if err != nil {
			act.JobsError = op.AsError(err)
			break
		}
		execHost = parts[0]
		gh, _ := defaultHost()
		if !strings.EqualFold(gh, execHost) {
			act.JobsError = op.AsError(op.Errorf(op.CodeAuthUnavailable, "the Runners serve %s, on another host than the gh login's (%s)", hs.Repo, gh))
			break
		}
		if e := a.readJobs(ctx, execHost, parts[1]+"/"+parts[2], from, byName); e != nil {
			act.JobsError = e
		}
	}
	if act.JobsError != nil {
		// Periods read before the failure are not a complete picture.
		for i := range act.Runners {
			act.Runners[i].Periods = []Period{}
			act.Runners[i].Current = nil
		}
	}
	if execHost == "" && hs.Repo != "" {
		execHost, _, _ = strings.Cut(hs.Repo, "/")
	}
	if execHost == "" {
		execHost, _ = defaultHost()
	}
	a.attribute(&act, hs, execHost)
	return act, nil
}

type runListJSON struct {
	ID           int64  `json:"id"`
	DisplayTitle string `json:"display_title"`
	Status       string `json:"status"`
	RunAttempt   int    `json:"run_attempt"`
	Path         string `json:"path"`
	CreatedAt    string `json:"created_at"`
}

type jobJSON struct {
	RunnerName  *string `json:"runner_name"`
	Status      string  `json:"status"`
	Conclusion  *string `json:"conclusion"`
	StartedAt   *string `json:"started_at"`
	CompletedAt *string `json:"completed_at"`
	HTMLURL     string  `json:"html_url"`
}

// readJobs lists the shared-runner runs created in the window and those in
// progress, reads each run's latest jobs and gives each named slot its
// current Job and periods.
func (a *ActivityReader) readJobs(ctx context.Context, host, repo string, from time.Time, byName map[string]*SlotActivity) *op.Error {
	c, err := newREST(host)
	if err != nil {
		return op.AsError(op.Errorf(op.CodeAuthUnavailable, "cannot use the gh login for %s: %v", host, err))
	}
	r := &reader{c: c, host: host}
	created := ">=" + from.Format("2006-01-02T15:04:05") + "+00:00"
	runs, e := r.sharedRuns(ctx, repo, url.Values{"created": {created}}, from)
	if e != nil {
		return e
	}
	running, e := r.sharedRuns(ctx, repo, url.Values{"status": {"in_progress"}}, time.Time{})
	if e != nil {
		return e
	}
	seen := map[int64]bool{}
	var all []runListJSON
	for _, run := range append(runs, running...) {
		if !seen[run.ID] {
			seen[run.ID] = true
			all = append(all, run)
		}
	}

	keep := map[runKey]bool{}
	for _, run := range all {
		k := runKey{run.ID, run.RunAttempt}
		keep[k] = true
		a.mu.Lock()
		jobs, cached := a.cache[k]
		a.mu.Unlock()
		if !cached {
			var e *op.Error
			if jobs, e = r.runJobs(ctx, repo, run.ID); e != nil {
				return e
			}
			if run.Status == "completed" {
				a.mu.Lock()
				a.cache[k] = jobs
				a.mu.Unlock()
			}
		}
		addJobs(run, jobs, host, from, byName)
	}
	// Runs that left the window are no longer needed.
	a.mu.Lock()
	for k := range a.cache {
		if !keep[k] {
			delete(a.cache, k)
		}
	}
	a.mu.Unlock()
	for _, s := range byName {
		sort.SliceStable(s.Periods, func(i, j int) bool { return s.Periods[i].Start < s.Periods[j].Start })
	}
	return nil
}

// sharedRuns lists the shared-runner workflow's runs with q, newest first,
// up to GitHub's 1,000 results. With a non-zero from, paging stops at the
// first run created before it.
func (r *reader) sharedRuns(ctx context.Context, repo string, q url.Values, from time.Time) ([]runListJSON, *op.Error) {
	var out []runListJSON
	seen := 0
	for page := 1; ; page++ {
		q.Set("per_page", strconv.Itoa(perPage))
		q.Set("page", strconv.Itoa(page))
		var body struct {
			Runs []runListJSON `json:"workflow_runs"`
		}
		p := base(repo) + "/actions/workflows/" + sharedRunner + "/runs?" + q.Encode()
		if err := r.get(ctx, p, &body); err != nil {
			return nil, apiErr(op.CodeGitHubAPIFailed, "list the shared-runner runs of "+repo, err)
		}
		older := false
		for _, run := range body.Runs {
			if !strings.HasSuffix(run.Path, "/"+sharedRunner) && run.Path != "" {
				continue
			}
			if !from.IsZero() {
				if t, err := time.Parse(time.RFC3339, run.CreatedAt); err == nil && t.Before(from) {
					older = true
					continue
				}
			}
			out = append(out, run)
		}
		seen += len(body.Runs)
		if len(body.Runs) < perPage || seen >= maxResults || older {
			return out, nil
		}
	}
}

// runJobs reads a run's latest jobs.
func (r *reader) runJobs(ctx context.Context, repo string, id int64) ([]jobJSON, *op.Error) {
	var out []jobJSON
	for page := 1; ; page++ {
		var body struct {
			Jobs []jobJSON `json:"jobs"`
		}
		p := fmt.Sprintf("%s/actions/runs/%d/jobs?filter=latest&per_page=%d&page=%d", base(repo), id, perPage, page)
		if err := r.get(ctx, p, &body); err != nil {
			return nil, apiErr(op.CodeGitHubAPIFailed, fmt.Sprintf("list the jobs of run %d of %s", id, repo), err)
		}
		out = append(out, body.Jobs...)
		if len(body.Jobs) < perPage || len(out) >= maxResults {
			return out, nil
		}
	}
}

// addJobs adds a run's jobs to the slots their runner names.
func addJobs(run runListJSON, jobs []jobJSON, host string, from time.Time, byName map[string]*SlotActivity) {
	repo, issue, stage, ok := TitleJob(run.DisplayTitle)
	issueURL := ""
	if ok && ownerRepoPattern.MatchString(repo) {
		issueURL = "https://" + host + "/" + repo + "/issues/" + strconv.Itoa(issue)
	} else {
		repo, issue, stage = "", 0, ""
	}
	for _, j := range jobs {
		if j.RunnerName == nil || j.StartedAt == nil {
			continue
		}
		s := byName[strings.ToLower(*j.RunnerName)]
		if s == nil {
			continue
		}
		start, err := time.Parse(time.RFC3339, *j.StartedAt)
		if err != nil {
			continue
		}
		jobURL := httpsURL(j.HTMLURL)
		if j.Status == "in_progress" {
			if s.Current == nil || s.Current.StartedAt < *j.StartedAt {
				s.Current = &CurrentJob{Repo: repo, Issue: issue, IssueURL: issueURL, Stage: stage, URL: jobURL,
					StartedAt: start.UTC().Format(time.RFC3339), Source: "github"}
			}
		}
		p := Period{Repo: repo, Issue: issue, IssueURL: issueURL, Stage: stage, URL: jobURL}
		if j.Status == "completed" {
			if j.CompletedAt == nil {
				continue
			}
			end, err := time.Parse(time.RFC3339, *j.CompletedAt)
			if err != nil || end.Before(from) {
				continue
			}
			if end.Before(start) {
				end = start
			}
			p.End = end.UTC().Format(time.RFC3339)
			p.Failed = j.Conclusion != nil && (*j.Conclusion == "failure" || *j.Conclusion == "timed_out")
		} else if j.Status != "in_progress" {
			continue
		}
		if start.Before(from) {
			start = from
		}
		p.Start = start.UTC().Format(time.RFC3339)
		s.Periods = append(s.Periods, p)
	}
}

// attribute gives a slot without a GitHub current the running Job container
// that bind-mounts a path under the slot's directory; the others are
// unattributed. Labels are untrusted: only a valid OWNER/REPO and a
// positive canonical number name an Issue.
func (a *ActivityReader) attribute(act *Activity, hs op.HostSlots, host string) {
	for _, c := range hs.Jobs {
		repo, issue, issueURL := validJob(c.Repo, c.Issue, host)
		started := ""
		if t, err := time.Parse(time.RFC3339Nano, c.StartedAt); err == nil {
			started = t.UTC().Format(time.RFC3339)
		}
		slot := -1
		for i, s := range hs.Slots {
			if s.Dir != "" && i < len(act.Runners) && mountsUnder(c.Mounts, s.Dir) {
				slot = i
				break
			}
		}
		if slot < 0 {
			act.Unattributed = append(act.Unattributed, ContainerJob{Repo: repo, Issue: issue, IssueURL: issueURL, StartedAt: started})
			continue
		}
		if act.Runners[slot].Current == nil {
			act.Runners[slot].Current = &CurrentJob{Repo: repo, Issue: issue, IssueURL: issueURL, StartedAt: started, Source: "container"}
		}
	}
}

func validJob(repo, issue, host string) (string, int, string) {
	n, err := strconv.Atoi(issue)
	if !ownerRepoPattern.MatchString(repo) || err != nil || n <= 0 || strconv.Itoa(n) != issue {
		return "", 0, ""
	}
	return repo, n, "https://" + host + "/" + repo + "/issues/" + issue
}

// mountsUnder reports whether a mount source is dir or lies under it.
func mountsUnder(sources []string, dir string) bool {
	d := strings.TrimRight(dir, "/")
	if d == "" {
		return false
	}
	for _, s := range sources {
		if s == d || strings.HasPrefix(s, d+"/") {
			return true
		}
	}
	return false
}

// httpsURL passes u through only when it is an absolute https URL.
func httpsURL(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" || p.Host == "" {
		return ""
	}
	return u
}

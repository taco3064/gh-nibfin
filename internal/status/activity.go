package status

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

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
	// History is set on a full read's answer: the periods cover the
	// window. A current read's answer has only the runs in progress.
	History bool `json:"history"`
	// CoversFrom, RFC 3339, is set when the window held more runs than
	// GitHub lists: the periods then reach back only to this time, the
	// earliest listed run's creation.
	CoversFrom string `json:"coversFrom,omitempty"`
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

// The jobs requests a read part makes at once. Only one read of each part
// runs at a time, so the reader never has more than
// historyJobsLimit+currentJobsLimit jobs requests in flight, far below
// GitHub's 100 concurrent requests, and a current read never queues behind
// a full read's backlog.
const (
	historyJobsLimit = 6
	currentJobsLimit = 4
)

// The read parts: the keys of the reader's reads in flight.
const (
	partHistory = "history"
	partCurrent = "current"
)

// flightJoined is called once a caller has started or joined the read of a
// part in flight; tests use it to order overlapping reads.
var flightJoined = func(part string) {}

// ActivityReader reads the activity and keeps each completed run's jobs,
// keyed by run id and attempt, for its own lifetime. It runs one read of
// each part at a time: a caller that arrives while a read of the same part
// is in flight joins it and gets its answer.
type ActivityReader struct {
	mu    sync.Mutex
	cache map[runKey][]jobJSON
	reads singleflight.Group
}

type runKey struct {
	id      int64
	attempt int
}

// currentRead is a current read's answer with the in-progress runs it
// listed and their jobs. jobs is nil when the read failed; a full read
// reuses them otherwise.
type currentRead struct {
	act  Activity
	runs []runListJSON
	jobs map[runKey][]jobJSON
}

// jobsRead reads the jobs of an execution repository into the named slots
// and returns the earliest time the periods cover when the window listing
// was cut off.
type jobsRead func(r *reader, repo string, from time.Time, byName map[string]*SlotActivity) (string, *op.Error)

// NewActivityReader returns a reader with an empty cache.
func NewActivityReader() *ActivityReader {
	return &ActivityReader{cache: map[runKey][]jobJSON{}}
}

// Read reads the activity of the window that ends now.
func (a *ActivityReader) Read(ctx context.Context) (Activity, error) {
	return a.ReadActivity(ctx, time.Now())
}

// Current reads the Runners' current Jobs now.
func (a *ActivityReader) Current(ctx context.Context) (Activity, error) {
	return a.ReadCurrent(ctx, time.Now())
}

// ReadActivity reads the activity of the window that ends at now: the
// current Jobs and the busy periods, with History set. It only reads; every
// failure is reported in the result, and the error is ctx's only, when the
// caller leaves before the answer.
func (a *ActivityReader) ReadActivity(ctx context.Context, now time.Time) (Activity, error) {
	v, err := a.flight(ctx, partHistory, func(ctx context.Context) any { return a.readFull(ctx, now) })
	if err != nil {
		return Activity{}, err
	}
	return v.(Activity), nil
}

// ReadCurrent reads only the Runners' current Jobs at now, from the runs in
// progress: it never lists the window, never touches the cache and leaves
// History unset. Its periods are those of the runs in progress.
func (a *ActivityReader) ReadCurrent(ctx context.Context, now time.Time) (Activity, error) {
	cur, err := a.current(ctx, now)
	if err != nil {
		return Activity{}, err
	}
	return cur.act, nil
}

func (a *ActivityReader) current(ctx context.Context, now time.Time) (*currentRead, error) {
	v, err := a.flight(ctx, partCurrent, func(ctx context.Context) any { return a.readCurrent(ctx, now) })
	if err != nil {
		return nil, err
	}
	return v.(*currentRead), nil
}

// flight runs read as the one read of part in flight, or joins the one
// already running. The read runs without the first caller's cancellation,
// so one caller leaving fails no other; each caller returns its own
// ctx.Err() when it leaves first. A read nobody waits for still finishes
// and fills the cache.
func (a *ActivityReader) flight(ctx context.Context, part string, read func(context.Context) any) (any, error) {
	shared := context.WithoutCancel(ctx)
	ch := a.reads.DoChan(part, func() (any, error) { return read(shared), nil })
	flightJoined(part)
	select {
	case r := <-ch:
		return r.Val, r.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// readCurrent is a current read: the in-progress runs and their jobs.
func (a *ActivityReader) readCurrent(ctx context.Context, now time.Time) *currentRead {
	cur := &currentRead{}
	cur.act = a.read(ctx, now, func(r *reader, repo string, from time.Time, byName map[string]*SlotActivity) (string, *op.Error) {
		running, _, e := r.sharedRuns(ctx, repo, url.Values{"status": {"in_progress"}}, time.Time{})
		if e != nil {
			return "", e
		}
		running = uniqueRuns(running)
		got, e := fetchJobs(ctx, r, repo, running, currentJobsLimit)
		if e != nil {
			return "", e
		}
		jobs := make(map[runKey][]jobJSON, len(running))
		for i, run := range running {
			jobs[runKey{run.ID, run.RunAttempt}] = got[i]
			addJobs(run, got[i], r.host, from, byName)
		}
		sortPeriods(byName)
		cur.runs, cur.jobs = running, jobs
		return "", nil
	})
	return cur
}

// readFull is a full read. It first takes a current read's answer, joining
// one in flight or starting one, and reuses its in-progress runs and jobs;
// when that read failed, it lists and fetches the in-progress runs itself.
// The two share no request, so neither's failure fails the other.
func (a *ActivityReader) readFull(ctx context.Context, now time.Time) Activity {
	cur, err := a.current(ctx, now)
	if err != nil || cur.jobs == nil {
		cur = nil
	}
	act := a.read(ctx, now, func(r *reader, repo string, from time.Time, byName map[string]*SlotActivity) (string, *op.Error) {
		return a.readHistory(ctx, r, repo, from, byName, cur)
	})
	act.History = true
	return act
}

// read reads the host slots and, when they name an execution repository on
// the gh login's host, calls jobs for the slots' current Jobs and periods.
// A jobs failure clears both; the Job containers then fill the currents.
func (a *ActivityReader) read(ctx context.Context, now time.Time, jobs jobsRead) Activity {
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
		c, err := newREST(execHost)
		if err != nil {
			act.JobsError = op.AsError(op.Errorf(op.CodeAuthUnavailable, "cannot use the gh login for %s: %v", execHost, err))
			break
		}
		act.CoversFrom, act.JobsError = jobs(&reader{c: c, host: execHost}, parts[1]+"/"+parts[2], from, byName)
	}
	if act.JobsError != nil {
		// Periods read before the failure are not a complete picture.
		for i := range act.Runners {
			act.Runners[i].Periods = []Period{}
			act.Runners[i].Current = nil
		}
		act.CoversFrom = ""
	}
	if execHost == "" && hs.Repo != "" {
		execHost, _, _ = strings.Cut(hs.Repo, "/")
	}
	if execHost == "" {
		execHost, _ = defaultHost()
	}
	a.attribute(&act, hs, execHost)
	return act
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

// readHistory lists the shared-runner runs created in the window and those
// in progress, reads each run's latest jobs and gives each named slot its
// current Job and periods. The in-progress runs and their jobs come from
// cur when it is set; completed runs come from the cache when they are in
// it. It returns the earliest listed run's creation time when the window
// listing was cut off before reaching the window start.
func (a *ActivityReader) readHistory(ctx context.Context, r *reader, repo string, from time.Time, byName map[string]*SlotActivity, cur *currentRead) (string, *op.Error) {
	created := ">=" + from.Format("2006-01-02T15:04:05") + "+00:00"
	runs, cut, e := r.sharedRuns(ctx, repo, url.Values{"created": {created}}, from)
	if e != nil {
		return "", e
	}
	coversFrom := ""
	if cut {
		coversFrom = earliest(runs)
	}
	var running []runListJSON
	seenStatus := map[runKey]string{}
	if cur != nil {
		running = cur.runs
		for _, run := range cur.runs {
			seenStatus[runKey{run.ID, run.RunAttempt}] = run.Status
		}
	} else if running, _, e = r.sharedRuns(ctx, repo, url.Values{"status": {"in_progress"}}, time.Time{}); e != nil {
		return "", e
	}
	all := uniqueRuns(append(runs, running...))

	jobs := make([][]jobJSON, len(all))
	keep := map[runKey]bool{}
	var need []runListJSON
	var at []int
	a.mu.Lock()
	for i, run := range all {
		k := runKey{run.ID, run.RunAttempt}
		keep[k] = true
		if j, ok := a.cache[k]; ok {
			jobs[i] = j
			continue
		}
		// A run the current read saw in the same state; one that has
		// changed state since is new data and read again.
		if st, ok := seenStatus[k]; ok && st == run.Status {
			jobs[i] = cur.jobs[k]
			continue
		}
		need = append(need, run)
		at = append(at, i)
	}
	a.mu.Unlock()

	got, e := fetchJobs(ctx, r, repo, need, historyJobsLimit)
	if e != nil {
		return "", e
	}
	a.mu.Lock()
	for n, i := range at {
		jobs[i] = got[n]
		if run := all[i]; run.Status == "completed" {
			a.cache[runKey{run.ID, run.RunAttempt}] = got[n]
		}
	}
	// Runs that left the window are no longer needed.
	for k := range a.cache {
		if !keep[k] {
			delete(a.cache, k)
		}
	}
	a.mu.Unlock()
	for i, run := range all {
		addJobs(run, jobs[i], r.host, from, byName)
	}
	sortPeriods(byName)
	return coversFrom, nil
}

// uniqueRuns keeps the first of each run id, in order.
func uniqueRuns(runs []runListJSON) []runListJSON {
	seen := map[int64]bool{}
	var out []runListJSON
	for _, run := range runs {
		if !seen[run.ID] {
			seen[run.ID] = true
			out = append(out, run)
		}
	}
	return out
}

// earliest is the earliest creation time among runs, RFC 3339.
func earliest(runs []runListJSON) string {
	var first time.Time
	for _, run := range runs {
		if t, err := time.Parse(time.RFC3339, run.CreatedAt); err == nil && (first.IsZero() || t.Before(first)) {
			first = t
		}
	}
	if first.IsZero() {
		return ""
	}
	return first.UTC().Format(time.RFC3339)
}

// fetchJobs reads the runs' latest jobs, at most limit requests at once,
// and returns them by the runs' index. The first failure cancels the rest
// of this call's requests, and only those, and is returned.
func fetchJobs(ctx context.Context, r *reader, repo string, runs []runListJSON, limit int) ([][]jobJSON, *op.Error) {
	out := make([][]jobJSON, len(runs))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for i, run := range runs {
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			jobs, e := r.runJobs(gctx, repo, run.ID)
			if e != nil {
				return e
			}
			out[i] = jobs
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		var e *op.Error
		if errors.As(err, &e) {
			return nil, e
		}
		return nil, apiErr(op.CodeGitHubAPIFailed, "list the jobs of the shared-runner runs of "+repo, err)
	}
	return out, nil
}

func sortPeriods(byName map[string]*SlotActivity) {
	for _, s := range byName {
		sort.SliceStable(s.Periods, func(i, j int) bool { return s.Periods[i].Start < s.Periods[j].Start })
	}
}

// sharedRuns lists the shared-runner workflow's runs with q, newest first,
// up to GitHub's 1,000 results. With a non-zero from, paging stops at the
// first run created before it, and cut reports that the listing stopped at
// the result limit before reaching such a run.
func (r *reader) sharedRuns(ctx context.Context, repo string, q url.Values, from time.Time) (runs []runListJSON, cut bool, e *op.Error) {
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
			return nil, false, apiErr(op.CodeGitHubAPIFailed, "list the shared-runner runs of "+repo, err)
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
		if len(body.Runs) < perPage || older {
			return out, false, nil
		}
		if seen >= maxResults {
			return out, !from.IsZero(), nil
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

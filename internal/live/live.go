// Package live keeps the Dashboard's person view current. It follows
// podman events for the Job containers of this host's Runners and checks
// GitHub with conditional requests, re-reads what changed, and sends each
// change as an Event. It only reads: dispatch stays event-driven in Core,
// and no Job connects back.
package live

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/auth"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/status"
)

// interval is the poll tick. A probe is checked at most once per tick, and
// not before its x-poll-interval or Retry-After has passed.
const interval = 30 * time.Second

// Tests replace these.
var (
	now         = time.Now
	defaultHost = auth.DefaultHost
	newTicker   = func(d time.Duration) (<-chan time.Time, func()) {
		t := time.NewTicker(d)
		return t.C, t.Stop
	}
)

// Deps are the operation entry points Watch reads through.
type Deps struct {
	// Status is the status entry point; Watch reads the person view with it.
	Status func(op.RepoParams) op.Operation
	// ReadIssue re-reads one person-view Issue (status.ReadIssue).
	ReadIssue func(ctx context.Context, host, user string, runners op.RunnersReport, repo string, number int) (status.Issue, bool, *op.Error)
}

// Event is one change Watch sends. Event names it:
//
//   - partial: before the stream's first snapshot, the part of the first
//     full read done so far; Loading names what is still being read.
//   - snapshot: a full person-view read; Result is the status --json object.
//   - issue: one re-read Issue of Repo.
//   - checked: every GitHub check succeeded and found nothing new up to At.
//   - podman: whether podman events are followed, and why not.
//   - error: a failed GitHub check (Source github) or read (Source status).
//
// At is the server's RFC 3339 time: when a read began, or the oldest last
// successful check.
type Event struct {
	Event    string        `json:"event"`
	At       string        `json:"at,omitempty"`
	Result   op.Result     `json:"result,omitempty"`
	Loading  []string      `json:"loading,omitempty"`
	Repo     string        `json:"repo,omitempty"`
	Issue    *status.Issue `json:"issue,omitempty"`
	Watching *bool         `json:"watching,omitempty"`
	Source   string        `json:"source,omitempty"`
	Error    *op.Error     `json:"error,omitempty"`
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ref is a listed Issue.
type ref struct {
	repo   string // OWNER/REPO as the person view lists it
	number int
}

// view is the watch set a snapshot gives.
type view struct {
	host    string
	user    string
	runners op.RunnersReport
	// exec is the execution repository, OWNER/REPO, when it is on host;
	// execElsewhere is true when it is known to be on another host.
	exec          string
	execElsewhere bool
	repos         []string
	listed        map[string]ref // owner/repo#n, lower case
}

func viewOf(res op.Result) view {
	v := view{listed: map[string]ref{}}
	v.host, _ = res["githubHost"].(string)
	v.user, _ = res["user"].(string)
	if hr, ok := res["host"].(op.HostReport); ok {
		v.runners = hr.Runners
		if hr.Runners.Error == nil && hr.Runners.Repo != "" {
			parts := strings.Split(hr.Runners.Repo, "/")
			if len(parts) == 3 && strings.EqualFold(parts[0], v.host) {
				v.exec = parts[1] + "/" + parts[2]
			} else {
				v.execElsewhere = true
			}
		}
	}
	repos, _ := res["repos"].([]status.RepoIssues)
	for _, r := range repos {
		v.repos = append(v.repos, r.Repo)
		for _, is := range r.Issues {
			v.listed[key(r.Repo, is.Number)] = ref{repo: r.Repo, number: is.Number}
		}
	}
	return v
}

func key(repo string, n int) string { return strings.ToLower(repo + "#" + strconv.Itoa(n)) }

// watcher is one Watch run. The poll loop owns the probes; the read worker
// does every read, one at a time; the podman follower marks Issues.
type watcher struct {
	d       Deps
	sendMu  sync.Mutex
	sendRaw func(Event)

	mu       sync.Mutex
	v        view
	full     bool            // a full read is pending
	dirty    map[string]bool // Issues pending a re-read
	busy     bool            // the worker is reading
	lastDate time.Time       // GitHub's newest Date seen by a check
	podState string          // the last podman event sent

	wake    chan struct{} // the worker has pending work
	rebuilt chan struct{} // a snapshot changed the watch set
	podDone chan error

	// Poll loop state.
	g        getter
	gHost    string
	probes   map[string]*probe
	podOn    bool
	checkedT time.Time
}

func (w *watcher) send(e Event) {
	w.sendMu.Lock()
	defer w.sendMu.Unlock()
	w.sendRaw(e)
}

func (w *watcher) sendError(source string, err *op.Error) {
	w.send(Event{Event: "error", Source: source, Error: err})
}

// Watch reads the person view, sends it as a snapshot, and then sends every
// change it sees until ctx ends. send is never called after Watch returns.
// The first read failing as a command (such as no gh login) ends Watch with
// that error; later failures are sent as error events.
func Watch(ctx context.Context, d Deps, send func(Event)) error {
	w := &watcher{
		d: d, sendRaw: send, dirty: map[string]bool{},
		wake: make(chan struct{}, 1), rebuilt: make(chan struct{}, 1), podDone: make(chan error, 1),
		probes: map[string]*probe{},
	}
	// The assigned Issues' baseline comes before the read, so a change
	// after it is seen by the next check.
	if host, _ := defaultHost(); host != "" {
		w.setHost(ctx, host)
	}
	if err := w.fullRead(ctx, true); err != nil {
		return err
	}
	select {
	case <-w.rebuilt:
	default:
	}
	w.rebuild(ctx)

	var wg sync.WaitGroup
	defer wg.Wait()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wg.Add(1)
	go func() { defer wg.Done(); w.work(ctx) }()
	w.startPodman(ctx, &wg)

	tick, stop := newTicker(interval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.rebuilt:
			w.rebuild(ctx)
		case err := <-w.podDone:
			w.podOn = false
			w.podman(false, err)
		case <-tick:
			w.tick(ctx, &wg)
		}
	}
}

// setHost makes the client and the assigned probes for host, and takes the
// probes' baselines.
func (w *watcher) setHost(ctx context.Context, host string) {
	w.gHost, w.g = host, nil
	w.probes = map[string]*probe{}
	g, err := newGetter(host)
	if err != nil {
		w.sendError("github", op.AsError(op.Errorf(op.CodeAuthUnavailable, "cannot use the gh login for %s: %v", host, err)))
		return
	}
	w.g = g
	for _, p := range assignedProbes() {
		w.add(ctx, p, time.Time{})
	}
}

// add takes a new probe's first answer; since is the runs probes' start.
func (w *watcher) add(ctx context.Context, p *probe, since time.Time) {
	if p.kind == runs {
		p.since = since
	}
	w.probes[p.id()] = p
	w.run(ctx, p)
}

// rebuild matches the probes to the current watch set: one per listed
// repository's Nibfin runs and workflow files, and the execution
// repository's shared-runner runs. Stopped probes start again.
func (w *watcher) rebuild(ctx context.Context) {
	w.mu.Lock()
	v, since := w.v, w.lastDate
	w.mu.Unlock()
	if v.host != "" && !strings.EqualFold(v.host, w.gHost) {
		w.setHost(ctx, v.host)
	}
	if w.g == nil {
		return
	}
	want := assignedProbes()
	for _, r := range v.repos {
		want = append(want, &probe{kind: runs, repo: r, workflow: issueWorkflow}, &probe{kind: folder, repo: r})
	}
	if v.exec != "" {
		want = append(want, &probe{kind: runs, repo: v.exec, workflow: sharedWorkflow})
	}
	keep := map[string]bool{}
	for _, p := range want {
		id := p.id()
		keep[id] = true
		if old := w.probes[id]; old != nil && !old.stopped {
			continue
		}
		w.add(ctx, p, since)
	}
	for id := range w.probes {
		if !keep[id] {
			delete(w.probes, id)
		}
	}
}

// run checks one probe and hands what it found to the worker. It reports
// whether the check succeeded.
func (w *watcher) run(ctx context.Context, p *probe) bool {
	t := now()
	o := p.check(ctx, w.g, t, w.isListed)
	w.mu.Lock()
	if o.date.After(w.lastDate) {
		w.lastDate = o.date
	}
	w.mu.Unlock()
	p.failing = o.err != nil
	if o.err != nil {
		p.due = 1
		if o.retry > 0 {
			p.due = ticksFor(o.retry)
		}
		w.sendError("github", o.err)
		return false
	}
	if o.full {
		w.mark(true)
	}
	if len(o.dirty) > 0 {
		w.mark(false, o.dirty...)
	}
	return true
}

func (w *watcher) isListed(k string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.v.listed[k]
	return ok
}

// tick checks every due probe, restarts a stopped podman follower, and
// sends checked when everything due succeeded and no read is pending.
func (w *watcher) tick(ctx context.Context, wg *sync.WaitGroup) {
	if !w.podOn {
		w.startPodman(ctx, wg)
	}
	if w.g == nil {
		w.rebuild(ctx)
		if w.g == nil {
			return
		}
	}
	ids := make([]string, 0, len(w.probes))
	for id := range w.probes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ok := true
	for _, id := range ids {
		p := w.probes[id]
		if p.stopped {
			continue
		}
		if p.due--; p.due > 0 {
			continue
		}
		if !w.run(ctx, p) {
			ok = false
		}
	}
	if !ok || ctx.Err() != nil {
		return
	}
	w.mu.Lock()
	idle := !w.busy && !w.full && len(w.dirty) == 0
	w.mu.Unlock()
	if !idle {
		return
	}
	var oldest time.Time
	for _, p := range w.probes {
		if p.stopped {
			continue
		}
		if p.ok.IsZero() || p.failing {
			return
		}
		if oldest.IsZero() || p.ok.Before(oldest) {
			oldest = p.ok
		}
	}
	if oldest.After(w.checkedT) {
		w.checkedT = oldest
		w.send(Event{Event: "checked", At: stamp(oldest)})
	}
}

// mark adds a full read or Issues to the pending set and wakes the worker.
func (w *watcher) mark(full bool, keys ...string) {
	w.mu.Lock()
	w.full = w.full || full
	for _, k := range keys {
		w.dirty[k] = true
	}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// work runs the pending reads, the whole pending set at a time, until ctx
// ends. What arrives during a batch runs in the next one.
func (w *watcher) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		}
		w.mu.Lock()
		full, dirty := w.full, w.dirty
		w.full, w.dirty, w.busy = false, map[string]bool{}, true
		v := w.v
		w.mu.Unlock()

		if !full {
			keys := make([]string, 0, len(dirty))
			for k := range dirty {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				r, ok := v.listed[k]
				if !ok {
					continue
				}
				t := now()
				is, listed, err := w.d.ReadIssue(ctx, v.host, v.user, v.runners, r.repo, r.number)
				if ctx.Err() != nil {
					break
				}
				if err != nil {
					w.sendError("status", err)
					continue
				}
				if !listed {
					full = true
					break
				}
				w.send(Event{Event: "issue", At: stamp(t), Repo: r.repo, Issue: &is})
			}
		}
		if full && ctx.Err() == nil {
			if err := w.fullRead(ctx, false); err != nil && ctx.Err() == nil {
				w.sendError("status", op.AsError(err))
			}
		}
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
	}
}

// fullRead reads the person view as the status plan route does, sends it
// as a snapshot and updates the watch set. The stream's first read also
// sends each section as it is read, as partial events before the snapshot.
func (w *watcher) fullRead(ctx context.Context, first bool) error {
	t := now()
	if first {
		ctx = status.WithPartial(ctx, func(res op.Result, loading []string) {
			w.send(Event{Event: "partial", At: stamp(t), Result: res, Loading: loading})
		})
	}
	o := w.d.Status(op.RepoParams{})
	p, err := o.Plan(ctx)
	if err != nil {
		return err
	}
	res, err := op.Finish(ctx, o, p, false)
	if err != nil {
		return err
	}
	if res == nil {
		res = op.Result{}
	}
	w.mu.Lock()
	w.v = viewOf(res)
	w.mu.Unlock()
	w.send(Event{Event: "snapshot", At: stamp(t), Result: res})
	select {
	case w.rebuilt <- struct{}{}:
	default:
	}
	return nil
}

// assignedLabels are the labels of the person view's Issues: an open Issue
// assigned to the gh user with either label (status's label:scope,request).
var assignedLabels = []string{"scope", "request"}

// assignedProbes are one assigned probe per label of the person view.
func assignedProbes() []*probe {
	var ps []*probe
	for _, l := range assignedLabels {
		ps = append(ps, &probe{kind: assigned, label: l})
	}
	return ps
}

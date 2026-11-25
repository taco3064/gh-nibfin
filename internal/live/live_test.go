package live

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/status"
)

var t0 = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

// fakeGH is GitHub for the probes: each path has a body whose hash is its
// ETag; a matching If-None-Match is a 304.
type fakeGH struct {
	fx      *fixture
	mu      sync.Mutex
	bodies  map[string]any
	errs    map[string]error
	polls   map[string]int
	reqs    []string
	headers []string // path + " " + If-None-Match
}

func (g *fakeGH) get(_ context.Context, path, etag string) (answer, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reqs = append(g.reqs, path)
	g.headers = append(g.headers, path+" "+etag)
	if err := g.errs[path]; err != nil {
		return answer{}, err
	}
	v, ok := g.bodies[path]
	if !ok {
		return answer{}, &api.HTTPError{StatusCode: http.StatusNotFound, Message: "Not Found"}
	}
	b, _ := json.Marshal(v)
	tag := fmt.Sprintf(`"%x"`, sha256.Sum256(b))
	a := answer{status: 200, etag: tag, date: g.fx.clock(), poll: time.Duration(g.polls[path]) * time.Second, body: b}
	if etag == tag {
		a.status, a.body = 304, nil
	}
	return a, nil
}

func (g *fakeGH) set(path string, v any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bodies[path] = v
}

func (g *fakeGH) count(path string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, r := range g.reqs {
		if r == path {
			n++
		}
	}
	return n
}

type statusOp struct{ fx *fixture }

func (s statusOp) Plan(context.Context) (op.Plan, error) {
	return op.Plan{Operation: "status", Steps: []op.Step{}, ReadOnly: true}, nil
}
func (s statusOp) Apply(context.Context, op.Plan) error { return nil }
func (s statusOp) ReadBack(context.Context, op.Plan) (op.Result, error) {
	s.fx.mu.Lock()
	defer s.fx.mu.Unlock()
	s.fx.reads++
	if s.fx.readErr != nil {
		return nil, s.fx.readErr
	}
	return s.fx.result(), nil
}

type fixture struct {
	t  *testing.T
	gh *fakeGH

	mu       sync.Mutex
	now      time.Time
	reads    int
	readErr  error
	repos    []status.RepoIssues
	issues   [][]string // ReadIssue keys, one slice per batch call order
	calls    []string
	unlisted map[string]bool
	block    chan struct{}
	podCalls int
	podErr   error
	podW     *io.PipeWriter
	podCtx   context.Context

	ticks  chan time.Time
	events chan Event
	cancel context.CancelFunc
	done   chan error
}

func (fx *fixture) clock() time.Time {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.now
}

func (fx *fixture) advance(d time.Duration) {
	fx.mu.Lock()
	fx.now = fx.now.Add(d)
	fx.mu.Unlock()
}

func (fx *fixture) result() op.Result {
	return op.Result{
		"githubHost": "github.com",
		"user":       "me",
		"host":       op.HostReport{Runners: op.RunnersReport{Repo: "github.com/e/exec", Runners: []op.RunnerState{}}},
		"repos":      fx.repos,
	}
}

func issues(ns ...int) []status.Issue {
	var out []status.Issue
	for _, n := range ns {
		out = append(out, status.Issue{Number: n, Labels: []string{}, BlockedBy: []status.Blocker{}, Runs: []status.Run{}})
	}
	return out
}

var (
	assignedPath = (&probe{kind: assigned}).path(1)
	assigned2    = (&probe{kind: assigned}).path(2)
	runsOne      = (&probe{kind: runs, repo: "a/one", workflow: issueWorkflow}).path(0)
	folderOne    = (&probe{kind: folder, repo: "a/one"}).path(0)
	runsExec     = (&probe{kind: runs, repo: "e/exec", workflow: sharedWorkflow}).path(0)
)

func runList(rs ...[2]string) map[string]any {
	l := []any{}
	for _, r := range rs {
		l = append(l, map[string]any{"display_title": r[0], "updated_at": r[1]})
	}
	return map[string]any{"workflow_runs": l}
}

func items(n int) []any {
	l := []any{}
	for i := range n {
		l = append(l, map[string]any{"number": i + 1})
	}
	return l
}

func newFixture(t *testing.T) *fixture {
	fx := &fixture{t: t, now: t0, repos: []status.RepoIssues{{Repo: "a/one", Issues: issues(3, 5)}},
		unlisted: map[string]bool{}, ticks: make(chan time.Time), events: make(chan Event, 100)}
	saved := &old{}
	saved.save()
	t.Cleanup(func() { saved.restore() })
	fx.gh = &fakeGH{fx: fx, bodies: map[string]any{
		assignedPath: items(2),
		runsOne:      runList([2]string{"Nibfin · a/one#3 · wake", "2026-11-01T00:00:00Z"}),
		folderOne:    []any{map[string]any{"name": "nibfin-issue.yml"}},
		runsExec:     runList(),
	}, errs: map[string]error{}, polls: map[string]int{}}
	newGetter = func(host string) (getter, error) {
		if host != "github.com" {
			t.Errorf("getter for %q", host)
		}
		return fx.gh, nil
	}
	now = fx.clock
	defaultHost = func() (string, string) { return "github.com", "default" }
	newTicker = func(time.Duration) (<-chan time.Time, func()) { return fx.ticks, func() {} }
	podmanEvents = func(ctx context.Context) (io.Reader, func() error, error) {
		fx.mu.Lock()
		defer fx.mu.Unlock()
		fx.podCalls++
		if fx.podErr != nil {
			return nil, nil, fx.podErr
		}
		r, w := io.Pipe()
		fx.podW, fx.podCtx = w, ctx
		go func() { <-ctx.Done(); w.Close() }()
		return r, func() error { return op.Errorf(op.CodePodmanFailed, "podman events stopped") }, nil
	}
	return fx
}

type old struct {
	g       func(string) (getter, error)
	n       func() time.Time
	h       func() (string, string)
	tk      func(time.Duration) (<-chan time.Time, func())
	pe      func(context.Context) (io.Reader, func() error, error)
	restore func()
}

func (o *old) save() {
	o.g, o.n, o.h, o.tk, o.pe = newGetter, now, defaultHost, newTicker, podmanEvents
	o.restore = func() { newGetter, now, defaultHost, newTicker, podmanEvents = o.g, o.n, o.h, o.tk, o.pe }
}

func (fx *fixture) deps() Deps {
	return Deps{
		Status: func(p op.RepoParams) op.Operation {
			if p.Repo != "" {
				fx.t.Errorf("status of %q", p.Repo)
			}
			return statusOp{fx}
		},
		ReadIssue: func(ctx context.Context, host, user string, runners op.RunnersReport, repo string, n int) (status.Issue, bool, *op.Error) {
			fx.mu.Lock()
			k := key(repo, n)
			fx.calls = append(fx.calls, k)
			block, unlisted := fx.block, fx.unlisted[k]
			fx.mu.Unlock()
			if host != "github.com" || user != "me" || runners.Repo != "github.com/e/exec" {
				fx.t.Errorf("ReadIssue(%s, %s, %+v)", host, user, runners)
			}
			if block != nil {
				select {
				case <-block:
				case <-ctx.Done():
				}
			}
			is := issues(n)[0]
			is.Title = "re-read"
			return is, !unlisted, nil
		},
	}
}

func (fx *fixture) start() {
	ctx, cancel := context.WithCancel(context.Background())
	fx.cancel, fx.done = cancel, make(chan error, 1)
	go func() { fx.done <- Watch(ctx, fx.deps(), func(e Event) { fx.events <- e }) }()
	fx.t.Cleanup(func() { fx.stop() })
}

func (fx *fixture) stop() error {
	if fx.cancel == nil {
		return nil
	}
	fx.cancel()
	fx.cancel = nil
	select {
	case err := <-fx.done:
		return err
	case <-time.After(5 * time.Second):
		fx.t.Fatal("Watch did not return")
		return nil
	}
}

// next returns the next event other than podman ones.
func (fx *fixture) next() Event {
	fx.t.Helper()
	for {
		select {
		case e := <-fx.events:
			if e.Event == "podman" {
				continue
			}
			return e
		case <-time.After(5 * time.Second):
			fx.t.Fatal("no event")
			return Event{}
		}
	}
}

func (fx *fixture) nextPodman() Event {
	fx.t.Helper()
	for {
		select {
		case e := <-fx.events:
			if e.Event == "podman" {
				return e
			}
		case <-time.After(5 * time.Second):
			fx.t.Fatal("no podman event")
			return Event{}
		}
	}
}

// tick sends one tick and waits until the poll loop took it and the one
// before it (the loop takes the next only after handling the last).
func (fx *fixture) tick() {
	fx.advance(interval)
	fx.ticks <- fx.clock()
}

// drain returns the events available after the poll loop handled every
// tick sent, without waiting for more.
func (fx *fixture) drain() []Event {
	time.Sleep(20 * time.Millisecond)
	var out []Event
	for {
		select {
		case e := <-fx.events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func names(es []Event) []string {
	var out []string
	for _, e := range es {
		if e.Event != "podman" {
			out = append(out, e.Event)
		}
	}
	return out
}

func (fx *fixture) started() {
	fx.t.Helper()
	fx.start()
	if e := fx.next(); e.Event != "snapshot" || e.At != stamp(t0) || e.Result["user"] != "me" {
		fx.t.Fatalf("first event = %+v", e)
	}
	fx.waitPodman()
}

func (fx *fixture) waitPodman() {
	fx.t.Helper()
	if e := fx.nextPodman(); e.Watching == nil || !*e.Watching {
		fx.t.Fatalf("podman = %+v", e)
	}
}

func (fx *fixture) readCount() int {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.reads
}

func TestStartAndQuietTicks(t *testing.T) {
	fx := newFixture(t)
	fx.started()
	gh := fx.gh
	gh.mu.Lock()
	// The assigned Issues' baseline is taken before the read; then the
	// watch set's probes.
	if gh.reqs[0] != assignedPath {
		t.Errorf("first request %s", gh.reqs[0])
	}
	got := append([]string{}, gh.reqs...)
	gh.mu.Unlock()
	if !reflect.DeepEqual(got, []string{assignedPath, runsOne, folderOne, runsExec}) {
		t.Errorf("baselines = %v", got)
	}

	for i := 1; i <= 3; i++ {
		fx.tick()
		if e := fx.next(); e.Event != "checked" || e.At != stamp(t0.Add(time.Duration(i)*interval)) {
			t.Fatalf("tick %d: %+v", i, e)
		}
	}
	if n := fx.readCount(); n != 1 {
		t.Errorf("status reads = %d", n)
	}
	if len(fx.calls) != 0 {
		t.Errorf("ReadIssue calls %v", fx.calls)
	}
	// Every tick sent the last ETag of each probe.
	gh.mu.Lock()
	for _, h := range gh.headers[4:] {
		if strings.HasSuffix(h, " ") {
			t.Errorf("unconditional %q", h)
		}
	}
	if len(gh.reqs) != 4*4 {
		t.Errorf("requests = %d", len(gh.reqs))
	}
	gh.mu.Unlock()
}

func TestFullReadTriggers(t *testing.T) {
	for name, change := range map[string]func(*fixture){
		"assigned": func(fx *fixture) { fx.gh.set(assignedPath, items(3)) },
		"pages": func(fx *fixture) {
			fx.gh.set(assignedPath, items(100))
			fx.gh.set(assigned2, items(1))
		},
		"folder": func(fx *fixture) {
			fx.gh.set(folderOne, []any{map[string]any{"name": "nibfin-issue.yml"}, map[string]any{"name": "nibfin-job.yml"}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t)
			fx.started()
			change(fx)
			fx.repos = []status.RepoIssues{{Repo: "a/one", Issues: issues(3)}, {Repo: "b/two", Issues: issues(1)}}
			fx.tick()
			if e := fx.next(); e.Event != "snapshot" || e.At != stamp(t0.Add(interval)) {
				t.Fatalf("event = %+v", e)
			}
			if n := fx.readCount(); n != 2 {
				t.Errorf("status reads = %d", n)
			}
			if name == "pages" && fx.gh.count(assigned2) != 1 {
				t.Errorf("page 2 read %d times", fx.gh.count(assigned2))
			}
			// The new repository's probes get their baselines; then a quiet
			// tick checks every probe.
			fx.tick()
			if e := fx.next(); e.Event != "checked" {
				t.Fatalf("event = %+v", e)
			}
			if fx.gh.count((&probe{kind: folder, repo: "b/two"}).path(0)) == 0 {
				t.Error("b/two's workflow files are not probed")
			}
			if n := fx.readCount(); n != 2 {
				t.Errorf("status reads = %d", n)
			}
		})
	}
}

func TestRunsMarkNewerListedIssues(t *testing.T) {
	fx := newFixture(t)
	fx.started()
	fx.advance(10 * time.Second)
	fx.gh.set(runsOne, runList(
		[2]string{"Nibfin · a/one#3 · deliver", stamp(t0.Add(5 * time.Second))},
		[2]string{"Nibfin · A/One#3 · wake", stamp(t0.Add(6 * time.Second))},
		[2]string{"Nibfin · a/one#5 · wake", "2026-11-01T00:00:00Z"},
		[2]string{"Nibfin · x/y#1 · wake", stamp(t0.Add(5 * time.Second))},
		[2]string{"CI", stamp(t0.Add(5 * time.Second))},
	))
	fx.tick()
	e := fx.next()
	if e.Event != "issue" || e.Repo != "a/one" || e.Issue == nil || e.Issue.Number != 3 || e.Issue.Title != "re-read" {
		t.Fatalf("event = %+v", e)
	}
	if !reflect.DeepEqual(fx.calls, []string{"a/one#3"}) || fx.readCount() != 1 {
		t.Errorf("calls %v, reads %d", fx.calls, fx.readCount())
	}
	fx.tick()
	if e := fx.next(); e.Event != "checked" {
		t.Fatalf("event = %+v", e)
	}
	if len(fx.calls) != 1 {
		t.Errorf("calls %v", fx.calls)
	}
}

func TestPollIntervalAndNotFound(t *testing.T) {
	fx := newFixture(t)
	fx.gh.polls[folderOne] = 120
	delete(fx.gh.bodies, runsExec) // 404: the shared runner workflow is absent
	fx.started()
	for i := 1; i <= 4; i++ {
		fx.tick()
		fx.drain()
		if got, want := fx.gh.count(folderOne), map[bool]int{true: 2, false: 1}[i == 4]; got != want {
			t.Errorf("tick %d: folder checked %d times, want %d", i, got, want)
		}
		if got := fx.gh.count(assignedPath); got != 1+i {
			t.Errorf("tick %d: assigned checked %d times", i, got)
		}
	}
	if n := fx.gh.count(runsExec); n != 1 {
		t.Errorf("a stopped probe was checked %d times", n)
	}
}

func TestProbeError(t *testing.T) {
	fx := newFixture(t)
	fx.started()
	fx.gh.mu.Lock()
	fx.gh.errs[runsOne] = &api.HTTPError{StatusCode: http.StatusForbidden, Message: "rate limited", Headers: http.Header{"Retry-After": {"90"}}}
	fx.gh.mu.Unlock()
	fx.tick()
	e := fx.next()
	if e.Event != "error" || e.Source != "github" || e.Error == nil || e.Error.Code != op.CodeGitHubAPIFailed || !strings.Contains(e.Error.Error(), "HTTP 403: rate limited") {
		t.Fatalf("event = %+v", e)
	}
	fx.gh.mu.Lock()
	delete(fx.gh.errs, runsOne)
	fx.gh.mu.Unlock()
	// Retry-After 90 s: three ticks after the failure; until then the oldest
	// check does not move, so nothing is sent.
	fx.tick()
	fx.tick()
	if n := fx.gh.count(runsOne); n != 2 {
		t.Errorf("runs checked %d times during Retry-After", n)
	}
	fx.tick()
	if e := fx.next(); e.Event != "checked" || e.At != stamp(t0.Add(4*interval)) {
		t.Fatalf("event = %+v", e)
	}
	if n := fx.gh.count(runsOne); n != 3 {
		t.Errorf("runs checked %d times", n)
	}
	if got := names(fx.drain()); len(got) != 0 {
		t.Errorf("extra events %v", got)
	}
}

func (fx *fixture) podman(line string) {
	fx.t.Helper()
	fx.mu.Lock()
	w := fx.podW
	fx.mu.Unlock()
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		fx.t.Fatal(err)
	}
}

func podLine(status string, attrs map[string]string) string {
	b, _ := json.Marshal(map[string]any{"ID": "abc", "Name": "x", "Status": status, "Type": "container", "Time": "t", "Attributes": attrs})
	return string(b)
}

func labels(repo, issue string) map[string]string {
	return map[string]string{repoLabel: repo, issueLabel: issue, "image": "ghcr.io/x"}
}

func TestPodmanEvents(t *testing.T) {
	fx := newFixture(t)
	fx.started()
	fx.podman(podLine("start", labels("a/other", "3")))
	fx.podman(podLine("create", labels("a/one", "3")))
	fx.podman(podLine("died", labels("a/one", "")))
	fx.podman(`{"Type":"image","Status":"pull","Attributes":{"io.github.taco3064.nibfin.repo":"a/one","io.github.taco3064.nibfin.issue":"3"}}`)
	fx.podman("not json")
	fx.podman(podLine("start", labels("A/One", "5")))
	if e := fx.next(); e.Event != "issue" || e.Issue.Number != 5 {
		t.Fatalf("event = %+v", e)
	}
	fx.podman(podLine("died", labels("a/one", "3")))
	if e := fx.next(); e.Event != "issue" || e.Issue.Number != 3 {
		t.Fatalf("event = %+v", e)
	}
	if !reflect.DeepEqual(fx.calls, []string{"a/one#5", "a/one#3"}) || fx.readCount() != 1 {
		t.Errorf("calls %v, reads %d", fx.calls, fx.readCount())
	}
	// Labels missing from the attributes: a full read.
	fx.podman(podLine("start", map[string]string{"image": "x"}))
	if e := fx.next(); e.Event != "snapshot" {
		t.Fatalf("event = %+v", e)
	}
}

func TestCoalesceAndUnlisted(t *testing.T) {
	fx := newFixture(t)
	fx.started()
	release := make(chan struct{})
	fx.mu.Lock()
	fx.block = release
	fx.mu.Unlock()
	fx.podman(podLine("start", labels("a/one", "3")))
	waitCalls(t, fx, 1)
	fx.podman(podLine("start", labels("a/one", "5")))
	fx.podman(podLine("died", labels("a/one", "3")))
	fx.podman(podLine("start", labels("a/one", "5")))
	time.Sleep(20 * time.Millisecond)
	fx.mu.Lock()
	fx.block = nil
	fx.mu.Unlock()
	close(release)
	for range 3 {
		if e := fx.next(); e.Event != "issue" {
			t.Fatalf("event = %+v", e)
		}
	}
	if !reflect.DeepEqual(fx.calls, []string{"a/one#3", "a/one#3", "a/one#5"}) {
		t.Errorf("calls %v", fx.calls)
	}

	// An Issue no longer listed turns the batch into a full read.
	fx.mu.Lock()
	fx.unlisted["a/one#5"] = true
	fx.mu.Unlock()
	fx.repos = []status.RepoIssues{{Repo: "a/one", Issues: issues(3)}}
	fx.podman(podLine("died", labels("a/one", "5")))
	if e := fx.next(); e.Event != "snapshot" {
		t.Fatalf("event = %+v", e)
	}
	// #5 is not listed any more: its events cause no read.
	fx.podman(podLine("died", labels("a/one", "5")))
	fx.tick()
	if e := fx.next(); e.Event != "checked" {
		t.Fatalf("event = %+v", e)
	}
	if len(fx.calls) != 4 {
		t.Errorf("calls %v", fx.calls)
	}
}

func waitCalls(t *testing.T, fx *fixture, n int) {
	t.Helper()
	for range 500 {
		fx.mu.Lock()
		got := len(fx.calls)
		fx.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("ReadIssue called fewer than %d times", n)
}

func TestPodmanUnavailable(t *testing.T) {
	fx := newFixture(t)
	fx.podErr = op.Errorf(op.CodeMachineStopped, "podman machine m is stopped")
	fx.start()
	if e := fx.next(); e.Event != "snapshot" {
		t.Fatalf("event = %+v", e)
	}
	e := fx.nextPodman()
	if e.Watching == nil || *e.Watching || e.Error == nil || e.Error.Code != op.CodeMachineStopped {
		t.Fatalf("podman = %+v", e)
	}
	// Retried on each tick; the same failure is not sent again.
	fx.tick()
	fx.next()
	fx.tick()
	fx.next()
	fx.mu.Lock()
	calls := fx.podCalls
	fx.podErr = nil
	fx.mu.Unlock()
	if calls != 3 {
		t.Errorf("podman events started %d times", calls)
	}
	fx.tick()
	fx.waitPodman()
	// The process ending is sent, and the next tick follows again.
	fx.mu.Lock()
	fx.podW.Close()
	fx.mu.Unlock()
	if e := fx.nextPodman(); *e.Watching || e.Error == nil || e.Error.Code != op.CodePodmanFailed {
		t.Fatalf("podman = %+v", e)
	}
	fx.tick()
	fx.waitPodman()
}

func TestCancelEndsWatch(t *testing.T) {
	fx := newFixture(t)
	fx.started()
	fx.mu.Lock()
	pctx := fx.podCtx
	fx.mu.Unlock()
	if err := fx.stop(); err != nil {
		t.Errorf("Watch = %v", err)
	}
	if pctx.Err() == nil {
		t.Error("podman events still running")
	}
}

func TestFirstReadFails(t *testing.T) {
	fx := newFixture(t)
	fx.readErr = op.Errorf(op.CodeAuthUnavailable, "no login")
	fx.start()
	select {
	case err := <-fx.done:
		fx.cancel = nil
		if op.Code(err) != op.CodeAuthUnavailable {
			t.Errorf("Watch = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not return")
	}
}

func TestLaterReadFails(t *testing.T) {
	fx := newFixture(t)
	fx.started()
	fx.mu.Lock()
	fx.readErr = op.Errorf(op.CodeGitHubAPIFailed, "down")
	fx.mu.Unlock()
	fx.gh.set(assignedPath, items(5))
	fx.tick()
	if e := fx.next(); e.Event != "error" || e.Source != "status" || e.Error.Code != op.CodeGitHubAPIFailed {
		t.Fatalf("event = %+v", e)
	}
}

// TestConditionalClient sends a probe through go-gh's client: the request
// carries the login's Authorization and If-None-Match, and a 304 is an
// answer with its headers.
func TestConditionalClient(t *testing.T) {
	var got []http.Header
	rt := roundTrip(func(r *http.Request) (*http.Response, error) {
		got = append(got, r.Header.Clone())
		h := http.Header{"Etag": {`"b"`}, "Date": {t0.Format(http.TimeFormat)}, "X-Poll-Interval": {"60"}}
		code, body := http.StatusOK, "[]"
		if r.Header.Get("If-None-Match") == `"b"` {
			code, body = http.StatusNotModified, ""
		} else {
			h.Set("Content-Type", "application/json")
		}
		return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	g, err := restGetterFor(api.ClientOptions{Host: "github.com", AuthToken: "fake-token"}, rt)
	if err != nil {
		t.Fatal(err)
	}
	a, err := g.get(context.Background(), assignedPath, "")
	if err != nil || a.status != 200 || a.etag != `"b"` || string(a.body) != "[]" || !a.date.Equal(t0) || a.poll != time.Minute {
		t.Fatalf("200 = %+v, %v", a, err)
	}
	a, err = g.get(context.Background(), assignedPath, `"b"`)
	if err != nil || a.status != 304 || a.etag != `"b"` || a.body != nil {
		t.Fatalf("304 = %+v, %v", a, err)
	}
	if got[0].Get("If-None-Match") != "" || got[1].Get("If-None-Match") != `"b"` {
		t.Errorf("If-None-Match = %q, %q", got[0].Get("If-None-Match"), got[1].Get("If-None-Match"))
	}
	for _, h := range got {
		if h.Get("Authorization") != "token fake-token" {
			t.Errorf("Authorization = %q", h.Get("Authorization"))
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRetryAfter(t *testing.T) {
	h := func(v string) error { return &api.HTTPError{StatusCode: 429, Headers: http.Header{"Retry-After": {v}}} }
	if d := retryAfter(h("90"), t0); d != 90*time.Second {
		t.Errorf("seconds = %v", d)
	}
	if d := retryAfter(h(t0.Add(time.Minute).Format(http.TimeFormat)), t0); d != time.Minute {
		t.Errorf("date = %v", d)
	}
	if d := retryAfter(errors.New("x"), t0); d != 0 {
		t.Errorf("other = %v", d)
	}
	if ticksFor(90*time.Second) != 3 || ticksFor(0) != 1 || ticksFor(120*time.Second) != 4 {
		t.Error("ticksFor")
	}
}

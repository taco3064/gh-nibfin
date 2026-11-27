package status

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
)

// A busy execution repository (#495): busyRuns shared-runner runs in the
// window, newest first, one every busyStep; the newest busyRunning are in
// progress, one on each Runner. The listing stops at GitHub's 1,000.
const (
	busyRuns    = 1500
	busyRunning = 4
	busyStep    = 50 * time.Second
)

func busyCreated(id int) time.Time { return actNow.Add(-time.Duration(id) * busyStep) }

func busyRun(id int) map[string]any {
	st := "completed"
	if id <= busyRunning {
		st = "in_progress"
	}
	return runItem(id, fmt.Sprintf("Nibfin · acme/app#%d · deliver", id), st, 1, busyCreated(id).Format(time.RFC3339))
}

// busyGH answers the run listings and the jobs requests of a busy
// repository. It counts the jobs requests per run and in flight, can hold
// the in-progress and the completed runs' jobs requests until a gate
// closes, and can fail chosen runs' jobs requests (failOnce only the first
// time).
type busyGH struct {
	t        *testing.T
	gateCur  chan struct{}
	gateHist chan struct{}
	fail     map[int64]bool
	failOnce map[int64]bool

	mu                 sync.Mutex
	lists              []string
	jobs               map[int64]int
	cur, hist, all     int
	maxCur, maxHist, n int
}

func newBusyGH(t *testing.T) *busyGH {
	return &busyGH{t: t, fail: map[int64]bool{}, failOnce: map[int64]bool{}, jobs: map[int64]int{}}
}

func (b *busyGH) DoWithContext(ctx context.Context, method, p string, body io.Reader, out any) error {
	if method != http.MethodGet || body != nil {
		b.t.Errorf("activity sent %s %s", method, p)
		return fmt.Errorf("write")
	}
	u, err := url.Parse(p)
	if err != nil {
		return err
	}
	q := u.Query()
	if strings.HasSuffix(u.Path, "/actions/workflows/nibfin-shared-runner.yml/runs") {
		b.mu.Lock()
		b.lists = append(b.lists, u.RawQuery)
		b.mu.Unlock()
		page, _ := strconv.Atoi(q.Get("page"))
		runs := []any{}
		switch {
		case q.Get("status") == "in_progress":
			if page == 1 {
				for id := 1; id <= busyRunning; id++ {
					runs = append(runs, busyRun(id))
				}
			}
		case q.Get("created") == createdQ:
			// GitHub lists at most 1,000 results.
			for id := (page-1)*perPage + 1; id <= page*perPage && id <= busyRuns && page <= maxResults/perPage; id++ {
				runs = append(runs, busyRun(id))
			}
		default:
			b.t.Errorf("unexpected listing %s", p)
		}
		return remarshal(b.t, map[string]any{"workflow_runs": runs}, out)
	}
	var id int64
	if _, err := fmt.Sscanf(u.Path, "repos/o/r/actions/runs/%d/jobs", &id); err != nil {
		b.t.Errorf("unexpected request %s", p)
		return err
	}
	running := id <= busyRunning
	b.mu.Lock()
	b.jobs[id]++
	b.all++
	b.n = max(b.n, b.all)
	if running {
		b.cur++
		b.maxCur = max(b.maxCur, b.cur)
	} else {
		b.hist++
		b.maxHist = max(b.maxHist, b.hist)
	}
	fail := b.fail[id] || b.failOnce[id]
	delete(b.failOnce, id)
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.all--
		if running {
			b.cur--
		} else {
			b.hist--
		}
		b.mu.Unlock()
	}()
	gate := b.gateHist
	if running {
		gate = b.gateCur
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	time.Sleep(200 * time.Microsecond)
	if fail {
		return &api.HTTPError{StatusCode: http.StatusBadGateway, Message: "Bad Gateway"}
	}
	created := busyCreated(int(id))
	var j map[string]any
	if running {
		j = job(fmt.Sprintf("h-claude-%d", id), "in_progress", "", created.Add(10*time.Second).Format(time.RFC3339), "")
	} else {
		j = job(fmt.Sprintf("h-claude-%d", id%4+1), "completed", "success", created.Add(5*time.Second).Format(time.RFC3339), created.Add(40*time.Second).Format(time.RFC3339))
	}
	return remarshal(b.t, map[string]any{"jobs": []any{j}}, out)
}

func remarshal(t *testing.T, v, out any) error {
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return json.Unmarshal(data, out)
}

var fourSlots = op.HostSlots{
	Repo: "github.com/o/r",
	Slots: []op.SlotDir{
		{Slot: "claude-1", Provider: "claude", Name: "h-claude-1"},
		{Slot: "claude-2", Provider: "claude", Name: "h-claude-2"},
		{Slot: "claude-3", Provider: "claude", Name: "h-claude-3"},
		{Slot: "claude-4", Provider: "claude", Name: "h-claude-4"},
	},
}

// useBusy points the activity reads at b and reports every start or join
// of a read in flight on joined.
func useBusy(t *testing.T, b *busyGH) chan string {
	t.Helper()
	setupActivity(t, map[string]any{})
	useSlots(t, fourSlots)
	newREST = func(string) (templates.RESTClient, error) { return b, nil }
	joined := make(chan string, 16)
	old := flightJoined
	t.Cleanup(func() { flightJoined = old })
	flightJoined = func(part string) { joined <- part }
	return joined
}

// expect waits for the starts or joins of reads in flight of the parts
// want, in any order: a caller and the read it starts report concurrently.
func expect(t *testing.T, joined chan string, want ...string) {
	t.Helper()
	left := map[string]int{}
	for _, w := range want {
		left[w]++
	}
	for range want {
		select {
		case got := <-joined:
			if left[got] == 0 {
				t.Fatalf("joined %s, want %v", got, want)
			}
			left[got]--
		case <-time.After(10 * time.Second):
			t.Fatalf("no read of %v joined", want)
		}
	}
}

type answer struct {
	act Activity
	err error
}

func goRead(read func(context.Context, time.Time) (Activity, error)) chan answer {
	ch := make(chan answer, 1)
	go func() {
		act, err := read(context.Background(), actNow)
		ch <- answer{act, err}
	}()
	return ch
}

func wait(t *testing.T, ch chan answer) Activity {
	t.Helper()
	select {
	case a := <-ch:
		if a.err != nil {
			t.Fatalf("read: %v", a.err)
		}
		return a.act
	case <-time.After(30 * time.Second):
		t.Fatal("read did not answer")
		return Activity{}
	}
}

func checkCurrents(t *testing.T, what string, act Activity) {
	t.Helper()
	if act.JobsError != nil || len(act.Runners) != 4 {
		t.Fatalf("%s = %+v", what, act)
	}
	for i, r := range act.Runners {
		want := fmt.Sprintf("acme/app#%d deliver", i+1)
		if c := r.Current; c == nil || c.Source != "github" || fmt.Sprintf("%s#%d %s", c.Repo, c.Issue, c.Stage) != want {
			t.Errorf("%s %s current = %+v, want %s", what, r.Slot, r.Current, want)
		}
	}
}

func TestActivityBusyRepository(t *testing.T) {
	b := newBusyGH(t)
	b.gateCur, b.gateHist = make(chan struct{}), make(chan struct{})
	joined := useBusy(t, b)
	a := NewActivityReader()

	// The view asks for the current Jobs, then for the full activity; a
	// second full read arrives while the first is in flight.
	curCh := goRead(a.ReadCurrent)
	expect(t, joined, partCurrent)
	full1 := goRead(a.ReadActivity)
	// The full read joins the current read in flight instead of reading
	// the in-progress runs again.
	expect(t, joined, partHistory, partCurrent)
	full2 := goRead(a.ReadActivity)
	expect(t, joined, partHistory)

	// The current Jobs answer while the history is still held.
	close(b.gateCur)
	cur := wait(t, curCh)
	checkCurrents(t, "current", cur)
	if cur.History || cur.CoversFrom != "" {
		t.Errorf("current history = %v, coversFrom = %q", cur.History, cur.CoversFrom)
	}
	select {
	case <-full1:
		t.Fatal("the full read answered before the history was read")
	case <-full2:
		t.Fatal("the full read answered before the history was read")
	default:
	}

	close(b.gateHist)
	act1, act2 := wait(t, full1), wait(t, full2)
	checkCurrents(t, "full", act1)
	if !act1.History || act1.To != act2.To || len(act2.Runners) != 4 || !act2.History {
		t.Errorf("full reads = %+v / %+v", act1, act2)
	}
	// The window listing stopped at 1,000 runs: the periods reach back to
	// the earliest listed run only.
	if want := busyCreated(maxResults).Format(time.RFC3339); act1.CoversFrom != want {
		t.Errorf("coversFrom = %q, want %q", act1.CoversFrom, want)
	}
	periods := 0
	for _, r := range act1.Runners {
		periods += len(r.Periods)
	}
	if periods != maxResults {
		t.Errorf("periods = %d, want %d", periods, maxResults)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	// No run's jobs were requested twice by the overlapping reads.
	if len(b.jobs) != maxResults {
		t.Errorf("jobs read for %d runs, want %d", len(b.jobs), maxResults)
	}
	for id, n := range b.jobs {
		if n != 1 {
			t.Errorf("run %d's jobs requested %d times", id, n)
		}
	}
	// The window was listed once (10 pages) and the runs in progress once.
	if len(b.lists) != maxResults/perPage+1 {
		t.Errorf("listings = %q", b.lists)
	}
	// The jobs requests stay within the limits, and the history read uses
	// its whole share.
	if b.maxCur > currentJobsLimit || b.maxHist != historyJobsLimit || b.n > historyJobsLimit+currentJobsLimit {
		t.Errorf("in flight: current %d, history %d, all %d", b.maxCur, b.maxHist, b.n)
	}

	// A later full read reads only the runs in progress again: the
	// completed runs' jobs come from memory.
	b.jobs = map[int64]int{}
	b.mu.Unlock()
	act3, err := a.ReadActivity(context.Background(), actNow)
	b.mu.Lock()
	if err != nil || act3.JobsError != nil {
		t.Fatalf("third read = %+v, %v", act3, err)
	}
	if len(b.jobs) != busyRunning {
		t.Errorf("third read requested the jobs of %d runs", len(b.jobs))
	}
}

func TestActivityHistoryFails(t *testing.T) {
	b := newBusyGH(t)
	b.gateCur = make(chan struct{})
	b.fail[700] = true
	joined := useBusy(t, b)
	a := NewActivityReader()

	curCh := goRead(a.ReadCurrent)
	expect(t, joined, partCurrent)
	fullCh := goRead(a.ReadActivity)
	expect(t, joined, partHistory, partCurrent)
	close(b.gateCur)

	// A history failure fails only the full read.
	checkCurrents(t, "current", wait(t, curCh))
	full := wait(t, fullCh)
	if full.JobsError == nil || full.JobsError.Code != op.CodeGitHubAPIFailed || !strings.Contains(full.JobsError.Error(), "run 700") || !full.History {
		t.Fatalf("full jobsError = %+v", full.JobsError)
	}
	if full.CoversFrom != "" {
		t.Errorf("failed read coversFrom = %q", full.CoversFrom)
	}
	for _, r := range full.Runners {
		if r.Current != nil || len(r.Periods) != 0 {
			t.Errorf("failed read %s = %+v", r.Slot, r)
		}
	}
	// The current read still answers on its own.
	cur, err := a.ReadCurrent(context.Background(), actNow)
	if err != nil {
		t.Fatal(err)
	}
	checkCurrents(t, "later current", cur)
}

func TestActivityCurrentFails(t *testing.T) {
	b := newBusyGH(t)
	b.failOnce[1] = true
	useBusy(t, b)
	a := NewActivityReader()

	// The full read's current read fails; the full read then lists and
	// reads the runs in progress itself.
	full, err := a.ReadActivity(context.Background(), actNow)
	if err != nil {
		t.Fatal(err)
	}
	checkCurrents(t, "full", full)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.jobs[1] != 2 {
		t.Errorf("run 1's jobs requested %d times, want 2", b.jobs[1])
	}
	running := 0
	for _, l := range b.lists {
		if strings.Contains(l, "status=in_progress") {
			running++
		}
	}
	if running != 2 {
		t.Errorf("in-progress listings = %d, want 2", running)
	}
}

func TestActivityCallerLeaves(t *testing.T) {
	b := newBusyGH(t)
	b.gateHist = make(chan struct{})
	joined := useBusy(t, b)
	a := NewActivityReader()

	ctx, cancel := context.WithCancel(context.Background())
	left := make(chan error, 1)
	go func() {
		_, err := a.ReadActivity(ctx, actNow)
		left <- err
	}()
	expect(t, joined, partHistory, partCurrent)
	other := goRead(a.ReadActivity)
	expect(t, joined, partHistory)
	// The first caller leaves: it gets its own error, the read goes on for
	// the one still waiting.
	cancel()
	if err := <-left; err != context.Canceled {
		t.Fatalf("left = %v", err)
	}
	close(b.gateHist)
	act := wait(t, other)
	if act.JobsError != nil || !act.History {
		t.Fatalf("joined read = %+v", act)
	}
}

package op

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// recorder collects progress events as "phase id" lines, and the events.
type recorder struct {
	mu     sync.Mutex
	events []Event
}

func record(ctx context.Context) (context.Context, *recorder) {
	r := &recorder{}
	return WithProgress(ctx, func(e Event) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }), r
}

func (r *recorder) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for _, e := range r.events {
		out = append(out, e.Phase+" "+e.ID)
	}
	return out
}

func (r *recorder) find(phase, id string) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Phase == phase && e.ID == id {
			return e, true
		}
	}
	return Event{}, false
}

func TestTrack(t *testing.T) {
	ctx, rec := record(context.Background())
	a := Step{ID: "a", Action: "do", Target: "x"}
	b := Step{ID: "b", Action: "do", Target: "y"}
	if err := Track(ctx, a, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	boom := Errorf(CodeGitHubError, "boom")
	if err := Track(ctx, b, func() error { return boom }); err != boom {
		t.Fatalf("Track changed the error: %v", err)
	}
	if want := []string{"started a", "done a", "started b", "failed b"}; !reflect.DeepEqual(rec.lines(), want) {
		t.Errorf("events = %q", rec.lines())
	}
	if e, _ := rec.find(PhaseFailed, "b"); e.Code != CodeGitHubError || e.Message != "boom" || e.Action != "do" || e.Target != "y" {
		t.Errorf("failed event = %+v", e)
	}
	if e, _ := rec.find(PhaseDone, "a"); e.Code != "" || e.Message != "" {
		t.Errorf("done event carries an error: %+v", e)
	}
}

func TestTrackWithoutCallback(t *testing.T) {
	ran := false
	if err := Track(context.Background(), Step{ID: "a"}, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("ran %v, err %v", ran, err)
	}
}

func TestTrackAllAndBatch(t *testing.T) {
	ctx, rec := record(context.Background())
	steps := []Step{{ID: "a"}, {ID: "b"}}
	err := TrackAll(ctx, steps, func() error { return errors.New("plain") })
	if err == nil {
		t.Fatal("no error")
	}
	if want := []string{"started a", "started b", "failed a", "failed b"}; !reflect.DeepEqual(rec.lines(), want) {
		t.Errorf("events = %q", rec.lines())
	}
	// A plain error still fails with a code.
	if e, _ := rec.find(PhaseFailed, "a"); e.Code == "" || e.Message == "" {
		t.Errorf("failed event = %+v", e)
	}

	ctx, rec = record(context.Background())
	b := StartAll(ctx, []Step{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	b.Done("b")
	b.Fail("a", Errorf(CodeNotApplied, "a failed"))
	b.Done("a") // already finished
	b.Abort(Errorf(CodeRunnerInstallFailed, "stop"))
	want := []string{"started a", "started b", "started c", "done b", "failed a", "failed c"}
	if !reflect.DeepEqual(rec.lines(), want) {
		t.Errorf("events = %q", rec.lines())
	}
	if e, _ := rec.find(PhaseFailed, "c"); e.Code != CodeRunnerInstallFailed || !strings.Contains(e.Message, "stopped before this item finished") {
		t.Errorf("aborted event = %+v", e)
	}
}

func TestChecks(t *testing.T) {
	var c Checks
	a, b, d := Step{ID: "a", Action: "x", Target: "1"}, Step{ID: "b"}, Step{ID: "d"}
	c.Expect(a, b, d)
	c.Set(a, true, "present", "ignored")
	c.Drop("d")
	err := Errorf(CodeGitHubError, "read failed")
	r := c.Fill(Result{"k": 1}, nil, err)
	items := r[ResultItems].([]Check)
	want := []Check{
		{ID: "a", Action: "x", Target: "1", Confirmed: true, State: "present"},
		{ID: "b", Reason: "not read back: read failed"},
	}
	if !reflect.DeepEqual(items, want) || r["k"] != 1 {
		t.Errorf("items = %+v", items)
	}
	if notes, ok := r[ResultNotes].([]string); !ok || len(notes) != 0 {
		t.Errorf("notes = %#v", r[ResultNotes])
	}
	c.Set(b, false, "absent", "")
	if got := c.List(nil)[1]; got.Confirmed || got.State != "absent" || got.Reason == "" {
		t.Errorf("not confirmed item = %+v", got)
	}
}

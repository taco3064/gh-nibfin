package op

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// stepByID returns the plan item id.
func stepByID(t *testing.T, p Plan, id string) Step {
	t.Helper()
	for _, s := range p.Steps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no item %q in %+v", id, p.Steps)
	return Step{}
}

// checkByID returns the read-back item id.
func checkByID(t *testing.T, r Result, id string) Check {
	t.Helper()
	items, _ := r[ResultItems].([]Check)
	for _, c := range items {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no item %q in %+v", id, items)
	return Check{}
}

func uniqueIDs(t *testing.T, p Plan) {
	t.Helper()
	seen := map[string]bool{}
	for _, s := range p.Steps {
		if s.ID == "" || seen[s.ID] {
			t.Errorf("item id %q empty or repeated in %+v", s.ID, p.Steps)
		}
		seen[s.ID] = true
	}
}

func TestRunnerAddItems(t *testing.T) {
	// Linger, the tools volume and the login are in place: no change.
	f := &fakeHost{probe: strings.Replace(defaultProbe, "linger 0", "linger 1", 1), slots: map[string]*fakeSlot{},
		volumes: map[string]bool{"nibfin-tools": true, "claude": true}, loggedIn: map[string]bool{"claude": true}}
	useRunnerHost(t, f)
	ctx, rec := record(context.Background())
	p, r, err := Run(ctx, RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil {
		t.Fatal(err)
	}
	uniqueIDs(t, p)
	for _, id := range []string{"host", "linger", "volume:nibfin-tools"} {
		if s := stepByID(t, p, id); !s.NoChange {
			t.Errorf("%s is a change: %+v", id, s)
		}
	}
	slot := stepByID(t, p, "runner:claude-1")
	if slot.NoChange || slot.Current != "absent" || !strings.HasPrefix(slot.Next, "configured and registered to github.com/cur/repo") {
		t.Errorf("slot item = %+v", slot)
	}
	// Only the slot is applied.
	if want := []string{"started runner:claude-1", "done runner:claude-1"}; !reflect.DeepEqual(rec.lines(), want) {
		t.Errorf("events = %q", rec.lines())
	}
	for _, id := range []string{"host", "linger", "volume:nibfin-tools", "runner:claude-1"} {
		if c := checkByID(t, r, id); !c.Confirmed {
			t.Errorf("%s not confirmed: %+v", id, c)
		}
	}
}

func TestRunnerAddStoppedMachineItems(t *testing.T) {
	f := &fakeHost{stopped: true, vm: true, slots: map[string]*fakeSlot{}, loginWorks: true,
		volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useRunnerHost(t, f)
	o := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2, Interact: &recInteract{}})
	p, err := o.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s := stepByID(t, p, "runners"); !s.AfterStart {
		t.Errorf("runners item = %+v", s)
	}
	if s := stepByID(t, p, "linger"); !s.AfterStart {
		t.Errorf("linger item = %+v", s)
	}
	ctx, rec := record(context.Background())
	if err := o.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	// The slots chosen after the start report their own items.
	for _, id := range []string{"host", "runner:claude-1", "runner:claude-2"} {
		if e, ok := rec.find(PhaseDone, id); !ok || e.Action == "" || e.Target == "" {
			t.Errorf("no done event for %s: %q", id, rec.lines())
		}
	}
	r, err := o.ReadBack(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := r[ResultItems].([]Check)
	for _, c := range items {
		if c.ID == "runners" {
			t.Errorf("read-back reports the placeholder: %+v", items)
		}
	}
	checkByID(t, r, "runner:claude-2")
}

func TestRunnerAddFailedItem(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, loggedIn: map[string]bool{"claude": true}, volumes: map[string]bool{"claude": true}}
	fx := useRunnerHost(t, f)
	fx.gh.downloads[1].SHA256 = shaHex("tampered")
	ctx, rec := record(context.Background())
	_, _, err := Run(ctx, RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerDownloadFailed {
		t.Fatalf("err = %v", err)
	}
	if e, ok := rec.find(PhaseFailed, "runner:claude-1"); !ok || e.Code != CodeRunnerDownloadFailed {
		t.Errorf("events = %+v", rec.events)
	}
	if _, ok := rec.find(PhaseDone, "runner:claude-1"); ok {
		t.Error("the failed slot is reported done")
	}
}

func TestRunnerAddReadBackItemsWithError(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, loggedIn: map[string]bool{"claude": true}, volumes: map[string]bool{"claude": true}}
	fx := useRunnerHost(t, f)
	fx.gh.unlisted = true
	_, r, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerMissing || r == nil {
		t.Fatalf("r %v, err %v", r, err)
	}
	if c := checkByID(t, r, "runner:claude-1"); c.Confirmed || c.Reason == "" {
		t.Errorf("slot = %+v", c)
	}
	if c := checkByID(t, r, "host"); !c.Confirmed {
		t.Errorf("host = %+v", c)
	}
}

func TestRunnerRemoveItems(t *testing.T) {
	f, _ := removeHost(t)
	ctx, rec := record(context.Background())
	p, r, err := Run(ctx, RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil {
		t.Fatalf("%v (events %v)", err, f.events)
	}
	uniqueIDs(t, p)
	if s := stepByID(t, p, "host"); !s.NoChange {
		t.Errorf("host = %+v", s)
	}
	slot := stepByID(t, p, "runner:claude-2")
	if slot.NoChange || slot.Current == "" {
		t.Errorf("slot = %+v", slot)
	}
	if want := []string{"started runner:claude-2", "done runner:claude-2"}; !reflect.DeepEqual(rec.lines(), want) {
		t.Errorf("events = %q", rec.lines())
	}
	if c := checkByID(t, r, "runner:claude-2"); !c.Confirmed {
		t.Errorf("slot = %+v", c)
	}

	// A slot left on the host is not confirmed, next to not_applied.
	f, _ = removeHost(t)
	f.purgeNoop = true
	_, r, err = Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeNotApplied || r == nil {
		t.Fatalf("r %v, err %v", r, err)
	}
	if c := checkByID(t, r, "runner:claude-2"); c.Confirmed || !strings.Contains(c.Reason, "still on the host") {
		t.Errorf("slot = %+v", c)
	}
}

func TestLoginItems(t *testing.T) {
	f := &fakeHost{volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useHost(t, f)
	p, err := Login(LoginParams{}).Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	uniqueIDs(t, p)
	// claude is in place; codex needs its volume and a login.
	var noChange, change []string
	for _, s := range p.Steps {
		if s.NoChange {
			noChange = append(noChange, s.Action+" "+s.Target)
		} else {
			change = append(change, s.Action+" "+s.Target)
		}
	}
	if !reflect.DeepEqual(change, []string{"create volume nibfin-home-codex", "log in codex"}) || len(noChange) < 2 {
		t.Errorf("no change %q, change %q", noChange, change)
	}

	// Every item in place: a read-only plan whose items are confirmed.
	f = &fakeHost{volumes: map[string]bool{"claude": true, "codex": true}, loggedIn: map[string]bool{"claude": true, "codex": true}}
	useHost(t, f)
	p, r, err := Run(context.Background(), Login(LoginParams{}), nil)
	if err != nil || !p.ReadOnly {
		t.Fatalf("plan %+v, err %v", p, err)
	}
	items, _ := r[ResultItems].([]Check)
	if len(items) == 0 {
		t.Fatalf("no items: %v", r)
	}
	for _, c := range items {
		if !c.Confirmed {
			t.Errorf("not confirmed: %+v", c)
		}
	}
}

func TestRunnerRemoveFailedItems(t *testing.T) {
	f, _ := removeHost(t)
	f.shellExit = map[string]int{"stop": 1}
	f.shellErr = map[string]string{"stop": "boom"}
	ctx, rec := record(context.Background())
	p, _, err := Run(ctx, RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 2}), &yes{})
	if Code(err) != CodeRunnerRemoveFailed {
		t.Fatalf("err = %v", err)
	}
	var slots []string
	for _, s := range p.Changes() {
		if strings.HasPrefix(s.ID, "runner:") {
			slots = append(slots, s.ID)
		}
	}
	if len(slots) != 2 {
		t.Fatalf("slots = %q", slots)
	}
	// The slot whose stop failed fails with that error; the other one was
	// started and is aborted with the same code; none is done.
	var failed, aborted int
	for _, id := range slots {
		if _, ok := rec.find(PhaseStarted, id); !ok {
			t.Errorf("%s not started: %q", id, rec.lines())
		}
		if _, ok := rec.find(PhaseDone, id); ok {
			t.Errorf("%s done: %q", id, rec.lines())
		}
		e, ok := rec.find(PhaseFailed, id)
		switch {
		case !ok || e.Code != CodeRunnerRemoveFailed:
			t.Errorf("%s = %+v", id, e)
		case strings.Contains(e.Message, "stopped before this item finished"):
			aborted++
		default:
			failed++
		}
	}
	if failed != 1 || aborted != 1 {
		t.Errorf("failed %d, aborted %d: %+v", failed, aborted, rec.events)
	}
}

func TestLoginProgress(t *testing.T) {
	f := &fakeHost{loginWorks: true, want: "the-code"}
	useHost(t, f)
	in := &recInteract{code: "the-code"}
	// Each event records how many login steps were sent before it.
	var at []string
	ctx := WithProgress(context.Background(), func(e Event) {
		at = append(at, e.Phase+" "+e.ID+" @"+string(rune('0'+len(in.steps))))
	})
	if _, _, err := Run(ctx, Login(LoginParams{Provider: "claude", Interact: in}), nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"started volume:nibfin-home-claude @0", "done volume:nibfin-home-claude @0", "started login:claude @0", "done login:claude @3"}
	if !reflect.DeepEqual(at, want) {
		t.Errorf("events = %q", at)
	}
}

func TestLoginFailedItem(t *testing.T) {
	f := &fakeHost{want: "right", volumes: map[string]bool{"claude": true}}
	useHost(t, f)
	ctx, rec := record(context.Background())
	_, _, err := Run(ctx, Login(LoginParams{Provider: "claude", Interact: &recInteract{code: "wrong"}}), nil)
	if Code(err) != CodeLoginFailed {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"started login:claude", "failed login:claude"}; !reflect.DeepEqual(rec.lines(), want) {
		t.Errorf("events = %q", rec.lines())
	}
	if e, _ := rec.find(PhaseFailed, "login:claude"); e.Code != CodeLoginFailed || e.Message == "" {
		t.Errorf("failed = %+v", e)
	}
}

func TestLoginReadBackItems(t *testing.T) {
	// A named login that did not take: not confirmed, next to not_logged_in.
	f := &fakeHost{volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": false}}
	useHost(t, f)
	o := Login(LoginParams{Provider: "claude", Interact: &recInteract{}})
	p, err := o.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, err := o.ReadBack(context.Background(), p)
	if Code(err) != CodeNotLoggedIn || r == nil {
		t.Fatalf("r %v, err %v", r, err)
	}
	if c := checkByID(t, r, "volume:nibfin-home-claude"); !c.Confirmed {
		t.Errorf("volume = %+v", c)
	}
	if c := checkByID(t, r, "login:claude"); c.Confirmed || c.Reason == "" {
		t.Errorf("login = %+v", c)
	}

	// The machine stopped: the host is not confirmed and the items not
	// read are reported with the error.
	f.stopped = true
	r, err = o.ReadBack(context.Background(), p)
	if Code(err) != CodePodmanFailed || r == nil {
		t.Fatalf("r %v, err %v", r, err)
	}
	if c := checkByID(t, r, "host"); c.Confirmed {
		t.Errorf("host = %+v", c)
	}
	if c := checkByID(t, r, "login:claude"); c.Confirmed || !strings.Contains(c.Reason, "not read back") {
		t.Errorf("login = %+v", c)
	}
}

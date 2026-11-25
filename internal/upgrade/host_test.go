package upgrade

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
)

// fakeHostPart is the host part of upgrade: steps and notes are its plan;
// it records the order of its calls into log.
type fakeHostPart struct {
	steps   []op.Step
	notes   []string
	planErr error
	applied []op.Step
	log     *[]string
	// readErr fails the read-back, which then confirms no item.
	readErr error
	// onApply runs when Apply starts.
	onApply func()
}

func (f *fakeHostPart) record(s string) {
	if f.log != nil {
		*f.log = append(*f.log, s)
	}
}

func (f *fakeHostPart) Plan(context.Context) ([]op.Step, []string, error) {
	f.record("host plan")
	return f.steps, f.notes, f.planErr
}

func (f *fakeHostPart) Apply(_ context.Context, steps []op.Step) error {
	f.record("host apply")
	if f.onApply != nil {
		f.onApply()
	}
	f.applied = steps
	return nil
}

func (f *fakeHostPart) ReadBack(_ context.Context, checks *op.Checks, r op.Result) error {
	f.record("host read back")
	for _, s := range f.steps {
		checks.Set(s, f.readErr == nil, "current", "differs")
	}
	if f.steps != nil {
		r["runners"] = map[string]any{"repo": repo}
	}
	return f.readErr
}

func useHostPart(t *testing.T, f *fakeHostPart) {
	t.Helper()
	old := hostPart
	hostPart = func() hostUpgrade { return f }
	t.Cleanup(func() { hostPart = old })
}

var hostSteps = []op.Step{
	{ID: "runner-env:claude-1", Action: "set provider in .env", Target: "claude-1"},
	{ID: "runner-unit:claude-1", Action: "update service", Target: "claude-1", NoChange: true},
}

func TestCurrentRepoUpgradesTheHost(t *testing.T) {
	files := render(t, templates.Peer, imageRef)
	f := newFake(files)
	stub(t, f)
	h := &fakeHostPart{steps: hostSteps, notes: []string{"labels left as they are"}}
	useHostPart(t, h)
	c, p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	if !c.asked || p.ReadOnly || !p.Confirm || len(f.writes()) != 0 {
		t.Errorf("asked %v, plan %+v, writes %v", c.asked, p, f.writes())
	}
	if !reflect.DeepEqual(h.applied, hostSteps[:1]) {
		t.Errorf("host applied %+v", h.applied)
	}
	if !reflect.DeepEqual(p.Notes, h.notes) || !reflect.DeepEqual(r[op.ResultNotes], h.notes) {
		t.Errorf("notes %q / %v", p.Notes, r[op.ResultNotes])
	}
	items := checks(t, r)
	if len(items) != len(p.Steps) || r["runners"] == nil || r["current"] != true {
		t.Errorf("items %+v, result %v", items, r)
	}
	for _, c := range items {
		if !c.Confirmed {
			t.Errorf("not confirmed %+v", c)
		}
	}
}

func TestBothCurrentIsReadOnly(t *testing.T) {
	f := newFake(render(t, templates.Peer, imageRef))
	stub(t, f)
	h := &fakeHostPart{steps: hostSteps[1:]}
	useHostPart(t, h)
	c, p, _, err := run(t)
	if err != nil || c.asked || !p.ReadOnly || p.Confirm || h.applied != nil {
		t.Errorf("err %v, asked %v, plan %+v, applied %+v", err, c.asked, p, h.applied)
	}
}

func TestRepositoryPartComesFirst(t *testing.T) {
	f := newFake(stale(t, templates.Peer))
	stub(t, f)
	var log []string
	h := &fakeHostPart{steps: hostSteps, log: &log}
	h.onApply = func() { log = append(log, "prs "+strconv.Itoa(len(f.prs))) }
	useHostPart(t, h)
	_, p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"host plan", "host apply", "prs 1", "host read back"}
	if !reflect.DeepEqual(log, want) {
		t.Errorf("log %q", log)
	}
	// The repository steps come first, then the host steps.
	n := len(p.Steps)
	if p.Steps[n-2].ID != hostSteps[0].ID || p.Steps[n-1].ID != hostSteps[1].ID || r["pr"] == nil {
		t.Errorf("steps %+v, result %v", p.Steps, r)
	}
}

func TestHostErrorKeepsItsCode(t *testing.T) {
	f := newFake(render(t, templates.Peer, imageRef))
	stub(t, f)
	useHostPart(t, &fakeHostPart{planErr: op.Errorf(op.CodeRunnerRepoConflict, "two repositories")})
	if _, _, _, err := run(t); op.Code(err) != op.CodeRunnerRepoConflict {
		t.Errorf("plan err %v", err)
	}
	useHostPart(t, &fakeHostPart{steps: hostSteps, readErr: op.Errorf(op.CodeNotApplied, "claude-1")})
	_, _, r, err := run(t)
	if op.Code(err) != op.CodeNotApplied || r == nil {
		t.Fatalf("read back err %v, %v", err, r)
	}
	for _, c := range checks(t, r) {
		if c.ID == hostSteps[0].ID && c.Confirmed {
			t.Errorf("confirmed %+v", c)
		}
	}
}

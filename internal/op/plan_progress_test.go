package op

import (
	"context"
	"reflect"
	"testing"
)

// planRecorder collects the planning events of one Plan.
type planRecorder struct {
	checks  []string
	checked []Step
}

func (r *planRecorder) ctx() context.Context {
	return WithPlanProgress(context.Background(), func(e PlanEvent) {
		switch e.Kind {
		case PlanChecking:
			r.checks = append(r.checks, e.Check)
		case PlanChecked:
			if len(e.Steps) == 0 {
				panic("checked event without steps")
			}
			r.checked = append(r.checked, e.Steps...)
		}
	})
}

// reportsPlan checks the contract: every item reported as checked is in the
// final plan with the same values, once and in plan order; here every item
// of the plan is reported.
func (r *planRecorder) reportsPlan(t *testing.T, final []Step) {
	t.Helper()
	if !reflect.DeepEqual(r.checked, final) {
		t.Errorf("checked items differ from the plan:\nchecked %+v\nplan    %+v", r.checked, final)
	}
	if len(r.checks) == 0 {
		t.Error("no checking events")
	}
}

func TestPlanProgressWithoutSink(t *testing.T) {
	ctx := context.Background()
	Checking(ctx, "x")
	Checked(ctx, Step{ID: "a"})
	if withoutChecked(ctx) != ctx {
		t.Error("withoutChecked changed a ctx without a sink")
	}
}

func TestPlanProgressWithoutChecked(t *testing.T) {
	var got []PlanEvent
	ctx := WithPlanProgress(context.Background(), func(e PlanEvent) { got = append(got, e) })
	inner := withoutChecked(ctx)
	Checking(inner, "inner")
	Checked(inner, Step{ID: "dropped"})
	Checked(ctx)
	Checked(ctx, Step{ID: "kept"})
	want := []PlanEvent{{Kind: PlanChecking, Check: "inner"}, {Kind: PlanChecked, Steps: []Step{{ID: "kept"}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events %+v", got)
	}
}

func TestPlanProgressRunnerAdd(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, loginWorks: true}
	useRunnerHost(t, f)
	var r planRecorder
	p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}).Plan(r.ctx())
	if err != nil {
		t.Fatal(err)
	}
	r.reportsPlan(t, p.Steps)
}

// The nested login plan's machine start is dropped by runner add, so it is
// never reported.
func TestPlanProgressRunnerAddStopped(t *testing.T) {
	f := &fakeHost{stopped: true, vm: true, slots: map[string]*fakeSlot{}, loginWorks: true,
		volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useRunnerHost(t, f)
	var r planRecorder
	p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}).Plan(r.ctx())
	if err != nil {
		t.Fatal(err)
	}
	r.reportsPlan(t, p.Steps)
}

func TestPlanProgressLogin(t *testing.T) {
	f := &fakeHost{volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useHost(t, f)
	var r planRecorder
	p, err := Login(LoginParams{}).Plan(r.ctx())
	if err != nil {
		t.Fatal(err)
	}
	r.reportsPlan(t, p.Steps)
}

func TestPlanProgressRunnerRemove(t *testing.T) {
	removeHost(t)
	var r planRecorder
	p, err := RunnerRemove(RunnerRemoveParams{Provider: "claude"}).Plan(r.ctx())
	if err != nil {
		t.Fatal(err)
	}
	r.reportsPlan(t, p.Steps)
}

func TestPlanProgressHostUpgrade(t *testing.T) {
	upgradeHost(t)
	var r planRecorder
	steps, _, err := RunnersUpgrade().Plan(r.ctx())
	if err != nil {
		t.Fatal(err)
	}
	r.reportsPlan(t, steps)
}

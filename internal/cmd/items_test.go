package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
)

func TestWritePlan(t *testing.T) {
	p := op.Plan{Operation: "init", Steps: []op.Step{
		{ID: "label:shaped", Action: "create label", Target: "shaped", Detail: "already present", Current: "present", Next: "present", NoChange: true},
		{ID: "secret:S", Action: "set secret", Target: "S", Current: "absent", Next: "set"},
		{ID: "variable:V", Action: "set variable", Target: "V", AfterStart: true},
	}, Notes: []string{"set in the web UI https://x: Issues"}}
	var b bytes.Buffer
	writePlan(&b, p)
	want := "init will:\n" +
		"  [no change] create label shaped (already present): present\n" +
		"  set secret S: absent → set\n" +
		"  [after start] set variable V\n" +
		"notes:\n  - set in the web UI https://x: Issues\n"
	if b.String() != want {
		t.Errorf("plan =\n%s\nwant\n%s", b.String(), want)
	}
}

// itemOp applies one item and reads back a partial result with an error.
type itemOp struct{}

var itemStep = op.Step{ID: "issue:u", Action: "add label", Target: "u"}

func (itemOp) Plan(context.Context) (op.Plan, error) {
	return op.Plan{Operation: "pause", Steps: []op.Step{itemStep}}, nil
}
func (itemOp) Apply(ctx context.Context, _ op.Plan) error {
	return op.Track(ctx, itemStep, func() error { return nil })
}
func (itemOp) ReadBack(context.Context, op.Plan) (op.Result, error) {
	r := op.Result{
		op.ResultItems: []op.Check{{ID: "issue:u", Action: "add label", Target: "u", State: "no pause", Reason: "has no pause"}},
		op.ResultNotes: []string{},
	}
	return r, op.Errorf(op.CodeNotApplied, "pause not applied")
}

func TestChangingCommandPrintsItemsAndProgress(t *testing.T) {
	fakeRepo(t)
	old := ops
	t.Cleanup(func() { ops = old })
	ops.Pause = func(op.IssuesParams) op.Operation { return itemOp{} }
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"pause", "1"}, failReader{t}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	// The items read back print also when the read-back fails.
	if want := "items:\n  [not confirmed] add label u: no pause (has no pause)\n"; stdout.String() != want {
		t.Errorf("stdout = %q", stdout.String())
	}
	if want := "started: add label u\ndone: add label u\nnot_applied: pause not applied\n"; stderr.String() != want {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestProgressAndCheckLines(t *testing.T) {
	if got := progressLine(op.Event{Action: "open PR", Target: "b", Phase: op.PhaseFailed, Code: "repo_write_failed", Message: "boom"}); got != "failed: open PR b (repo_write_failed: boom)" {
		t.Errorf("progress = %q", got)
	}
	if got := progressLine(op.Event{Action: "remove runner", Target: "claude-1", Phase: op.PhaseStarted}); got != "started: remove runner claude-1" {
		t.Errorf("started = %q", got)
	}
	waiting := op.Event{Action: "remove runner", Target: "claude-1", Phase: op.PhaseStarted, Message: "waiting for its Job to end: h-claude-1 in github.com/o/r"}
	if got := progressLine(waiting); got != "started: remove runner claude-1 (waiting for its Job to end: h-claude-1 in github.com/o/r)" {
		t.Errorf("waiting = %q", got)
	}
	if got := checkLine(op.Check{Action: "set secret", Target: "S", Confirmed: true, State: "set"}); got != "[confirmed] set secret S: set" {
		t.Errorf("check = %q", got)
	}
	var b bytes.Buffer
	writeResult(&b, op.Result{"repo": "o/r", op.ResultItems: []op.Check{}, op.ResultNotes: []string{"run gh nibfin upgrade"}, "advice": []string{"x"}}, false)
	if want := "repo: o/r\nnotes:\n  - run gh nibfin upgrade\n"; b.String() != want || strings.Contains(b.String(), "advice") {
		t.Errorf("result = %q", b.String())
	}
}

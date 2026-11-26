package op

import "context"

// Kinds of a PlanEvent.
const (
	PlanChecking = "checking"
	PlanChecked  = "checked"
)

// PlanEvent is the progress of a Plan while it reads: the check it starts
// ("checking", with Check naming what is read) or the items whose values are
// final ("checked", with Steps).
//
// Every Step a Plan reports as checked appears in the Plan it returns with
// the same ID and values; the returned Plan stays the authority.
type PlanEvent struct {
	Kind  string `json:"kind"`
	Check string `json:"check,omitempty"`
	Steps []Step `json:"steps,omitempty"`
}

// planSink receives the events of a Plan. checked is false under a nested
// plan whose items the outer plan reports itself.
type planSink struct {
	fn      func(PlanEvent)
	checked bool
}

type planProgressKey struct{}

// WithPlanProgress returns ctx carrying fn, which receives the planning
// events of every Plan under ctx. Without it nothing is emitted, so the CLI
// and --json output do not change.
func WithPlanProgress(ctx context.Context, fn func(PlanEvent)) context.Context {
	return context.WithValue(ctx, planProgressKey{}, planSink{fn: fn, checked: true})
}

// withoutChecked returns ctx whose planning sink only receives checking
// events: a nested plan under it reports its checks, while the outer plan
// reports the items it keeps.
func withoutChecked(ctx context.Context) context.Context {
	s, ok := ctx.Value(planProgressKey{}).(planSink)
	if !ok || s.fn == nil {
		return ctx
	}
	s.checked = false
	return context.WithValue(ctx, planProgressKey{}, s)
}

// Checking reports that the plan starts reading what label names, such as
// "default branch files".
func Checking(ctx context.Context, label string) {
	if s, ok := ctx.Value(planProgressKey{}).(planSink); ok && s.fn != nil {
		s.fn(PlanEvent{Kind: PlanChecking, Check: label})
	}
}

// Checked reports items whose values are final, once each and in plan
// order.
func Checked(ctx context.Context, steps ...Step) {
	if len(steps) == 0 {
		return
	}
	if s, ok := ctx.Value(planProgressKey{}).(planSink); ok && s.fn != nil && s.checked {
		s.fn(PlanEvent{Kind: PlanChecked, Steps: append([]Step(nil), steps...)})
	}
}

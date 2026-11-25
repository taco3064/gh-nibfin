// Package op is the shared operation layer of gh nibfin. The CLI and the
// Dashboard API call the same entry points here; this package imports no CLI
// or terminal code.
package op

import "context"

// Step is one item of a plan: a change, or an item already in place
// (NoChange). Its ID names the item in the plan, its progress events and its
// read-back check.
type Step struct {
	// ID is unique within the plan and stable across plan, progress and
	// read-back, such as runner:claude-2 or label:shaped.
	ID     string `json:"id"`
	Action string `json:"action"`
	Target string `json:"target"`
	Detail string `json:"detail,omitempty"`
	// Current is the state read at plan time, "" when unknown; Next is the
	// resulting state.
	Current string `json:"current,omitempty"`
	Next    string `json:"next,omitempty"`
	// NoChange is true when the item is already in place: nothing applies
	// it, and read-back confirms it is still in place.
	NoChange bool `json:"noChange"`
	// AfterStart is true when the target or its state is only known after
	// the machine starts.
	AfterStart bool `json:"afterStart"`
}

// Plan is the structured preview of an operation.
type Plan struct {
	Operation string `json:"operation"`
	Repo      string `json:"repo,omitempty"`
	Steps     []Step `json:"steps"`
	// Notes are advice and steps only the person can take. They are not
	// items: nothing applies them and nothing reads them back.
	Notes []string `json:"notes,omitempty"`
	// Confirm is true when the plan must be confirmed before it is applied.
	Confirm bool `json:"confirm"`
	// ReadOnly is true when the operation only reads back.
	ReadOnly bool `json:"readOnly"`
}

// Changes returns the steps that change something: those without NoChange.
func (p Plan) Changes() []Step { return Changes(p.Steps) }

// Changes returns the steps without NoChange.
func Changes(steps []Step) []Step {
	out := []Step{}
	for _, s := range steps {
		if !s.NoChange {
			out = append(out, s)
		}
	}
	return out
}

// Result is the JSON-serialisable read-back summary of an operation.
type Result map[string]any

// Operation is one command's logic, split so a caller can return the plan
// and apply it on a later request.
type Operation interface {
	Plan(ctx context.Context) (Plan, error)
	Apply(ctx context.Context, p Plan) error
	ReadBack(ctx context.Context, p Plan) (Result, error)
}

// Confirmer decides whether a plan may be applied.
type Confirmer interface {
	Confirm(p Plan) (bool, error)
}

// Run plans, confirms when the plan asks for it, applies and reads back.
// A read-only plan goes straight to read-back. The read-back result is what
// gets reported.
func Run(ctx context.Context, o Operation, c Confirmer) (Plan, Result, error) {
	p, err := o.Plan(ctx)
	if err != nil {
		return p, nil, err
	}
	if p.Confirm && !p.ReadOnly {
		ok, err := c.Confirm(p)
		if err != nil {
			return p, nil, err
		}
		if !ok {
			return p, nil, Errorf(CodeCancelled, "%s cancelled", p.Operation)
		}
	}
	r, err := Finish(ctx, o, p, true)
	return p, r, err
}

// Finish runs what follows a plan: a read-only plan is read back; a plan
// that needs confirmation and is not confirmed fails with
// confirmation_required and nothing is applied; any other plan is applied and
// read back. The CLI (through Run) and the Dashboard API share it.
func Finish(ctx context.Context, o Operation, p Plan, confirmed bool) (Result, error) {
	if p.ReadOnly {
		return o.ReadBack(ctx, p)
	}
	if p.Confirm && !confirmed {
		return nil, Errorf(CodeConfirmationRequired, "%s needs confirmation", p.Operation)
	}
	if err := o.Apply(ctx, p); err != nil {
		return nil, err
	}
	return o.ReadBack(ctx, p)
}

// DefaultRunnerAddCount is the number of Runners runner add adds when no
// count is given, on the CLI and in the Dashboard API alike.
const DefaultRunnerAddCount = 2

// ReadBack plans and reads back without confirming or applying. It is the
// entry for read-only operations.
func ReadBack(ctx context.Context, o Operation) (Plan, Result, error) {
	p, err := o.Plan(ctx)
	if err != nil {
		return p, nil, err
	}
	r, err := o.ReadBack(ctx, p)
	return p, r, err
}

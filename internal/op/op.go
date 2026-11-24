// Package op is the shared operation layer of gh nibfin. The CLI and the
// Dashboard API call the same entry points here; this package imports no CLI
// or terminal code.
package op

import "context"

// Step is one planned change.
type Step struct {
	Action string `json:"action"`
	Target string `json:"target"`
	Detail string `json:"detail,omitempty"`
}

// Plan is the structured preview of an operation.
type Plan struct {
	Operation string `json:"operation"`
	Repo      string `json:"repo,omitempty"`
	Steps     []Step `json:"steps"`
	// Confirm is true when the plan must be confirmed before it is applied.
	Confirm bool `json:"confirm"`
	// ReadOnly is true when the operation only reads back.
	ReadOnly bool `json:"readOnly"`
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
	if p.ReadOnly {
		r, err := o.ReadBack(ctx, p)
		return p, r, err
	}
	if p.Confirm {
		ok, err := c.Confirm(p)
		if err != nil {
			return p, nil, err
		}
		if !ok {
			return p, nil, Errorf(CodeCancelled, "%s cancelled", p.Operation)
		}
	}
	if err := o.Apply(ctx, p); err != nil {
		return p, nil, err
	}
	r, err := o.ReadBack(ctx, p)
	return p, r, err
}

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

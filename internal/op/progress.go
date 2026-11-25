package op

import (
	"context"
	"fmt"
)

// Progress phases of an Event.
const (
	PhaseStarted = "started"
	PhaseDone    = "done"
	PhaseFailed  = "failed"
)

// Event is the progress of one plan item during Apply. Code and Message are
// set only when the item failed.
type Event struct {
	ID      string `json:"id"`
	Action  string `json:"action"`
	Target  string `json:"target"`
	Phase   string `json:"phase"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type progressKey struct{}

// WithProgress returns ctx carrying fn, which receives every progress event
// an Apply under ctx emits. Without it nothing is emitted.
func WithProgress(ctx context.Context, fn func(Event)) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

func emit(ctx context.Context, s Step, phase string, err error) {
	fn, _ := ctx.Value(progressKey{}).(func(Event))
	if fn == nil {
		return
	}
	e := Event{ID: s.ID, Action: s.Action, Target: s.Target, Phase: phase}
	if phase == PhaseFailed {
		ae := AsError(err)
		if ae == nil {
			ae = AsError(Errorf(CodeNotApplied, "%s %s failed", s.Action, s.Target))
		}
		e.Code, e.Message = ae.Code, ae.Error()
	}
	fn(e)
}

// Started, Done and Failed emit one item's progress.
func Started(ctx context.Context, s Step)           { emit(ctx, s, PhaseStarted, nil) }
func Done(ctx context.Context, s Step)              { emit(ctx, s, PhaseDone, nil) }
func Failed(ctx context.Context, s Step, err error) { emit(ctx, s, PhaseFailed, err) }

// Track runs one item's write: it emits started, then done or failed, and
// returns fn's error unchanged.
func Track(ctx context.Context, s Step, fn func() error) error {
	Started(ctx, s)
	err := fn()
	if err != nil {
		Failed(ctx, s, err)
	} else {
		Done(ctx, s)
	}
	return err
}

// TrackAll runs one write that covers several items: started for each,
// then done for each, or failed for each with fn's error.
func TrackAll(ctx context.Context, steps []Step, fn func() error) error {
	b := StartAll(ctx, steps)
	err := fn()
	b.Close(err)
	return err
}

// Batch is a set of started items that finish one by one. Close finishes
// the ones still open.
type Batch struct {
	ctx  context.Context
	open []Step
}

// StartAll emits started for each step and returns them as open.
func StartAll(ctx context.Context, steps []Step) *Batch {
	b := &Batch{ctx: ctx}
	for _, s := range steps {
		b.Start(s)
	}
	return b
}

// Start emits started for s and keeps it open.
func (b *Batch) Start(s Step) {
	Started(b.ctx, s)
	b.open = append(b.open, s)
}

func (b *Batch) take(id string) (Step, bool) {
	for i, s := range b.open {
		if s.ID == id {
			b.open = append(b.open[:i:i], b.open[i+1:]...)
			return s, true
		}
	}
	return Step{}, false
}

// Done finishes the open item id as done.
func (b *Batch) Done(id string) {
	if s, ok := b.take(id); ok {
		Done(b.ctx, s)
	}
}

// Fail finishes the open item id as failed with err.
func (b *Batch) Fail(id string, err error) {
	if s, ok := b.take(id); ok {
		Failed(b.ctx, s, err)
	}
}

// Close finishes every open item: done when err is nil, otherwise failed
// with err.
func (b *Batch) Close(err error) {
	open := b.open
	b.open = nil
	for _, s := range open {
		if err == nil {
			Done(b.ctx, s)
		} else {
			Failed(b.ctx, s, err)
		}
	}
}

// Abort finishes every open item as failed because err stopped the apply
// before the item finished; the failure keeps err's code.
func (b *Batch) Abort(err error) {
	if len(b.open) > 0 {
		b.Close(fmt.Errorf("stopped before this item finished: %w", err))
	}
}

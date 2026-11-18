package dashboard

import (
	"context"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type OverviewCollector interface {
	CollectOverview() snapshot.Overview
}

const overviewHistory = 8
const overviewInterval = 10 * time.Second
const overviewMinimum = 5 * time.Second

// OverviewUpdate is an additive, session-local reconciliation contract for #219.
// Each reset replaces keyed collections. It is not durable execution history.
type OverviewUpdate struct {
	ID       uint64            `json:"id"`
	Kind     string            `json:"kind"`
	Overview snapshot.Overview `json:"overview"`
}

type overviewEvent struct {
	id   uint64
	data []byte
}

type overviewState struct {
	collector OverviewCollector
	current   snapshot.Overview
	history   []overviewEvent
	next      uint64
	changed   chan struct{}
	refresh   chan struct{}
	done      chan struct{}
	streams   int
}

func (s *Service) startOverview(ctx context.Context, collector OverviewCollector) {
	if collector == nil {
		return
	}
	s.overview = &overviewState{collector: collector, changed: make(chan struct{}),
		refresh: make(chan struct{}, 1), done: make(chan struct{})}
	go s.collectOverview(ctx)
}

func (s *Service) collectOverview(ctx context.Context) {
	o := s.overview
	defer close(o.done)
	for {
		s.mu.Lock()
		valid := !s.closed && s.now().Before(s.expires)
		s.mu.Unlock()
		if !valid || ctx.Err() != nil {
			return
		}
		// Exactly one tracked collector; it cannot outlive bounded shutdown.
		value := o.collector.CollectOverview()
		s.mu.Lock()
		if !s.closed && s.now().Before(s.expires) && ctx.Err() == nil {
			o.next++
			value, data := encodeOverview(o.current, value, o.next)
			if data != nil {
				o.current = value
				o.history = append(o.history, overviewEvent{o.next, data})
				if len(o.history) > overviewHistory {
					o.history = o.history[len(o.history)-overviewHistory:]
				}
				close(o.changed)
				o.changed = make(chan struct{})
			}
		}
		s.mu.Unlock()
		// Refresh requests coalesce behind a minimum delay; background reads
		// continue at a bounded interval even without a subscriber.
		timer := time.NewTimer(overviewMinimum)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		timer = time.NewTimer(overviewInterval - overviewMinimum)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-o.refresh:
			timer.Stop()
		case <-timer.C:
		}
		select {
		case <-o.refresh:
		default:
		}
	}
}

// Partial absence is not removal. Retained rows keep their original timestamps.
func reconcileOverview(old, next snapshot.Overview) snapshot.Overview {
	if !next.Issues.Status.Complete {
		next.Issues.Items = retainRows(old.Issues.Items, next.Issues.Items,
			func(row snapshot.OverviewIssue) string { return row.Key }, 1000,
			func(row snapshot.OverviewIssue) snapshot.OverviewIssue {
				row.Retained = true
				return row
			})
	}
	if !next.Runners.Status.Complete {
		next.Runners.Items = retainRows(old.Runners.Items, next.Runners.Items,
			func(row snapshot.LocalRunner) string { return row.Key }, 128,
			func(row snapshot.LocalRunner) snapshot.LocalRunner {
				row.Retained = true
				return row
			})
	}
	if !next.Repositories.Status.Complete {
		next.Repositories.Items = retainRows(old.Repositories.Items,
			next.Repositories.Items, func(row snapshot.Repository) string {
				return row.Name
			}, 32, nil)
	}
	return next
}

func retainRows[T any](old, next []T, key func(T) string, limit int,
	markRetained func(T) T) []T {
	seen := map[string]bool{}
	out := make([]T, 0, len(next))
	for section, rows := range [][]T{next, old} {
		for _, row := range rows {
			if !seen[key(row)] && len(out) < limit {
				seen[key(row)] = true
				if section == 1 && markRetained != nil {
					row = markRetained(row)
				}
				out = append(out, row)
			}
		}
	}
	return out
}

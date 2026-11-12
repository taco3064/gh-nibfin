package main

import (
	"context"
	"errors"
	"time"

	"github.com/taco3064/gh-nibfin/internal/pause"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

// Synthetic provider exercises the actual handler; it never invokes gh or GitHub.
type labels struct {
	value  snapshot.Snapshot
	reads  map[int]int
	paused map[int]bool
}

func labelFixture(value snapshot.Snapshot) *labels {
	p := &labels{value: value, reads: map[int]int{}, paused: map[int]bool{}}
	for _, issue := range value.Issues.Items {
		p.paused[issue.Identity.Number] = issue.PauseLabel != nil && issue.PauseLabel.Present
	}
	return p
}

func (p *labels) Collect(string) snapshot.Snapshot {
	value := p.value
	value.Issues.Items = append([]snapshot.Issue(nil), value.Issues.Items...)
	for i := range value.Issues.Items {
		issue := &value.Issues.Items[i]
		issue.PauseLabel = &snapshot.PauseLabel{
			Present: p.paused[issue.Identity.Number], Actor: "Unknown"}
	}
	return value
}

func (p *labels) Inspect(ctx context.Context, _ string,
	target pause.Target) (pause.Facts, error) {
	if ctx.Err() != nil {
		return pause.Facts{}, ctx.Err()
	}
	p.reads[target.Number]++
	for _, issue := range p.value.Issues.Items {
		id := issue.Identity
		if target != (pause.Target{RepositoryID: id.Repository.ID,
			IssueID: id.ID, Number: id.Number}) {
			continue
		}
		// Item 3 loses read-back; item 5 closes after review.
		if target.Number == 3 && p.reads[3] >= 3 {
			return pause.Facts{}, errors.New("fixture unavailable")
		}
		scoped := false
		for _, label := range issue.Progress {
			scoped = scoped || label == "scope"
		}
		return pause.Facts{User: pause.User{ID: 7, Login: "owner"}, Target: target,
			Title: issue.Title, Open: issue.State == "open" &&
				(target.Number != 5 || p.reads[5] == 1), Scoped: scoped,
			Paused: p.paused[target.Number], Owner: true}, nil
	}
	return pause.Facts{}, errors.New("unknown target")
}

func (p *labels) Edit(ctx context.Context, _ string,
	target pause.Target, action pause.Action) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(150 * time.Millisecond):
	}
	if target.Number == 2 {
		return pause.Rejected
	}
	p.paused[target.Number] = action == pause.Pause
	if target.Number == 4 {
		return errors.New("ambiguous")
	}
	return nil
}

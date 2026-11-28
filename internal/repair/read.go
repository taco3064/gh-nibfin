package repair

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Getter reads one REST path into v; status's reader and the resume
// operation's client both provide it.
type Getter func(ctx context.Context, path string, v any) error

// Comment is one Issue comment as read.
type Comment struct {
	ID        int64
	URL       string
	Body      string
	Author    string
	AuthorID  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// newerThan orders comments by creation time, then id.
func (c Comment) newerThan(o Comment) bool {
	if !c.CreatedAt.Equal(o.CreatedAt) {
		return c.CreatedAt.After(o.CreatedAt)
	}
	return c.ID > o.ID
}

// Snapshot is what one read of an Issue's events and comments gives.
type Snapshot struct {
	// Actor is the id of the account that added pending last, Core's
	// account; 0 when no labeled event for pending was read.
	Actor int64
	// Pending and Record are that account's oldest comments starting with
	// the pending and repair markers; nil when there is none.
	Pending *Comment
	Record  *Comment
	// Choice is that account's oldest comment starting with the choice
	// marker, the one Core keeps updating; nil when there is none.
	Choice *Comment
	// Unlabeled is the time of the latest unlabeled event for pending,
	// zero when there is none.
	Unlabeled time.Time
	// After are the comments created after Record, by anyone, oldest
	// first; empty without a record.
	After []Comment
}

// ReadError is a failed read: Part is "events" or "comments".
type ReadError struct {
	Part string
	Err  error
}

func (e *ReadError) Error() string { return "read the " + e.Part + ": " + e.Err.Error() }
func (e *ReadError) Unwrap() error { return e.Err }

type eventJSON struct {
	Event     string `json:"event"`
	CreatedAt string `json:"created_at"`
	Actor     *struct {
		ID int64 `json:"id"`
	} `json:"actor"`
	Label *struct {
		Name string `json:"name"`
	} `json:"label"`
}

type commentJSON struct {
	ID   int64 `json:"id"`
	User *struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"user"`
	Body      string `json:"body"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

const perPage = 100

// Read pages through the events and comments of the Issue at issuePath
// (repos/OWNER/REPO/issues/N). Core's account is the actor of the latest
// labeled event for pending; only its comments count as the pending
// comment and the record. A comment or event without a parseable time is
// skipped. Without a labeled event for pending no comment is read.
func Read(ctx context.Context, get Getter, issuePath string) (Snapshot, error) {
	var s Snapshot
	var at time.Time
	for page := 1; ; page++ {
		var events []eventJSON
		if err := get(ctx, fmt.Sprintf("%s/events?per_page=%d&page=%d", issuePath, perPage, page), &events); err != nil {
			return Snapshot{}, &ReadError{Part: "events", Err: err}
		}
		for _, e := range events {
			if e.Label == nil || !strings.EqualFold(e.Label.Name, "pending") {
				continue
			}
			t, err := time.Parse(time.RFC3339, e.CreatedAt)
			if err != nil {
				continue
			}
			switch {
			case e.Event == "labeled" && e.Actor != nil:
				if s.Actor == 0 || !t.Before(at) {
					s.Actor, at = e.Actor.ID, t
				}
			case e.Event == "unlabeled":
				if t.After(s.Unlabeled) {
					s.Unlabeled = t
				}
			}
		}
		if len(events) < perPage {
			break
		}
	}

	s.After = []Comment{}
	if s.Actor == 0 {
		return s, nil
	}
	var all []Comment
	for page := 1; ; page++ {
		var comments []commentJSON
		if err := get(ctx, fmt.Sprintf("%s/comments?per_page=%d&page=%d", issuePath, perPage, page), &comments); err != nil {
			return Snapshot{}, &ReadError{Part: "comments", Err: err}
		}
		for _, j := range comments {
			created, err := time.Parse(time.RFC3339, j.CreatedAt)
			if err != nil {
				continue
			}
			updated, err := time.Parse(time.RFC3339, j.UpdatedAt)
			if err != nil {
				updated = created
			}
			c := Comment{ID: j.ID, URL: j.HTMLURL, Body: j.Body, CreatedAt: created, UpdatedAt: updated}
			if j.User != nil {
				c.Author, c.AuthorID = j.User.Login, j.User.ID
			}
			all = append(all, c)
		}
		if len(comments) < perPage {
			break
		}
	}
	s.Pending = oldest(all, s.Actor, PendingMarker)
	s.Record = oldest(all, s.Actor, RecordMarker)
	s.Choice = oldest(all, s.Actor, ChoiceMarker)
	if s.Record != nil {
		for _, c := range all {
			if c.newerThan(*s.Record) {
				s.After = append(s.After, c)
			}
		}
	}
	return s, nil
}

// oldest is actor's oldest comment (by creation time, then id) that starts
// with marker.
func oldest(all []Comment, actor int64, marker string) *Comment {
	var best *Comment
	for i := range all {
		c := all[i]
		if c.AuthorID != actor || !strings.HasPrefix(c.Body, marker) {
			continue
		}
		if best == nil || best.newerThan(c) {
			cc := c
			best = &cc
		}
	}
	return best
}

// Report is the repair state of a pending Issue.
type Report struct {
	// State is StateRepairing, StateChoose or StateAct.
	State string
	// Reason is one line: the record's, or NoRecordReason.
	Reason string
	// Text is the record's visible part; zero without an applicable
	// record. Options are kept only for StateChoose.
	Text Text
	// Record is the applicable record comment, nil when none applies;
	// RecordState is its state line.
	Record      *Comment
	RecordState State
}

// Evaluate applies the reader rule to s. ok is false without the pending
// label. A waiting-for-choice record whose options are not 2–4 numbered
// options with exactly one recommended is act: the person reads the record
// itself.
func (s Snapshot) Evaluate(hasPending bool) (Report, bool) {
	in := Input{HasPending: hasPending, Pending: s.Pending, Unlabeled: s.Unlabeled}
	var st State
	if s.Record != nil {
		if p, ok := ParseState(s.Record.Body); ok {
			st = p
			in.Record = &st
		}
	}
	state := Status(in)
	if state == "" {
		return Report{}, false
	}
	if !in.Applies() {
		return Report{State: StateAct, Reason: NoRecordReason}, true
	}
	r := Report{State: state, Text: ParseText(s.Record.Body), Record: s.Record, RecordState: st}
	if r.State == StateChoose && !ValidOptions(r.Text.Options) {
		r.State = StateAct
	}
	if r.State != StateChoose {
		r.Text.Options = nil
	}
	r.Reason = r.Text.Reason
	if r.Reason == "" {
		r.Reason = "See the repair record"
	}
	return r, true
}

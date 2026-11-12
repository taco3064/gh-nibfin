// Package pause requests native label edits without asserting Core authorization.
package pause

import (
	"context"
	"errors"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

const MaxTargets = 50
const WorkBound = 2 * time.Minute

type Action string

const (
	Pause  Action = "pause"
	Resume Action = "resume"
)

func (a Action) Valid() bool { return a == Pause || a == Resume }

type Target struct {
	RepositoryID int64 `json:"repository_id"`
	IssueID      int64 `json:"issue_id"`
	Number       int   `json:"number"`
}

type Request struct {
	Batch      bool     `json:"batch"`
	Repository string   `json:"repository"`
	Action     Action   `json:"action"`
	Targets    []Target `json:"targets"`
}

func (r Request) Valid() bool {
	if !snapshot.ValidRepository(r.Repository) || !r.Action.Valid() ||
		len(r.Targets) == 0 || len(r.Targets) > MaxTargets ||
		!r.Batch && len(r.Targets) != 1 {
		return false
	}
	numbers, ids := map[int]bool{}, map[int64]bool{}
	for _, t := range r.Targets {
		if t.RepositoryID <= 0 || t.IssueID <= 0 || t.Number <= 0 ||
			t.Number > 2147483647 || numbers[t.Number] || ids[t.IssueID] {
			return false
		}
		numbers[t.Number], ids[t.IssueID] = true, true
	}
	return true
}

type User struct {
	ID    int64
	Login string
}

// Facts come only from native reads. Missing evidence is an error, not false.
type Facts struct {
	User            User
	Target          Target
	Title           string
	Open            bool
	Scoped          bool
	Paused          bool
	Owner           bool
	Admin           bool
	LabelPermission bool
	Assigned        bool
}

// Provider implementations must honor context and bound every transport call.
type Provider interface {
	Inspect(context.Context, string, Target) (Facts, error)
	Edit(context.Context, string, Target, Action) error
}

var Rejected = errors.New("definite_request_rejection")

type Item struct {
	Target   Target `json:"target"`
	Title    string `json:"title"`
	Label    string `json:"label"`
	Actor    string `json:"actor"`
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason"`
	User     User   `json:"-"`
}

type Selection struct {
	Request Request `json:"-"`
	Items   []Item  `json:"items"`
}

type Result struct {
	Target     Target `json:"target"`
	Submission string `json:"submission"`
	Label      string `json:"label"`
	Verdict    string `json:"verdict"`
	Reason     string `json:"reason"`
}

type Controller struct{ Provider Provider }

func eligibility(f Facts, action Action, batch bool) string {
	if batch && !f.Scoped {
		return "outside_scope"
	}
	if action == Pause && !f.Open {
		return "closed_issue"
	}
	if !f.Owner && !f.Admin && !(f.LabelPermission && f.Assigned) {
		return "ineligible"
	}
	return ""
}

func label(f Facts) string {
	if f.Paused {
		return "present"
	}
	return "absent"
}

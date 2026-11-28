// Package repair reads Core's repair contract on a pending Issue: the repair
// record Nibfin's repair stage writes (Proposal R4.49), the reader rule that
// says whether it covers the current stop (R9.52), and the decision comment
// a person writes to answer its options (R4.51), and the choice comment in
// which Core reports how applying that decision went. The formats are Core's
// (nibfin-core src/stages/repair-record.ts, src/stages/choice.ts and the
// "Repair stage" and "Chosen option" sections of src/stages/README.md);
// gh-nibfin only reads records and choices and never writes one.
//
// status and the resume operation both use it, so it imports neither.
package repair

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Markers of Nibfin's comments.
const (
	PendingMarker = "<!-- nibfin:pending -->"
	RecordMarker  = "<!-- nibfin:repair -->"
)

// States of a pending Issue, as status reports them.
const (
	// StateRepairing is a record that covers the stop and is still working
	// on it (retry, wait, agent, rerun or amend).
	StateRepairing = "repairing"
	// StateChoose is a record that covers the stop with numbered options a
	// person chooses from.
	StateChoose = "choose"
	// StateAct is everything else: a record with follow-up advice, or no
	// record that covers the stop.
	StateAct = "act"
)

// NoRecordReason is the reason of a pending Issue no record covers.
const NoRecordReason = "No repair record covers this stop yet; see the pending comment"

// Record statuses and actions, Core's lists.
var (
	statuses = []string{"repairing", "waiting-for-choice", "needs-person"}
	actions  = []string{"retry", "wait", "advice", "agent", "rerun", "amend", "options"}
)

// stateLine is Core's state line regex, unchanged.
var stateLine = regexp.MustCompile(`^<!-- nibfin:repair-state for=([0-9a-f]{16}) status=([a-z-]+) action=([a-z]+) ` +
	`handled=([A-Z0-9_,]*)(?: reset=([0-9T:.Z-]+))? -->$`)

// State is the state line of a repair record.
type State struct {
	// For is the Digest of the pending comment body the record covers.
	For     string
	Status  string
	Action  string
	Handled []string
	// Reset is the usage-limit reset of a wait, "" otherwise.
	Reset string
}

// Digest ties a record to one pending comment: the first 16 hex characters
// of the SHA-256 of its exact body, marker line included.
func Digest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])[:16]
}

// ParseState reads the state line of a record body: the line after the
// repair marker, or the first line when there is none. A missing or
// unparseable line, an unknown status or action, or a wait without a reset
// is no record.
func ParseState(body string) (State, bool) {
	lines := strings.Split(body, "\n")
	first := lines[0]
	if first == RecordMarker {
		first = ""
		if len(lines) > 1 {
			first = lines[1]
		}
	}
	m := stateLine.FindStringSubmatch(first)
	if m == nil || !slices.Contains(statuses, m[2]) || !slices.Contains(actions, m[3]) || (m[3] == "wait" && m[5] == "") {
		return State{}, false
	}
	s := State{For: m[1], Status: m[2], Action: m[3], Handled: []string{}, Reset: m[5]}
	for _, code := range strings.Split(m[4], ",") {
		if code != "" {
			s.Handled = append(s.Handled, code)
		}
	}
	return s, true
}

// Input is what the reader rule needs.
type Input struct {
	HasPending bool
	// Pending is Nibfin's marked pending comment, nil when there is none.
	Pending *Comment
	// Unlabeled is the time of the latest unlabeled event for pending, zero
	// when there is none.
	Unlabeled time.Time
	// Record is the state of Nibfin's marked repair comment, nil when there
	// is none or it does not parse.
	Record *State
}

// Current reports whether the pending comment belongs to the current
// pending label: it exists and changed after pending was last removed (or
// pending never was).
func (in Input) Current() bool {
	return in.Pending != nil && (in.Unlabeled.IsZero() || in.Pending.UpdatedAt.After(in.Unlabeled))
}

// Applies reports whether the record covers the current pending comment.
func (in Input) Applies() bool {
	return in.HasPending && in.Current() && in.Record != nil && in.Record.For == Digest(in.Pending.Body)
}

// Status is Core's reader rule in status's names: "" without the pending
// label; the record's status when it covers the current pending comment
// (repairing, or choose for waiting-for-choice); otherwise act.
func Status(in Input) string {
	if !in.HasPending {
		return ""
	}
	if !in.Applies() {
		return StateAct
	}
	switch in.Record.Status {
	case "repairing":
		return StateRepairing
	case "waiting-for-choice":
		return StateChoose
	}
	return StateAct
}

// FormatDecision is the marker line of a decision for option n (n ≥ 1).
func FormatDecision(n int) string {
	return "<!-- nibfin:decision option=" + strconv.Itoa(n) + " -->"
}

var (
	markedDecision = regexp.MustCompile(`^<!-- nibfin:decision option=([1-9][0-9]*) -->$`)
	bareDecision   = regexp.MustCompile(`^[1-9][0-9]*$`)
)

// maxSafe is JavaScript's largest safe integer, Core's bound on an option.
const maxSafe = 1<<53 - 1

// ParseDecision is the option a comment chooses: the decision marker on its
// first line, or a body that is only a number once trimmed.
func ParseDecision(body string) (int, bool) {
	first, _, _ := strings.Cut(body, "\n")
	v := ""
	if m := markedDecision.FindStringSubmatch(jsTrim(first)); m != nil {
		v = m[1]
	} else if t := jsTrim(body); bareDecision.MatchString(t) {
		v = t
	}
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n > maxSafe {
		return 0, false
	}
	return int(n), true
}

// jsTrim trims what JavaScript's String.prototype.trim does: white space,
// line terminators and the byte order mark.
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == 0xFEFF })
}

package repair

import (
	"regexp"
	"strings"
)

// ChoiceMarker is the marker of the choice comment, where Core stores the
// decision it took from a repair record's answers and how applying it went
// (nibfin-core src/stages/choice.ts, "Chosen option" in src/stages/README.md).
const ChoiceMarker = "<!-- nibfin:choice -->"

// nibfinMarker is a marker line with a bare key, Core's isNibfinComment.
var nibfinMarker = regexp.MustCompile(`^<!-- nibfin:([a-z0-9][a-z0-9-]*) -->$`)

// Normalize is a comment body as Core compares it: CRLF and CR line endings
// turned into LF, then trimmed as JavaScript trims.
func Normalize(body string) string {
	return jsTrim(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"))
}

// IsNibfinComment reports whether a comment is one of Nibfin's own: its
// first line, trimmed, is a marker with a bare key other than decision.
// Nibfin may write as a person's account, so only the marker tells its
// comments apart.
func IsNibfinComment(body string) bool {
	first, _, _ := strings.Cut(body, "\n")
	m := nibfinMarker.FindStringSubmatch(jsTrim(first))
	return m != nil && m[1] != "decision"
}

// Candidate is the newest comment (by creation time, then id) that Core
// can take as the decision on the record (R4.51): created after the
// record's last update, with an author and a body that is not empty once
// normalized, and not one of Nibfin's own marked comments. option is what
// ParseDecision reads from it, 0 for a decision in the person's own words.
//
// Core also requires the author to have write permission; gh-nibfin does
// not check that, so a newer comment by a reader still counts here. That
// errs towards reporting a decision as not the newest.
func (s Snapshot) Candidate() (c Comment, option int, ok bool) {
	if s.Record == nil {
		return Comment{}, 0, false
	}
	for _, a := range s.After {
		text := Normalize(a.Body)
		if !a.CreatedAt.After(s.Record.UpdatedAt) || a.Author == "" || text == "" || IsNibfinComment(text) {
			continue
		}
		if !ok || a.newerThan(c) {
			c, ok = a, true
		}
	}
	if !ok {
		return Comment{}, 0, false
	}
	option, _ = ParseDecision(Normalize(c.Body))
	return c, option, true
}

// choiceState is Core's state line of a choice comment, unchanged.
var choiceState = regexp.MustCompile(`^<!-- nibfin:choice-state for=([0-9a-f]{16})(?: option=([1-9][0-9]*))? ` +
	`comment=([1-9][0-9]*) status=(chosen|applying|applied|dropped) -->$`)

// Choice statuses, Core's.
const (
	ChoiceChosen   = "chosen"
	ChoiceApplying = "applying"
	ChoiceApplied  = "applied"
	ChoiceDropped  = "dropped"
)

// Choice is what a choice comment says about applying a decision.
type Choice struct {
	// Status is chosen, applying, applied or dropped.
	Status string
	// NotApplied are the parts Core could not apply and left for a person
	// (a label, CI, closing), one line each.
	NotApplied []string
}

// ParseChoice reads a choice comment body (the marker line optional): its
// state line, then the "- Not applied: " lines of the header. The header
// ends at the first "### " section, so the decision's own text, which Core
// quotes last, cannot add a line. A body without a valid state line is no
// choice.
func ParseChoice(body string) (Choice, bool) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if lines[0] == ChoiceMarker {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return Choice{}, false
	}
	m := choiceState.FindStringSubmatch(lines[0])
	if m == nil {
		return Choice{}, false
	}
	ch := Choice{Status: m[4], NotApplied: []string{}}
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "### ") {
			break
		}
		if v, ok := strings.CutPrefix(l, "- Not applied: "); ok {
			if v = strings.TrimSpace(v); v != "" {
				ch.NotApplied = append(ch.NotApplied, v)
			}
		}
	}
	return ch, true
}

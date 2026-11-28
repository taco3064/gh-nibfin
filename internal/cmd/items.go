package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// The CLI prints a changing command's plan, progress and read-back from the
// same items the Dashboard API returns.

// stepLine is one plan item: what it does, its state change and whether it
// needs no change or is only known after the machine starts.
func stepLine(s op.Step) string {
	line := strings.TrimSpace(s.Action + " " + s.Target)
	if s.Detail != "" {
		line += " (" + s.Detail + ")"
	}
	if s.Current != "" || s.Next != "" {
		cur := s.Current
		if cur == "" {
			cur = "unknown"
		}
		line += ": " + cur
		if s.Next != "" && s.Next != s.Current {
			line += " → " + s.Next
		}
	}
	switch {
	case s.NoChange:
		line = "[no change] " + line
	case s.AfterStart:
		line = "[after start] " + line
	}
	return line
}

// writePlan prints a plan's items and notes.
func writePlan(w io.Writer, p op.Plan) {
	fmt.Fprintf(w, "%s will:\n", p.Operation)
	for _, s := range p.Steps {
		fmt.Fprintln(w, "  "+stepLine(s))
	}
	writeNotes(w, p.Notes)
}

func writeNotes(w io.Writer, notes []string) {
	if len(notes) == 0 {
		return
	}
	fmt.Fprintln(w, "notes:")
	for _, n := range notes {
		fmt.Fprintln(w, "  - "+n)
	}
}

// checkLine is one read-back item.
func checkLine(c op.Check) string {
	line := strings.TrimSpace(c.Action + " " + c.Target)
	if c.State != "" {
		line += ": " + c.State
	}
	if c.Confirmed {
		return "[confirmed] " + line
	}
	if c.Reason != "" {
		line += " (" + c.Reason + ")"
	}
	return "[not confirmed] " + line
}

// writeChecks prints the read-back items of a result.
func writeChecks(w io.Writer, items []op.Check) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintln(w, "items:")
	for _, c := range items {
		fmt.Fprintln(w, "  "+checkLine(c))
	}
}

// progressLine is one progress event. A started event with a message is an
// item that waits, such as a Runner waiting for its Job to end.
func progressLine(e op.Event) string {
	line := e.Phase + ": " + strings.TrimSpace(e.Action+" "+e.Target)
	switch {
	case e.Phase == op.PhaseFailed:
		line += " (" + e.Code + ": " + e.Message + ")"
	case e.Phase == op.PhaseStarted && e.Message != "":
		line += " (" + e.Message + ")"
	}
	return line
}

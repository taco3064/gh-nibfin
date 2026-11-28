package status

import "github.com/taco3064/gh-nibfin/internal/repair"

// Repair is the repair state of a pending Issue, read from the repair
// record Core's repair stage writes (Proposal R4.49, R9.52).
type Repair struct {
	// State is repairing (the stage is retrying, waiting, running the
	// repair agent, rerunning CI or amending the Issue), choose (Options
	// wait for a person's choice) or act (a person follows the advice, or
	// no record covers the stop).
	State string `json:"state"`
	// Reason is one line: the record's, or why there is none.
	Reason   string `json:"reason"`
	Category string `json:"category,omitempty"`
	// Done is what the stage did.
	Done   string `json:"done,omitempty"`
	Advice string `json:"advice,omitempty"`
	// Options are set only for choose: 2–4 of them, one Recommended.
	// resume --option N answers them.
	Options []RepairOption `json:"options,omitempty"`
	// Steps and BugReport are the record's follow-up advice.
	Steps     []RepairStep     `json:"steps,omitempty"`
	BugReport *RepairBugReport `json:"bugReport,omitempty"`
	// Record links the repair record comment; empty when no record covers
	// the stop.
	Record string `json:"record,omitempty"`
}

// RepairOption is one numbered option of a choose record.
type RepairOption struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Consequence string `json:"consequence"`
	// IssueChange is how the Issue changes when this option is chosen.
	IssueChange string `json:"issueChange"`
	Recommended bool   `json:"recommended"`
}

// RepairStep is one follow-up step, with a link or a command to run.
type RepairStep struct {
	Text    string `json:"text"`
	Link    string `json:"link,omitempty"`
	Command string `json:"command,omitempty"`
}

// RepairBugReport is what to file at Nibfin for a Nibfin process error.
type RepairBugReport struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func toRepair(r repair.Report) *Repair {
	out := &Repair{State: r.State, Reason: r.Reason, Category: r.Text.Category, Done: r.Text.Done, Advice: r.Text.Advice}
	if r.Record != nil {
		out.Record = r.Record.URL
	}
	for _, o := range r.Text.Options {
		out.Options = append(out.Options, RepairOption{Number: o.Number, Title: o.Title, Consequence: o.Consequence,
			IssueChange: o.IssueChange, Recommended: o.Recommended})
	}
	for _, s := range r.Text.Steps {
		out.Steps = append(out.Steps, RepairStep(s))
	}
	if b := r.Text.BugReport; b != nil {
		out.BugReport = &RepairBugReport{URL: b.URL, Title: b.Title, Body: b.Body}
	}
	return out
}

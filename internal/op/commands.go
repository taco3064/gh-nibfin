package op

// RepoParams name the target repository as HOST/OWNER/REPO.
type RepoParams struct {
	Repo string `json:"repo"`
}

// IssuesParams name the issues an operation acts on. Each Issue is a number
// (N or #N) in Repo, given as HOST/OWNER/REPO, or an Issue URL; Repo is used
// only for numbers.
type IssuesParams struct {
	Repo   string   `json:"repo,omitempty"`
	Issues []string `json:"issues"`
}

// Pause is the entry point of pause: it adds the pause label to each Issue.
func Pause(p IssuesParams) Operation {
	return &labelOp{name: "pause", add: "pause", done: "paused", params: p}
}

// ResumeParams are resume's: the Issues, and the option or the reply in
// the person's own words that answers the repair record of one pending
// Issue.
type ResumeParams struct {
	IssuesParams
	// Option is the number of the repair record's option to choose; 0 is
	// none.
	Option int `json:"option,omitempty"`
	// Reply is a decision in the person's own words, posted as written; ""
	// is none.
	Reply string `json:"reply,omitempty"`
}

// Resume is the entry point of resume: it removes the pause and pending
// labels from each Issue. With an Option or a Reply it first writes the
// decision comment on its one Issue (decisionOp).
func Resume(p ResumeParams) Operation {
	l := &labelOp{name: "resume", remove: []string{"pause", "pending"}, done: "resumed", params: p.IssuesParams}
	if p.Option == 0 && p.Reply == "" {
		return l
	}
	return &decisionOp{label: l, option: p.Option, reply: p.Reply}
}

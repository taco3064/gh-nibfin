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

// Resume is the entry point of resume: it removes the pause and pending
// labels from each Issue.
func Resume(p IssuesParams) Operation {
	return &labelOp{name: "resume", remove: []string{"pause", "pending"}, done: "resumed", params: p}
}

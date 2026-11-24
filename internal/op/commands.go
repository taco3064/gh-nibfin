package op

import "context"

// RunnerRemoveParams are the inputs of runner remove.
type RunnerRemoveParams struct {
	Provider    string `json:"provider"`
	Count       int    `json:"count"`
	DeleteLogin bool   `json:"deleteLogin"`
}

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

// RunnerRemove is the entry point of runner remove.
func RunnerRemove(p RunnerRemoveParams) Operation { return notImplemented("runner remove") }

// Init is the entry point of init.
func Init(p RepoParams) Operation { return notImplemented("init") }

// Status is the entry point of status.
func Status(p RepoParams) Operation { return notImplemented("status") }

// Pause is the entry point of pause: it adds the pause label to each Issue.
func Pause(p IssuesParams) Operation {
	return &labelOp{name: "pause", add: "pause", done: "paused", params: p}
}

// Resume is the entry point of resume: it removes the pause and pending
// labels from each Issue.
func Resume(p IssuesParams) Operation {
	return &labelOp{name: "resume", remove: []string{"pause", "pending"}, done: "resumed", params: p}
}

// unimplemented is the baseline body of every entry point: its plan fails
// with not_implemented naming the command.
type unimplemented struct{ name string }

func notImplemented(name string) Operation { return unimplemented{name} }

func (u unimplemented) Plan(context.Context) (Plan, error) {
	return Plan{Operation: u.name}, NotImplemented(u.name)
}

func (u unimplemented) Apply(context.Context, Plan) error { return NotImplemented(u.name) }

func (u unimplemented) ReadBack(context.Context, Plan) (Result, error) {
	return nil, NotImplemented(u.name)
}

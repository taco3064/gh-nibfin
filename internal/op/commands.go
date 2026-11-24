package op

import "context"

// RunnerAddParams are the inputs of runner add.
type RunnerAddParams struct {
	Provider string `json:"provider"`
	Count    int    `json:"count"`
}

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

// IssuesParams name the issues an operation acts on.
type IssuesParams struct {
	Issues []string `json:"issues"`
}

// RunnerAdd is the entry point of runner add.
func RunnerAdd(p RunnerAddParams) Operation { return notImplemented("runner add") }

// RunnerRemove is the entry point of runner remove.
func RunnerRemove(p RunnerRemoveParams) Operation { return notImplemented("runner remove") }

// Init is the entry point of init.
func Init(p RepoParams) Operation { return notImplemented("init") }

// Upgrade is the entry point of upgrade.
func Upgrade(p RepoParams) Operation { return notImplemented("upgrade") }

// Status is the entry point of status.
func Status(p RepoParams) Operation { return notImplemented("status") }

// Pause is the entry point of pause.
func Pause(p IssuesParams) Operation { return notImplemented("pause") }

// Resume is the entry point of resume.
func Resume(p IssuesParams) Operation { return notImplemented("resume") }

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

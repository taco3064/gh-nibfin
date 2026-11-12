package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/pause"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type contextProcess interface {
	RunContext(context.Context, string, []string, []byte) ([]byte, error)
}

type pauseProcess struct {
	ctx     context.Context
	process Process
}

func (p pauseProcess) Run(name string, args []string, body []byte) ([]byte, error) {
	if p.ctx.Err() != nil {
		return nil, p.ctx.Err()
	}
	if process, ok := p.process.(contextProcess); ok {
		return process.RunContext(p.ctx, name, args, body)
	}
	return nil, errors.New("CONTEXT_PROCESS_REQUIRED")
}

// NativePause uses the current gh login; no browser authority enters this seam.
type NativePause struct {
	API     API
	Process Process
}

func (p NativePause) api(ctx context.Context) API {
	if g, ok := p.API.(GitHub); ok {
		return GitHub{Process: pauseProcess{ctx, g.Process}}
	}
	return p.API
}

func (p NativePause) Inspect(ctx context.Context, name string,
	target pause.Target) (pause.Facts, error) {
	var facts pause.Facts
	unavailable := errors.New("PAUSE_FACTS_UNAVAILABLE")
	if !snapshot.ValidRepository(name) || target.Number <= 0 || ctx.Err() != nil {
		return facts, unavailable
	}
	api := p.api(ctx)
	if api == nil {
		return facts, unavailable
	}
	var user User
	var repo Repository
	if api.Call("GET", "user", nil, &user) != nil || user.ID <= 0 ||
		user.Type != "User" || !dashboardLogin.MatchString(user.Login) {
		return facts, unavailable
	}
	if ctx.Err() != nil || api.Call("GET", "repos/"+name, nil, &repo) != nil ||
		repo.ID != target.RepositoryID || !strings.EqualFold(repo.FullName, name) ||
		repo.Owner.ID <= 0 || !dashboardLogin.MatchString(repo.Owner.Login) ||
		(repo.Owner.Type != "User" && repo.Owner.Type != "Organization") ||
		repo.Permissions == nil {
		return facts, unavailable
	}
	path := "repos/" + name + "/issues/" + strconv.Itoa(target.Number)
	var issue struct {
		ID            int64           `json:"id"`
		Number        int             `json:"number"`
		Title         string          `json:"title"`
		State         string          `json:"state"`
		RepositoryURL string          `json:"repository_url"`
		URL           string          `json:"url"`
		PullRequest   json.RawMessage `json:"pull_request"`
		Labels        *[]struct {
			Name string `json:"name"`
		} `json:"labels"`
		Assignees *[]User `json:"assignees"`
	}
	if ctx.Err() != nil || api.Call("GET", path, nil, &issue) != nil ||
		issue.ID != target.IssueID || issue.Number != target.Number ||
		!strings.EqualFold(issue.RepositoryURL, "https://api.github.com/repos/"+name) ||
		!strings.EqualFold(issue.URL, "https://api.github.com/"+path) ||
		len(issue.PullRequest) > 0 && string(issue.PullRequest) != "null" ||
		issue.Labels == nil || issue.Assignees == nil || len(issue.Title) > 1024 ||
		(issue.State != "open" && issue.State != "closed") {
		return facts, unavailable
	}
	_, pushKnown := repo.Permissions["push"]
	_, triageKnown := repo.Permissions["triage"]
	if !pushKnown && !triageKnown && !repo.Permissions["admin"] {
		return facts, unavailable
	}
	facts = pause.Facts{User: pause.User{ID: user.ID, Login: user.Login},
		Target: target, Title: issue.Title, Open: issue.State == "open",
		Owner: repo.Owner.Type == "User" && repo.Owner.ID == user.ID &&
			strings.EqualFold(repo.Owner.Login, user.Login),
		Admin: repo.Owner.Type == "Organization" && repo.Permissions["admin"],
		LabelPermission: repo.Permissions["push"] || repo.Permissions["triage"] ||
			repo.Permissions["maintain"] || repo.Permissions["admin"]}
	for _, label := range *issue.Labels {
		facts.Scoped = facts.Scoped || label.Name == "scope"
		facts.Paused = facts.Paused || label.Name == "pause"
	}
	for _, assignee := range *issue.Assignees {
		if assignee.ID <= 0 || !dashboardLogin.MatchString(assignee.Login) {
			return pause.Facts{}, unavailable
		}
		facts.Assigned = facts.Assigned || assignee.ID == user.ID &&
			strings.EqualFold(assignee.Login, user.Login)
	}
	return facts, nil
}

func (p NativePause) Edit(ctx context.Context, name string,
	target pause.Target, action pause.Action) error {
	request := pause.Request{Repository: name, Action: action,
		Targets: []pause.Target{target}}
	if !request.Valid() || p.Process == nil {
		return pause.Rejected
	}
	flag := "--add-label"
	if action == pause.Resume {
		flag = "--remove-label"
	}
	_, err := (pauseProcess{ctx, p.Process}).Run("gh", []string{
		"issue", "edit", strconv.Itoa(target.Number), "--repo", name, flag, "pause"}, nil)
	if err != nil && (err.Error() == "PERMISSION_DENIED" || err.Error() == "NOT_FOUND") {
		return pause.Rejected
	}
	return err
}

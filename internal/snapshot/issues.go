package snapshot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type nativeIssue struct {
	ID            int64           `json:"id"`
	Number        int             `json:"number"`
	Title         string          `json:"title"`
	State         string          `json:"state"`
	RepositoryURL string          `json:"repository_url"`
	URL           string          `json:"url"`
	HTMLURL       string          `json:"html_url"`
	PullRequest   json.RawMessage `json:"pull_request"`
	Labels        []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []Assignee `json:"assignees"`
}

func issuePath(repo string, number int) string {
	return fmt.Sprintf("repos/%s/issues/%d", repo, number)
}

func issueIdentity(raw nativeIssue, repo Repository) (Identity, error) {
	path := issuePath(repo.Name, raw.Number)
	if raw.ID <= 0 || raw.Number <= 0 ||
		!strings.EqualFold(raw.RepositoryURL, "https://api.github.com/repos/"+
			repo.Name) ||
		raw.URL != "" && !strings.EqualFold(raw.URL, "https://api.github.com/"+path) {
		return Identity{}, invalid
	}
	return Identity{repo, raw.ID, raw.Number,
		repo.URL + "/issues/" + strconv.Itoa(raw.Number)}, nil
}

func isPR(raw nativeIssue) bool {
	return len(raw.PullRequest) > 0 && string(raw.PullRequest) != "null"
}

func (c *collector) issues(repo Repository) Section[Issue] {
	out := Section[Issue]{}
	numbers := map[int]bool{}
	out.Status = c.list("repos/"+repo.Name+"/issues?state=all&sort=created&"+
		"direction=asc", "", c.limits.Issues, func(data json.RawMessage) (int64, error) {
		var raw nativeIssue
		if json.Unmarshal(data, &raw) != nil {
			return 0, invalid
		}
		id, err := issueIdentity(raw, repo)
		if err != nil || numbers[raw.Number] {
			return 0, invalid
		}
		numbers[raw.Number] = true
		if isPR(raw) {
			return raw.ID, nil
		}
		if (raw.State != "open" && raw.State != "closed") ||
			raw.Labels == nil || raw.Assignees == nil || raw.Title == "" {
			return 0, invalid
		}
		issue := Issue{Identity: id, Title: raw.Title, State: raw.State,
			Progress: []string{}, Assignees: raw.Assignees, Core: unknownCore()}
		for _, a := range raw.Assignees {
			if a.ID <= 0 || a.Login == "" {
				return 0, invalid
			}
		}
		labels := map[string]bool{}
		for _, label := range raw.Labels {
			labels[label.Name] = true
		}
		for _, label := range []string{"scope", "shaped", "delivered"} {
			if labels[label] {
				issue.Progress = append(issue.Progress, label)
			}
		}
		issue.PauseLabel = &PauseLabel{Present: labels["pause"], Actor: "Unknown"}
		issue.BlockedBy = c.dependencies(id)
		out.Items = append(out.Items, issue)
		return raw.ID, nil
	})
	if out.Items == nil && out.Status.Availability == Known {
		out.Items = []Issue{}
	}
	return out
}

func (c *collector) dependencies(from Identity) Section[Edge] {
	out := Section[Edge]{}
	path := issuePath(from.Repository.Name, from.Number) + "/dependencies/blocked_by"
	out.Status = c.list(path, "", 2000, func(data json.RawMessage) (int64, error) {
		var raw nativeIssue
		if json.Unmarshal(data, &raw) != nil || isPR(raw) {
			return 0, invalid
		}
		const prefix = "https://api.github.com/repos/"
		if !strings.HasPrefix(raw.RepositoryURL, prefix) {
			return 0, invalid
		}
		name := strings.TrimPrefix(raw.RepositoryURL, prefix)
		if !ValidRepository(name) {
			return 0, invalid
		}
		unresolved := Repository{0, name, "https://github.com/" + name}
		if _, err := issueIdentity(raw, unresolved); err != nil {
			return 0, err
		}
		// Preserve the native reference even when its repository is denied.
		r := c.repository(name)
		repo := r.repo
		if r.status.Availability != Known {
			repo = unresolved
		}
		id, err := issueIdentity(raw, repo)
		if err != nil {
			return 0, err
		}
		resolution := r.status
		if resolution.Availability == Known {
			resolution = c.status(issuePath(repo.Name, raw.Number))
			var actual nativeIssue
			reason := c.call(resolution.Source, &actual)
			if reason == None {
				checked, err := issueIdentity(actual, repo)
				if err != nil || checked != id || isPR(actual) {
					reason = Invalid
				}
			}
			if reason != None {
				resolution = failed(resolution, reason)
			}
		}
		out.Items = append(out.Items, Edge{from, Reference{id, resolution}})
		return raw.ID, nil
	})
	if out.Items == nil && out.Status.Availability == Known {
		out.Items = []Edge{}
	}
	return out
}

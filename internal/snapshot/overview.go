package snapshot

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

func (p OverviewProvider) CollectOverview() Overview {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	start := now().UTC()
	c := &collector{api: p.API, now: now, limits: Limits{Pages: 3, Issues: 300,
		Requests: 80}, deadline: start.Add(time.Minute), repos: map[string]repoResult{}}
	status := c.status("installed-core+github")
	out := Overview{SchemaVersion: 1, Subject: p.Subject, ObservedAt: start,
		Discovery: status, Repositories: Section[Repository]{Status: status,
			Items: []Repository{}}, Issues: Section[OverviewIssue]{Status: status,
			Items: []OverviewIssue{}}, Runners: Section[LocalRunner]{Status: status,
			Items: []LocalRunner{}}}
	local := UnavailableObservation("UNAVAILABLE")
	if p.Observer != nil {
		local = p.Observer.Observe()
	}
	out.LocalStatus, out.LocalCode, out.Errors = local.Status, local.Code, local.Errors
	out.LocalInterval, out.LocalCompleteness = local.Interval, local.Completeness
	if local.Status != "observed" || local.Subject.ID != p.Subject.ID ||
		!strings.EqualFold(local.Subject.Login, p.Subject.Login) {
		reason := Unavailable
		if local.Status == "denied" {
			reason = Permission
		}
		out.Discovery = failed(status, reason)
		out.Runners.Status, out.Issues.Status = out.Discovery, out.Discovery
		out.Repositories.Status = out.Discovery
		return finishOverview(out, now())
	}
	out.Work = local.Work
	if local.Completeness != "complete" {
		out.Discovery = failed(status, Unavailable)
		out.Runners.Status = out.Discovery
	}
	routes := map[int64]AdoptedRoute{}
	runners := map[string]LocalRunner{}
	for _, runner := range local.Runners {
		if previous, exists := runners[runner.Key]; exists {
			for _, route := range previous.Routes {
				if !containsRoute(runner.Routes, route) {
					runner.Routes = append(runner.Routes, route)
				}
			}
		}
		runners[runner.Key] = runner
		for _, route := range runner.Routes {
			routes[route.RepositoryID] = route
		}
	}
	for _, runner := range runners {
		out.Runners.Items = append(out.Runners.Items, runner)
	}
	// Shared-host inspection supplies adoption IDs even with no scope Issues.
	for _, id := range local.AdoptedIDs {
		if _, exists := routes[id]; exists {
			continue
		}
		var raw struct {
			ID   int64  `json:"id"`
			Name string `json:"full_name"`
		}
		if id <= 0 || c.call(fmt.Sprintf("repositories/%d", id), &raw) != None ||
			raw.ID != id || !ValidRepository(raw.Name) {
			out.Discovery = failed(status, Unavailable)
			continue
		}
		routes[id] = AdoptedRoute{raw.Name, id}
	}
	// An explicit empty inventory differs from a missing supplementary inspection.
	if len(routes) == 0 && local.AdoptedIDs == nil {
		out.Discovery = failed(status, Unavailable)
	}
	if len(routes) > 32 {
		out.Discovery = failed(status, Limit)
	}
	var subject Subject
	identityReason := c.call("user", &subject)
	if identityReason == None && (subject.ID != p.Subject.ID ||
		!strings.EqualFold(subject.Login, p.Subject.Login)) {
		identityReason = Permission
	}
	if identityReason != None {
		out.Discovery = failed(status, identityReason)
		out.Repositories.Status, out.Issues.Status = out.Discovery, out.Discovery
		return finishOverview(out, now())
	}
	ids := make([]int64, 0, len(routes))
	for id := range routes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for index, id := range ids {
		if index >= 32 {
			break
		}
		if len(out.Issues.Items) >= 1000 {
			out.Repositories.Status = failed(status, Limit)
			out.Issues.Status = failed(status, Limit)
			break
		}
		route := routes[id]
		var raw struct {
			ID          int64           `json:"id"`
			Name        string          `json:"full_name"`
			Private     bool            `json:"private"`
			Permissions map[string]bool `json:"permissions"`
		}
		reason := c.call("repos/"+route.Repository, &raw)
		if reason == None && (raw.ID != id || !strings.EqualFold(raw.Name,
			route.Repository) || raw.Private || !raw.Permissions["pull"]) {
			reason = Permission
		}
		if reason != None {
			out.Repositories.Status = failed(status, reason)
			out.Issues.Status = failed(status, reason)
			continue
		}
		repo := Repository{ID: id, Name: raw.Name, URL: "https://github.com/" + raw.Name}
		out.Repositories.Items = append(out.Repositories.Items, repo)
		issues, collected := c.assignedIssues(repo, p.Subject, local.Work,
			1000-len(out.Issues.Items))
		out.Issues.Items = append(out.Issues.Items, issues...)
		if !collected.Complete {
			out.Issues.Status = collected
		}
	}
	if !out.Discovery.Complete {
		out.Repositories.Status.Complete = false
		out.Issues.Status.Complete = false
	}
	sort.Slice(out.Runners.Items, func(i, j int) bool {
		return out.Runners.Items[i].Key < out.Runners.Items[j].Key
	})
	return finishOverview(out, now())
}

func containsRoute(routes []AdoptedRoute, target AdoptedRoute) bool {
	for _, route := range routes {
		if route == target {
			return true
		}
	}
	return false
}

func finishOverview(out Overview, now time.Time) Overview {
	out.ObservedAt = now.UTC()
	out.ExpiresAt = now.UTC().Add(30 * time.Second)
	return out
}

func (c *collector) assignedIssues(repo Repository, subject Subject,
	work []LocalWork, remaining int) ([]OverviewIssue, Status) {
	out := []OverviewIssue{}
	path := "repos/" + repo.Name + "/issues?state=open&assignee=" +
		url.QueryEscape(subject.Login) + "&sort=created&direction=asc"
	visit := func(data json.RawMessage) (int64, error) {
		var raw struct {
			nativeIssue
			NodeID string `json:"node_id"`
		}
		if json.Unmarshal(data, &raw) != nil {
			return 0, invalid
		}
		identity, err := issueIdentity(raw.nativeIssue, repo)
		if err != nil {
			return 0, err
		}
		if isPR(raw.nativeIssue) {
			return raw.ID, nil
		}
		assigned := false
		for _, assignee := range raw.Assignees {
			assigned = assigned || assignee.ID == subject.ID &&
				strings.EqualFold(assignee.Login, subject.Login)
		}
		if !assigned || raw.State != "open" || raw.Title == "" ||
			len(raw.Title) > 1024 || len(raw.Assignees) > 100 ||
			!nodeIssueID.MatchString(raw.NodeID) {
			return 0, invalid
		}
		row := OverviewIssue{Key: fmt.Sprintf("%d:%s", repo.ID, raw.NodeID),
			Identity: identity, NodeID: raw.NodeID, Title: raw.Title, State: raw.State,
			Assignees: raw.Assignees, Observed: c.now().UTC(), Evidence: []LocalWork{}}
		for _, evidence := range work {
			issue := evidence.Issue
			if issue != nil && issue.RepositoryID == repo.ID &&
				strings.EqualFold(issue.Repository, repo.Name) &&
				issue.Number == raw.Number && issue.ID == raw.NodeID {
				row.Evidence = append(row.Evidence, evidence)
			}
		}
		out = append(out, row)
		return raw.ID, nil
	}
	status := c.list(path, "", min(300, remaining), visit)
	return out, status
}

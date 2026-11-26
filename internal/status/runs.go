package status

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// runScan is one repository whose run list is scanned once, and the listed
// Issues whose runs can be in it.
type runScan struct {
	repo   string // OWNER/REPO on the reader's host
	issues []*Issue
	seen   map[*Issue]bool
}

func (s *runScan) add(is *Issue) {
	if !s.seen[is] {
		s.seen[is] = true
		s.issues = append(s.issues, is)
	}
}

func addRunsErr(is *Issue, e *op.Error) {
	if is.RunsError == nil {
		is.RunsError = e
		return
	}
	is.RunsError = op.AsError(op.Errorf(is.RunsError.Code, "%s; %s", is.RunsError.Error(), e.Error()))
}

// issueRuns lists each group's Issues' Nibfin runs: the runs titled
// "Nibfin · OWNER/REPO#N · …" in the Issue's repository and in the execution
// repository. Each repository's run list is scanned once, from the oldest
// creation time of the Issues whose runs can be in it, and grouped by title.
func (r *reader) issueRuns(ctx context.Context, groups []*repoGroup) {
	var all []*Issue
	repoOf := map[*Issue]string{}
	scans := map[string]*runScan{}
	var order []string
	scan := func(repo string) *runScan {
		k := strings.ToLower(repo)
		s := scans[k]
		if s == nil {
			s = &runScan{repo: repo, seen: map[*Issue]bool{}}
			scans[k] = s
			order = append(order, k)
		}
		return s
	}
	for _, g := range groups {
		s := scan(g.repo)
		for _, is := range g.issues {
			s.add(is)
			all = append(all, is)
			repoOf[is] = g.repo
		}
	}

	exec, known := r.execRepo()
	switch {
	case !known:
		e := op.AsError(op.Errorf(op.CodeGitHubAPIFailed, "execution repository unknown: %s", r.runners.Error.Code))
		for _, is := range all {
			addRunsErr(is, e)
		}
	case exec != "":
		parts, err := splitRepo(exec)
		if err != nil {
			e := op.AsError(op.Errorf(op.CodeGitHubAPIFailed, "execution repository unknown: %s", op.AsError(err).Error()))
			for _, is := range all {
				addRunsErr(is, e)
			}
			break
		}
		// The execution repository's run titles carry no host: every listed
		// Issue on its host can have runs there. Every listed Issue is on
		// the reader's host, so another host has none.
		if strings.EqualFold(parts[0], r.host) && len(all) > 0 {
			s := scan(parts[1] + "/" + parts[2])
			for _, is := range all {
				s.add(is)
			}
		}
	}

	created := map[*Issue]time.Time{}
	for _, is := range all {
		t, err := time.Parse(time.RFC3339, is.CreatedAt)
		if err != nil {
			addRunsErr(is, op.AsError(op.Errorf(op.CodeGitHubAPIFailed, "creation time of %s#%d unknown", repoOf[is], is.Number)))
			continue
		}
		created[is] = t
	}

	for _, k := range order {
		s := scans[k]
		var from time.Time
		for _, is := range s.issues {
			if t, ok := created[is]; ok && (from.IsZero() || t.Before(from)) {
				from = t
			}
		}
		if from.IsZero() {
			continue
		}
		runs, truncated, err := r.listRuns(ctx, s.repo, from)
		if err != nil {
			for _, is := range s.issues {
				addRunsErr(is, err)
			}
			continue
		}
		byKey := map[string]*Issue{}
		for _, is := range s.issues {
			byKey[strings.ToLower(repoOf[is]+"#"+strconv.Itoa(is.Number))] = is
		}
		for _, run := range runs {
			if is := byKey[IssueOfTitle(run.Name)]; is != nil {
				is.Runs = append(is.Runs, run)
			}
		}
		if truncated {
			for _, is := range s.issues {
				is.RunsTruncated = true
			}
		}
	}

	for _, is := range all {
		sort.SliceStable(is.Runs, func(i, j int) bool {
			a, b := is.Runs[i], is.Runs[j]
			ta, ea := time.Parse(time.RFC3339, a.CreatedAt)
			tb, eb := time.Parse(time.RFC3339, b.CreatedAt)
			if ea == nil && eb == nil && !ta.Equal(tb) {
				return ta.After(tb)
			}
			if ea != nil || eb != nil {
				if a.CreatedAt != b.CreatedAt {
					return a.CreatedAt > b.CreatedAt
				}
			}
			return a.URL < b.URL
		})
	}
}

// IssueOfTitle returns "owner/repo#n", lower case, from a run title
// "Nibfin · OWNER/REPO#N · …", or "" for any other title (the @ref route and
// wake runs among them).
func IssueOfTitle(title string) string {
	repo, n, _, ok := parseTitle(title)
	if !ok {
		return ""
	}
	return strings.ToLower(repo + "#" + strconv.Itoa(n))
}

// stages are the stage values of the shared-runner template's run titles.
var stages = map[string]bool{"route": true, "shape": true, "deliver": true, "review": true, "finish": true}

// TitleJob returns the Issue's OWNER/REPO and number, as the title writes
// them, and the stage of a run title "Nibfin · OWNER/REPO#N · <stage>".
// stage is empty when the last segment is not one of the template's stages;
// ok is false for every title IssueOfTitle rejects.
func TitleJob(title string) (repo string, number int, stage string, ok bool) {
	repo, number, rest, ok := parseTitle(title)
	if !ok {
		return "", 0, "", false
	}
	if s := strings.ToLower(rest); stages[s] {
		stage = s
	}
	return repo, number, stage, true
}

// parseTitle splits "Nibfin · OWNER/REPO#N · rest". The prefix is compared
// without case; repo and rest keep the title's case.
func parseTitle(title string) (repo string, number int, rest string, ok bool) {
	if len(title) < len(runPrefix) || !strings.EqualFold(title[:len(runPrefix)], runPrefix) {
		return "", 0, "", false
	}
	head, rest, ok := strings.Cut(title[len(runPrefix):], " · ")
	if !ok {
		return "", 0, "", false
	}
	repo, num, ok := strings.Cut(head, "#")
	if !ok || num == "" || strings.ContainsAny(repo, "@# ") {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 || strconv.Itoa(n) != num {
		return "", 0, "", false
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", 0, "", false
	}
	return repo, n, rest, true
}

type runJSON struct {
	Name         *string `json:"name"`
	DisplayTitle string  `json:"display_title"`
	Status       string  `json:"status"`
	Conclusion   *string `json:"conclusion"`
	CreatedAt    string  `json:"created_at"`
	HTMLURL      string  `json:"html_url"`
}

// listRuns lists repo's runs created at or after from, up to GitHub's
// 1,000 results; truncated is true when that limit was reached.
func (r *reader) listRuns(ctx context.Context, repo string, from time.Time) ([]Run, bool, *op.Error) {
	created := ">=" + from.UTC().Format("2006-01-02T15:04:05") + "+00:00"
	var out []Run
	seen := 0
	for page := 1; ; page++ {
		q := url.Values{"created": {created}, "per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(page)}}
		var body struct {
			Runs []runJSON `json:"workflow_runs"`
		}
		if err := r.get(ctx, base(repo)+"/actions/runs?"+q.Encode(), &body); err != nil {
			return nil, false, apiErr(op.CodeGitHubAPIFailed, "list the runs of "+repo, err)
		}
		for _, j := range body.Runs {
			run := Run{Repo: repo, Name: j.DisplayTitle, Status: j.Status, Conclusion: j.Conclusion, CreatedAt: j.CreatedAt, URL: j.HTMLURL}
			if j.Name != nil {
				run.Workflow = *j.Name
			}
			out = append(out, run)
		}
		seen += len(body.Runs)
		if len(body.Runs) < perPage {
			return out, false, nil
		}
		if seen >= maxResults {
			return out, true, nil
		}
	}
}

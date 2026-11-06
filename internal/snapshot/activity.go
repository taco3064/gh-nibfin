package snapshot

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type nativeRepo struct {
	ID   int64  `json:"id"`
	Name string `json:"full_name"`
	URL  string `json:"url"`
}

func matchesRepo(raw nativeRepo, repo Repository) bool {
	nameOK := strings.EqualFold(raw.Name, repo.Name)
	urlOK := strings.EqualFold(raw.URL, "https://api.github.com/repos/"+repo.Name)
	return raw.ID == repo.ID && (nameOK || raw.Name == "" && urlOK) &&
		(raw.URL == "" || urlOK)
}

func (c *collector) pulls(repo Repository) Section[PullRequest] {
	out := Section[PullRequest]{}
	numbers := map[int]bool{}
	cache := map[string]Section[Check]{}
	path := "repos/" + repo.Name + "/pulls?state=all&sort=created&direction=asc"
	out.Status = c.list(path, "", 2000, func(data json.RawMessage) (int64, error) {
		var raw struct {
			ID       int64      `json:"id"`
			Number   int        `json:"number"`
			State    string     `json:"state"`
			MergedAt *time.Time `json:"merged_at"`
			Base     struct {
				Repo nativeRepo `json:"repo"`
			} `json:"base"`
			Head struct {
				SHA string `json:"sha"`
			} `json:"head"`
		}
		if json.Unmarshal(data, &raw) != nil || raw.ID <= 0 || raw.Number <= 0 ||
			numbers[raw.Number] || !matchesRepo(raw.Base.Repo, repo) ||
			(raw.State != "open" && raw.State != "closed") ||
			!revisionPattern.MatchString(raw.Head.SHA) {
			return 0, invalid
		}
		numbers[raw.Number] = true
		checks, ok := cache[raw.Head.SHA]
		if !ok {
			checks = c.checks(repo, raw.Head.SHA)
			cache[raw.Head.SHA] = checks
		}
		id := Identity{repo, raw.ID, raw.Number,
			repo.URL + "/pull/" + strconv.Itoa(raw.Number)}
		out.Items = append(out.Items, PullRequest{id, raw.State, raw.MergedAt,
			raw.Head.SHA, checks})
		return raw.ID, nil
	})
	if out.Items == nil && out.Status.Availability == Known {
		out.Items = []PullRequest{}
	}
	return out
}

func (c *collector) checks(repo Repository, revision string) Section[Check] {
	out := Section[Check]{}
	path := "repos/" + repo.Name + "/commits/" + revision + "/check-runs?filter=all"
	out.Status = c.list(path, "check_runs", 2000,
		func(data json.RawMessage) (int64, error) {
			var raw struct {
				ID         int64   `json:"id"`
				Head       string  `json:"head_sha"`
				Name       string  `json:"name"`
				Status     string  `json:"status"`
				Conclusion *string `json:"conclusion"`
				HTMLURL    string  `json:"html_url"`
			}
			if json.Unmarshal(data, &raw) != nil || raw.ID <= 0 || raw.Head != revision ||
				!nativeStatus(raw.Status) || !nativeConclusion(raw.Conclusion) {
				return 0, invalid
			}
			// Check links can target Actions jobs or check UIs. Only retain exact
			// official resource paths; never expose an app's arbitrary details_url.
			link := safeCheckURL(raw.HTMLURL, repo, raw.ID)
			out.Items = append(out.Items, Check{raw.ID, raw.Head, raw.Name,
				raw.Status, raw.Conclusion, link})
			return raw.ID, nil
		})
	if out.Items == nil && out.Status.Availability == Known {
		out.Items = []Check{}
	}
	return out
}

func safeCheckURL(link string, repo Repository, id int64) string {
	prefix := repo.URL + "/"
	if !strings.HasPrefix(link, prefix) {
		return ""
	}
	path := strings.TrimPrefix(link, prefix)
	identifier := strconv.FormatInt(id, 10)
	for _, pattern := range []string{
		`^runs/` + identifier + `$`,
		`^actions/runs/[1-9][0-9]*/job/` + identifier + `$`,
		`^pull/[1-9][0-9]*/checks\?check_run_id=` + identifier + `$`,
	} {
		if regexp.MustCompile(pattern).MatchString(path) {
			return link
		}
	}
	return ""
}

func nativeStatus(value string) bool {
	switch value {
	case "queued", "in_progress", "completed", "waiting", "requested", "pending":
		return true
	default:
		return false
	}
}

func nativeConclusion(value *string) bool {
	if value == nil {
		return true
	}
	switch *value {
	case "success", "failure", "neutral", "cancelled", "skipped", "timed_out",
		"action_required", "stale", "startup_failure":
		return true
	default:
		return false
	}
}

func (c *collector) runs(repo Repository) Section[Run] {
	out := Section[Run]{}
	out.Status = c.list("repos/"+repo.Name+"/actions/runs", "workflow_runs", 2000,
		func(data json.RawMessage) (int64, error) {
			var raw struct {
				ID         int64      `json:"id"`
				Attempt    int        `json:"run_attempt"`
				Head       string     `json:"head_sha"`
				Status     string     `json:"status"`
				Conclusion *string    `json:"conclusion"`
				Repository nativeRepo `json:"repository"`
				PRs        []struct {
					ID     int64 `json:"id"`
					Number int   `json:"number"`
					Head   struct {
						SHA string `json:"sha"`
					} `json:"head"`
					Base struct {
						Repo nativeRepo `json:"repo"`
					} `json:"base"`
				} `json:"pull_requests"`
			}
			if json.Unmarshal(data, &raw) != nil || raw.ID <= 0 || raw.Attempt <= 0 ||
				!revisionPattern.MatchString(raw.Head) || !matchesRepo(raw.Repository, repo) ||
				!nativeStatus(raw.Status) || !nativeConclusion(raw.Conclusion) || raw.PRs == nil {
				return 0, invalid
			}
			prs := []Identity{}
			seen := map[int64]bool{}
			for _, pr := range raw.PRs {
				if pr.ID <= 0 || pr.Number <= 0 || seen[pr.ID] ||
					!matchesRepo(pr.Base.Repo, repo) || pr.Head.SHA != raw.Head {
					return 0, invalid
				}
				seen[pr.ID] = true
				prs = append(prs, Identity{repo, pr.ID, pr.Number,
					repo.URL + "/pull/" + strconv.Itoa(pr.Number)})
			}
			link := fmt.Sprintf("%s/actions/runs/%d", repo.URL, raw.ID)
			out.Items = append(out.Items, Run{repo, raw.ID, raw.Attempt, raw.Head,
				raw.Status, raw.Conclusion, link, prs})
			return raw.ID, nil
		})
	if out.Items == nil && out.Status.Availability == Known {
		out.Items = []Run{}
	}
	return out
}

package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type API interface {
	Call(string, string, any, any) error
}

// Limits may only lower the hard bounds. Zero selects the documented default.
type Limits struct {
	Pages    int
	Issues   int
	Requests int
	Duration time.Duration
}

type Provider struct {
	API    API
	Now    func() time.Time
	Limits Limits
}

type collector struct {
	api      API
	now      func() time.Time
	limits   Limits
	requests int
	deadline time.Time
	repos    map[string]repoResult
}

type repoResult struct {
	repo   Repository
	status Status
}

var repositoryPattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}$`)
var revisionPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

// ValidRepository validates the shared bounded GitHub owner/repository syntax.
func ValidRepository(name string) bool {
	return repositoryPattern.MatchString(name) &&
		!strings.HasSuffix(name, "/.") && !strings.HasSuffix(name, "/..")
}

func bounded(value, maximum int) int {
	if value <= 0 || value > maximum {
		return maximum
	}
	return value
}

func (p Provider) Collect(name string) Snapshot {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	start := now().UTC()
	limits := p.Limits
	limits.Pages = bounded(limits.Pages, 20)
	limits.Issues = bounded(limits.Issues, 1000)
	limits.Requests = bounded(limits.Requests, 5000)
	if limits.Duration <= 0 || limits.Duration > 2*time.Minute {
		limits.Duration = 2 * time.Minute
	}
	c := &collector{p.API, now, limits, 0, start.Add(limits.Duration),
		map[string]repoResult{}}
	s := Snapshot{SchemaVersion: 1, StartedAt: start,
		ExpiresAt: start.Add(5 * time.Minute), Core: unknownCore()}
	r := c.repository(name)
	s.Repository.Status = r.status
	if r.status.Availability == Known {
		s.Repository.Items = []Repository{r.repo}
		s.Issues = c.issues(r.repo)
		s.PullRequests = c.pulls(r.repo)
		s.Runs = c.runs(r.repo)
	} else {
		s.Issues.Status = r.status
		s.PullRequests.Status = r.status
		s.Runs.Status = r.status
	}
	s.FinishedAt = now().UTC()
	return s.At(s.FinishedAt)
}

func (c *collector) status(source string) Status {
	return Status{Known, true, None, c.now().UTC(), source}
}

func failed(s Status, reason Reason) Status {
	s.Availability, s.Complete, s.Reason = Unknown, false, reason
	if reason == Permission {
		s.Availability = Denied
	}
	return s
}

func (c *collector) call(path string, output any) Reason {
	if c.requests >= c.limits.Requests || !c.now().Before(c.deadline) {
		return Limit
	}
	c.requests++
	if c.api == nil {
		return Unavailable
	}
	err := c.api.Call("GET", path, nil, output)
	if err == nil {
		if !c.now().Before(c.deadline) {
			return Limit
		}
		return None
	}
	// Exact safe codes only. Raw errors never enter the snapshot.
	switch err.Error() {
	case "PERMISSION_DENIED":
		return Permission
	case "NOT_FOUND":
		return NotFound
	case "RATE_LIMITED":
		return RateLimited
	case "INVALID_PROVIDER_RESULT":
		return Invalid
	default:
		return Unavailable
	}
}

func (c *collector) repository(name string) repoResult {
	key := strings.ToLower(name)
	if r, ok := c.repos[key]; ok {
		return r
	}
	r := repoResult{status: c.status("")}
	if !ValidRepository(name) {
		r.status = failed(r.status, InvalidRepository)
		return r
	}
	r.status.Source = "repos/" + name
	var raw struct {
		ID   int64  `json:"id"`
		Name string `json:"full_name"`
	}
	reason := c.call(r.status.Source, &raw)
	if reason == None && (raw.ID <= 0 || !ValidRepository(raw.Name) ||
		!strings.EqualFold(raw.Name, name)) {
		reason = Invalid
	}
	if reason != None {
		r.status = failed(r.status, reason)
	} else {
		for _, existing := range c.repos {
			if existing.status.Availability == Known && existing.repo.ID == raw.ID &&
				!strings.EqualFold(existing.repo.Name, raw.Name) {
				r.status = failed(r.status, Invalid)
				c.repos[key] = r
				return r
			}
		}
		r.repo = Repository{raw.ID, raw.Name, "https://github.com/" + raw.Name}
	}
	c.repos[key] = r
	return r
}

// list handles array endpoints and total_count envelopes. A full final array
// requires an extra empty page. Unique IDs and fixed totals detect common races.
func (c *collector) list(path, field string, maximum int,
	visit func(json.RawMessage) (int64, error)) Status {
	s := c.status(path)
	seen := map[int64]bool{}
	total := -1
	for page := 1; page <= c.limits.Pages; page++ {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		endpoint := fmt.Sprintf("%s%sper_page=100&page=%d", path, sep, page)
		var raw json.RawMessage
		if reason := c.call(endpoint, &raw); reason != None {
			return failed(s, reason)
		}
		var items []json.RawMessage
		if field == "" {
			if json.Unmarshal(raw, &items) != nil || items == nil {
				return failed(s, Invalid)
			}
		} else {
			var envelope map[string]json.RawMessage
			if json.Unmarshal(raw, &envelope) != nil {
				return failed(s, Invalid)
			}
			var count int
			if len(envelope["total_count"]) == 0 ||
				string(envelope["total_count"]) == "null" ||
				json.Unmarshal(envelope["total_count"], &count) != nil || count < 0 ||
				json.Unmarshal(envelope[field], &items) != nil || items == nil {
				return failed(s, Invalid)
			}
			if total >= 0 && total != count {
				return failed(s, Changed)
			}
			total = count
		}
		if len(items) > 100 || total >= 0 && len(seen)+len(items) > total {
			return failed(s, Changed)
		}
		for _, item := range items {
			if len(seen) >= maximum {
				return failed(s, Limit)
			}
			// Check duplicates before visit so partial results contain no repeats.
			var identity struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(item, &identity) != nil || identity.ID <= 0 {
				return failed(s, Invalid)
			}
			if seen[identity.ID] {
				return failed(s, Changed)
			}
			id, err := visit(item)
			if err != nil || id != identity.ID {
				return failed(s, Invalid)
			}
			seen[id] = true
		}
		if total >= 0 {
			if len(seen) == total {
				return s
			}
			if len(items) < 100 {
				return failed(s, Changed)
			}
		} else if len(items) < 100 {
			return s
		}
	}
	return failed(s, Limit)
}

var invalid = errors.New("invalid provider identity")

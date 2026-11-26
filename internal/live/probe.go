package live

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/status"
)

// Workflow files whose runs carry Nibfin Issue titles: the Issue
// repository's caller and the execution repository's shared runner.
const (
	issueWorkflow  = "nibfin-issue.yml"
	sharedWorkflow = "nibfin-shared-runner.yml"
)

const (
	perPage = 100
	// maxPages bounds the assigned-Issues probe at 1,000 Issues, the most
	// the person view's search returns.
	maxPages = 10
)

type probeKind int

const (
	// assigned lists the open Issues with one label (scope or request)
	// assigned to the gh user. The /issues list ANDs its labels, so each
	// label has its own probe.
	assigned probeKind = iota
	// runs lists one Nibfin workflow's runs of a repository.
	runs
	// folder lists a repository's workflow files.
	folder
)

// probe is one conditional GET with fixed parameters. It keeps each page's
// last ETag, so an unchanged answer is a 304.
type probe struct {
	kind     probeKind
	repo     string // OWNER/REPO; empty for assigned
	label    string // assigned only
	workflow string // runs only

	pages []page
	// since is GitHub's Date of the last 200 of a runs probe: runs updated
	// at or after it are new. Zero before the first 200.
	since time.Time
	// wait is the ticks between checks, from max(30 s, x-poll-interval);
	// due counts down to the next check.
	wait, due int
	// ok is when the probe last checked successfully; zero before.
	ok time.Time
	// stopped is a 404: the probe polls nothing until the next snapshot.
	stopped bool
	// missed is a failure before any answer: the first answer then counts
	// as a change, not as a baseline.
	missed bool
	// failing is set while the last check failed; no checked is sent then.
	failing bool
}

type page struct {
	etag  string
	count int
}

func (p *probe) id() string {
	switch p.kind {
	case runs:
		return "runs " + strings.ToLower(p.repo) + " " + p.workflow
	case folder:
		return "folder " + strings.ToLower(p.repo)
	}
	return "assigned " + p.label
}

func base(repo string) string {
	owner, name, _ := strings.Cut(repo, "/")
	return "repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

func (p *probe) path(n int) string {
	switch p.kind {
	case runs:
		return base(p.repo) + "/actions/workflows/" + url.PathEscape(p.workflow) + "/runs?per_page=" + fmt.Sprint(perPage)
	case folder:
		return base(p.repo) + "/contents/.github/workflows"
	}
	q := url.Values{
		"filter": {"assigned"}, "state": {"open"}, "labels": {p.label},
		"sort": {"created"}, "direction": {"asc"},
		"per_page": {fmt.Sprint(perPage)}, "page": {fmt.Sprint(n)},
	}
	return "issues?" + q.Encode()
}

func (p *probe) what() string {
	switch p.kind {
	case runs:
		return p.workflow + " runs of " + p.repo
	case folder:
		return "the workflow files of " + p.repo
	}
	return "the open " + p.label + " Issues assigned to you"
}

// outcome is what one check found.
type outcome struct {
	// full asks for a full person-view read.
	full bool
	// dirty are the listed Issues (owner/repo#n, lower case) with newer
	// Nibfin runs.
	dirty []string
	// date is the newest GitHub Date of the check's answers.
	date time.Time
	// retry is a failure's Retry-After; zero when none.
	retry time.Duration
	err   *op.Error
}

// check sends the probe's conditional GETs. A probe without a previous
// answer only records one (its baseline) unless report is set; listed
// says whether an Issue key is in the person view.
func (p *probe) check(ctx context.Context, g getter, now time.Time, listed func(string) bool) outcome {
	var out outcome
	report := len(p.pages) > 0 || p.missed
	fail := func(err error) outcome {
		if notFound(err) {
			p.stopped = true
			out.err = nil
			return out
		}
		out.err = checkErr(p.what(), err)
		if len(p.pages) == 0 {
			p.missed = true
		}
		out.retry = retryAfter(err, now)
		return out
	}
	var poll time.Duration
	var pages []page
	changed := false
	var runsBody []byte
	for n := 1; ; n++ {
		etag := ""
		if n <= len(p.pages) {
			etag = p.pages[n-1].etag
		}
		a, err := g.get(ctx, p.path(n), etag)
		if err != nil {
			return fail(err)
		}
		poll = max(poll, a.poll)
		if a.date.After(out.date) {
			out.date = a.date
		}
		pg := page{etag: a.etag}
		if a.status == 304 && n <= len(p.pages) {
			pg.count = p.pages[n-1].count
		} else {
			changed = true
			if p.kind == runs {
				runsBody = a.body
			}
			if p.kind == assigned {
				var items []json.RawMessage
				if err := json.Unmarshal(a.body, &items); err != nil {
					return fail(fmt.Errorf("unexpected answer: %v", err))
				}
				pg.count = len(items)
			}
		}
		pages = append(pages, pg)
		if p.kind != assigned || pg.count < perPage || n == maxPages {
			break
		}
	}
	if len(pages) != len(p.pages) && len(p.pages) > 0 {
		changed = true
	}

	if changed && p.kind == runs {
		var body struct {
			Runs []struct {
				DisplayTitle string `json:"display_title"`
				UpdatedAt    string `json:"updated_at"`
			} `json:"workflow_runs"`
		}
		if err := json.Unmarshal(runsBody, &body); err != nil {
			return fail(fmt.Errorf("unexpected answer: %v", err))
		}
		if !p.since.IsZero() {
			seen := map[string]bool{}
			for _, r := range body.Runs {
				k := status.IssueOfTitle(r.DisplayTitle)
				t, err := time.Parse(time.RFC3339, r.UpdatedAt)
				if k == "" || err != nil || t.Before(p.since) || seen[k] || !listed(k) {
					continue
				}
				seen[k] = true
				out.dirty = append(out.dirty, k)
			}
		}
		if !out.date.IsZero() {
			p.since = out.date
		} else {
			p.since = now
		}
	} else if changed && report {
		out.full = true
	}

	p.pages, p.missed = pages, false
	p.wait = ticksFor(max(interval, poll))
	p.due = p.wait
	p.ok = now
	return out
}

// ticksFor is d in whole ticks, at least one.
func ticksFor(d time.Duration) int {
	n := int((d + interval - 1) / interval)
	return max(n, 1)
}

package status

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// Partial receives the Result read so far and what is still being read.
// res is a copy: every value is its JSON, so nothing in it changes after
// the call. loading names the Result keys still being read and, for the
// Issues, the Issue fields still being read (pendingComment, blockedBy and
// runs).
type Partial func(res op.Result, loading []string)

type partialKey struct{}

// WithPartial returns ctx carrying fn. A status ReadBack under ctx calls fn
// each time a section of its Result is read, while other sections are
// still being read; its calls are serialized, and none comes after
// ReadBack returns. Without it, ReadBack reports nothing before it returns.
func WithPartial(ctx context.Context, fn Partial) context.Context {
	return context.WithValue(ctx, partialKey{}, fn)
}

// PartialFrom returns the sink WithPartial put in ctx, or nil.
func PartialFrom(ctx context.Context) Partial {
	fn, _ := ctx.Value(partialKey{}).(Partial)
	return fn
}

// builder makes a view's Result from what is read; host and latest are nil
// while they are still being read.
type builder func(host *op.HostReport, latest *LatestImage) op.Result

// progress collects the sections of one ReadBack as they finish and sends
// each new state to the sink. Without a sink it does nothing.
type progress struct {
	sink Partial

	mu      sync.Mutex
	loading []string
	host    *op.HostReport
	latest  *LatestImage
	view    builder
}

func newProgress(ctx context.Context, view builder, loading ...string) *progress {
	return &progress{sink: PartialFrom(ctx), view: view, loading: loading}
}

func (p *progress) on() bool { return p.sink != nil }

func (p *progress) setHost(hr op.HostReport) {
	if !p.on() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.host = &hr
	p.done("host")
}

func (p *progress) setLatest(l LatestImage) {
	if !p.on() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.latest = &l
	p.done("latestImage")
}

// step takes the GitHub reads' new state. snap is called only with a sink,
// by the goroutine that owns the read data, and must return a builder over
// copies of it.
func (p *progress) step(snap func() builder, keys ...string) {
	if !p.on() {
		return
	}
	b := snap()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.view = b
	p.done(keys...)
}

// done drops keys from loading and sends the new state while anything is
// still loading; the complete Result is ReadBack's own. p.mu is held.
func (p *progress) done(keys ...string) {
	p.loading = slices.DeleteFunc(p.loading, func(k string) bool { return slices.Contains(keys, k) })
	if len(p.loading) == 0 {
		return
	}
	b, err := json.Marshal(p.view(p.host, p.latest))
	if err != nil {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return
	}
	res := op.Result{}
	for k, v := range raw {
		res[k] = v
	}
	p.sink(res, slices.Clone(p.loading))
}

// withLatest compares a read pin with the latest image once both are known.
func withLatest(img RepoImage, latest *LatestImage) RepoImage {
	img.Latest = nil
	if img.Ref != nil && latest != nil && latest.Ref != "" {
		same := *img.Ref == latest.Ref
		img.Latest = &same
	}
	return img
}

// withExecution marks the execution repository among repos once the host's
// runner report is known; ghHost is the reader's host.
func withExecution(repos []NibfinRepo, host *op.HostReport, ghHost string) []NibfinRepo {
	if repos == nil {
		return nil
	}
	out := slices.Clone(repos)
	for i := range out {
		out[i].Execution = nil
		if host == nil || host.Runners.Error != nil {
			continue
		}
		exec := host.Runners.Repo
		is := exec != "" && strings.EqualFold(ghHost+"/"+out[i].Repo, exec)
		out[i].Execution = &is
	}
	return out
}

func cloneIssues(in []Issue) []Issue {
	if in == nil {
		return nil
	}
	out := make([]Issue, len(in))
	for i, is := range in {
		is.Labels = slices.Clone(is.Labels)
		is.BlockedBy = slices.Clone(is.BlockedBy)
		is.Runs = slices.Clone(is.Runs)
		out[i] = is
	}
	return out
}

// repoRead is the GitHub part of the repository view as read so far.
type repoRead struct {
	ghHost    string
	repo      string // HOST/OWNER/REPO
	listed    bool
	issues    []Issue
	issuesErr *op.Error
	image     *RepoImage
	run       *NibfinRun
	permRead  bool
	perm      *string
	permErr   *op.Error
	reposRead bool
	repos     []NibfinRepo
	reposErr  *op.Error
}

func (d *repoRead) clone() builder {
	c := *d
	c.issues = cloneIssues(d.issues)
	if d.image != nil {
		img := *d.image
		c.image = &img
	}
	if d.run != nil {
		run := *d.run
		c.run = &run
	}
	c.repos = slices.Clone(d.repos)
	return c.result
}

func (d repoRead) result(host *op.HostReport, latest *LatestImage) op.Result {
	res := op.Result{"repo": d.repo}
	if host != nil {
		res["host"] = *host
	}
	if latest != nil {
		res["latestImage"] = *latest
	}
	if d.image != nil {
		res["image"] = withLatest(*d.image, latest)
	}
	if d.listed {
		res["issues"] = d.issues
		if d.issuesErr != nil {
			res["issuesError"] = d.issuesErr
		}
	}
	if d.run != nil {
		res["nibfinRun"] = *d.run
	}
	if d.permRead {
		res["permission"] = d.perm
		if d.permErr != nil {
			res["permissionError"] = d.permErr
		}
	}
	if d.reposRead {
		res["nibfinRepos"] = withExecution(d.repos, host, d.ghHost)
		if d.reposErr != nil {
			res["nibfinReposError"] = d.reposErr
		}
	}
	return res
}

// personRead is the GitHub part of the person view as read so far.
type personRead struct {
	ghHost    string
	userRead  bool
	user      string
	userErr   *op.Error
	listed    bool
	repos     []RepoIssues
	truncated bool
	issuesErr *op.Error
	reposRead bool
	nibfin    []NibfinRepo
	nibfinErr *op.Error
}

func (d *personRead) clone() builder {
	c := *d
	c.repos = slices.Clone(d.repos)
	for i := range c.repos {
		c.repos[i].Issues = cloneIssues(c.repos[i].Issues)
	}
	c.nibfin = slices.Clone(d.nibfin)
	return c.result
}

func (d personRead) result(host *op.HostReport, latest *LatestImage) op.Result {
	res := op.Result{"githubHost": d.ghHost}
	if host != nil {
		res["host"] = *host
	}
	if latest != nil {
		res["latestImage"] = *latest
	}
	if d.userRead {
		if d.userErr != nil {
			res["userError"] = d.userErr
		} else {
			res["user"] = d.user
		}
	}
	if d.listed {
		repos := slices.Clone(d.repos)
		for i := range repos {
			repos[i].Image = withLatest(repos[i].Image, latest)
		}
		res["repos"] = repos
		res["truncated"] = d.truncated
		if d.issuesErr != nil {
			res["issuesError"] = d.issuesErr
		}
	}
	if d.reposRead {
		res["nibfinRepos"] = withExecution(d.nibfin, host, d.ghHost)
		if d.nibfinErr != nil {
			res["nibfinReposError"] = d.nibfinErr
		}
	}
	return res
}

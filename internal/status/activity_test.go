package status

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
)

var actNow = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

const createdQ = ">=2026-11-25T12:00:00+00:00"

func sharedRunsPath(kv ...string) string {
	return q("repos/o/r/actions/workflows/nibfin-shared-runner.yml/runs", append(kv, "per_page", "100", "page", "1")...)
}

func jobsPath(id int) string {
	return fmt.Sprintf("repos/o/r/actions/runs/%d/jobs?filter=latest&per_page=100&page=1", id)
}

func runItem(id int, title, status string, attempt int, created string) map[string]any {
	return map[string]any{"id": id, "display_title": title, "status": status, "run_attempt": attempt,
		"path": ".github/workflows/nibfin-shared-runner.yml", "created_at": created}
}

func job(runner, status, conclusion, started, completed string) map[string]any {
	j := map[string]any{"runner_name": runner, "status": status, "html_url": "https://github.com/o/r/actions/runs/1/job/2"}
	if conclusion != "" {
		j["conclusion"] = conclusion
	}
	if started != "" {
		j["started_at"] = started
	}
	if completed != "" {
		j["completed_at"] = completed
	}
	return j
}

func slots(hs op.HostSlots) func(context.Context) op.HostSlots {
	return func(context.Context) op.HostSlots { return hs }
}

func useSlots(t *testing.T, hs op.HostSlots) {
	t.Helper()
	old := readHostSlots
	t.Cleanup(func() { readHostSlots = old })
	readHostSlots = slots(hs)
}

var twoSlots = op.HostSlots{
	Repo: "github.com/o/r",
	Slots: []op.SlotDir{
		{Slot: "claude-1", Provider: "claude", Name: "H-Claude-1", Dir: "/home/u/nibfin/claude-1"},
		{Slot: "claude-2", Provider: "claude", Name: "h-claude-2", Dir: "/home/u/nibfin/claude-2"},
	},
}

func TestTitleJob(t *testing.T) {
	for in, want := range map[string]string{
		"Nibfin · Acme/App#12 · deliver": "Acme/App 12 deliver",
		"NIBFIN · o/r#3 · Review":        "o/r 3 review",
		"Nibfin · o/r#3 · wake":          "o/r 3 ",
		"Nibfin · o/r@main · route":      "!",
		"Nibfin · o/r#03 · shape":        "!",
		"Other · o/r#3 · shape":          "!",
	} {
		repo, n, stage, ok := TitleJob(in)
		got := fmt.Sprintf("%s %d %s", repo, n, stage)
		if !ok {
			got = "!"
		}
		if got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}

func TestReadActivity(t *testing.T) {
	routes := map[string]any{
		sharedRunsPath("created", createdQ): map[string]any{"workflow_runs": []any{
			runItem(11, "Nibfin · acme/app#7 · deliver", "in_progress", 1, "2026-11-26T11:00:00Z"),
			runItem(12, "Nibfin · acme/app#5 · shape", "completed", 2, "2026-11-26T08:00:00Z"),
			// Another workflow's run is skipped.
			map[string]any{"id": 13, "display_title": "CI", "status": "completed", "run_attempt": 1, "path": ".github/workflows/ci.yml", "created_at": "2026-11-26T08:00:00Z"},
		}},
		sharedRunsPath("status", "in_progress"): map[string]any{"workflow_runs": []any{
			runItem(11, "Nibfin · acme/app#7 · deliver", "in_progress", 1, "2026-11-26T11:00:00Z"),
			// Created before the window, still running.
			runItem(9, "Nibfin · bad repo#9 · review", "in_progress", 1, "2026-11-25T10:00:00Z"),
		}},
		jobsPath(11): map[string]any{"jobs": []any{
			job("ignored-host", "completed", "success", "2026-11-26T11:00:00Z", "2026-11-26T11:01:00Z"),
			job("h-claude-1", "in_progress", "", "2026-11-26T11:02:00Z", ""),
		}},
		jobsPath(12): map[string]any{"jobs": []any{
			job("h-claude-2", "completed", "failure", "2026-11-26T08:01:00Z", "2026-11-26T08:30:00Z"),
			job("h-claude-2", "completed", "timed_out", "2026-11-26T09:00:00Z", "2026-11-26T09:10:00Z"),
			job("h-claude-2", "completed", "success", "2026-11-26T07:00:00Z", "2026-11-26T07:30:00Z"),
			// Queued: no runner yet.
			map[string]any{"runner_name": nil, "status": "queued"},
		}},
		jobsPath(9): map[string]any{"jobs": []any{
			job("h-claude-2", "completed", "cancelled", "2026-11-25T10:00:00Z", "2026-11-25T13:00:00Z"),
		}},
	}
	fx := setup(t, routes)
	useSlots(t, twoSlots)
	a := NewActivityReader()
	act, err := a.ReadActivity(context.Background(), actNow)
	if err != nil || act.JobsError != nil || act.SlotsError != nil {
		t.Fatalf("act = %+v, %v", act, err)
	}
	if act.From != "2026-11-25T12:00:00Z" || act.To != "2026-11-26T12:00:00Z" || act.Repo != "github.com/o/r" || len(act.Runners) != 2 {
		t.Fatalf("act = %+v", act)
	}
	c1 := act.Runners[0]
	want := CurrentJob{Repo: "acme/app", Issue: 7, IssueURL: "https://github.com/acme/app/issues/7", Stage: "deliver",
		URL: "https://github.com/o/r/actions/runs/1/job/2", StartedAt: "2026-11-26T11:02:00Z", Source: "github"}
	// The name matched without case.
	if c1.Current == nil || *c1.Current != want {
		t.Errorf("claude-1 current = %+v", c1.Current)
	}
	if len(c1.Periods) != 1 || c1.Periods[0].End != "" || c1.Periods[0].Start != "2026-11-26T11:02:00Z" {
		t.Errorf("claude-1 periods = %+v", c1.Periods)
	}
	c2 := act.Runners[1]
	if c2.Current != nil {
		t.Errorf("claude-2 current = %+v", c2.Current)
	}
	got := []string{}
	for _, p := range c2.Periods {
		got = append(got, fmt.Sprintf("%s-%s %v %s#%d %s", p.Start, p.End, p.Failed, p.Repo, p.Issue, p.Stage))
	}
	wantP := []string{
		// Clipped to the window; an invalid repository gives no Issue.
		"2026-11-25T12:00:00Z-2026-11-25T13:00:00Z false #0 ",
		"2026-11-26T07:00:00Z-2026-11-26T07:30:00Z false acme/app#5 shape",
		"2026-11-26T08:01:00Z-2026-11-26T08:30:00Z true acme/app#5 shape",
		"2026-11-26T09:00:00Z-2026-11-26T09:10:00Z true acme/app#5 shape",
	}
	if strings.Join(got, "\n") != strings.Join(wantP, "\n") {
		t.Errorf("claude-2 periods =\n%s", strings.Join(got, "\n"))
	}

	// A second read takes the completed runs' jobs from memory and reads
	// the running run's jobs again.
	fx.gh.gets = nil
	if _, err := a.ReadActivity(context.Background(), actNow); err != nil {
		t.Fatal(err)
	}
	for _, g := range fx.gh.gets {
		if g == jobsPath(12) {
			t.Errorf("completed run 12 read again")
		}
	}
	if countPrefix(fx.gh.gets, jobsPath(11)) != 1 || countPrefix(fx.gh.gets, jobsPath(9)) != 1 {
		t.Errorf("gets = %q", fx.gh.gets)
	}

	// A new attempt of run 12 is a new key.
	routes[sharedRunsPath("created", createdQ)].(map[string]any)["workflow_runs"].([]any)[1].(map[string]any)["run_attempt"] = 3
	fx.gh.gets = nil
	if _, err := a.ReadActivity(context.Background(), actNow); err != nil {
		t.Fatal(err)
	}
	if countPrefix(fx.gh.gets, jobsPath(12)) != 1 {
		t.Errorf("attempt 3 not read: %q", fx.gh.gets)
	}
	if _, ok := a.cache[runKey{12, 2}]; ok {
		t.Errorf("attempt 2 kept in memory")
	}
}

func TestReadActivityJobsFail(t *testing.T) {
	setup(t, map[string]any{
		sharedRunsPath("created", createdQ): &api.HTTPError{StatusCode: http.StatusForbidden, Message: "Resource not accessible"},
	})
	hs := twoSlots
	hs.Jobs = []op.JobContainer{
		// Mounts the slot's work directory.
		{Repo: "acme/app", Issue: "7", StartedAt: "2026-11-26T11:57:00.123456789+08:00", Mounts: []string{"/home/u/nibfin/claude-2/_work/app/app/nibfin-workspace"}},
		// A route container mounts nothing.
		{Repo: "acme/app", Issue: "8", StartedAt: "2026-11-26T11:59:00Z"},
		// A sibling directory with the same prefix is not the slot's.
		{Repo: "acme/<x>", Issue: "07", Mounts: []string{"/home/u/nibfin/claude-10/_work"}},
	}
	useSlots(t, hs)
	act, _ := NewActivityReader().ReadActivity(context.Background(), actNow)
	if act.JobsError == nil || act.JobsError.Code != op.CodeGitHubAPIFailed || !strings.Contains(act.JobsError.Error(), "HTTP 403") {
		t.Fatalf("jobsError = %+v", act.JobsError)
	}
	if act.Runners[0].Current != nil || len(act.Runners[0].Periods) != 0 {
		t.Errorf("claude-1 = %+v", act.Runners[0])
	}
	want := CurrentJob{Repo: "acme/app", Issue: 7, IssueURL: "https://github.com/acme/app/issues/7", StartedAt: "2026-11-26T03:57:00Z", Source: "container"}
	if c := act.Runners[1].Current; c == nil || *c != want {
		t.Errorf("claude-2 current = %+v", c)
	}
	wantU := []ContainerJob{
		{Repo: "acme/app", Issue: 8, IssueURL: "https://github.com/acme/app/issues/8", StartedAt: "2026-11-26T11:59:00Z"},
		{},
	}
	if fmt.Sprint(act.Unattributed) != fmt.Sprint(wantU) {
		t.Errorf("unattributed = %+v", act.Unattributed)
	}
}

func TestReadActivityNoRepo(t *testing.T) {
	fx := setup(t, map[string]any{})
	for _, c := range []struct {
		hs   op.HostSlots
		code string
	}{
		{op.HostSlots{Slots: []op.SlotDir{}}, op.CodeNoRunners},
		{op.HostSlots{Repo: "ghe.example/o/r", Slots: []op.SlotDir{{Slot: "claude-1", Name: "n"}}}, op.CodeAuthUnavailable},
		{op.HostSlots{Error: op.AsError(op.Errorf(op.CodeMachineStopped, "stopped"))}, op.CodeMachineStopped},
	} {
		useSlots(t, c.hs)
		act, _ := NewActivityReader().ReadActivity(context.Background(), actNow)
		if act.JobsError == nil || act.JobsError.Code != c.code {
			t.Errorf("%+v: jobsError = %+v", c.hs, act.JobsError)
		}
	}
	if len(fx.gh.gets) != 0 {
		t.Errorf("gets = %q", fx.gh.gets)
	}
}

func TestHTTPSURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/o/r/actions/runs/1/job/2": "https://github.com/o/r/actions/runs/1/job/2",
		"http://github.com/x":                         "",
		"javascript:alert(1)":                         "",
		"":                                            "",
	} {
		if got := httpsURL(in); got != want {
			t.Errorf("%q = %q", in, got)
		}
	}
}

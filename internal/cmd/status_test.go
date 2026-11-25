package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/runners"
	"github.com/taco3064/gh-nibfin/internal/status"
)

const (
	digestA = "ghcr.io/taco3064/nibfin@sha256:aaaaaaaaaaaa0000000000000000000000000000000000000000000000000000"
	digestB = "ghcr.io/taco3064/nibfin@sha256:bbbbbbbbbbbb0000000000000000000000000000000000000000000000000000"
)

func hostReport() op.HostReport {
	return op.HostReport{
		Host:   op.HostInfo{Describe: "podman machine m", Machine: "m", VMType: "wsl", Running: true},
		Memory: op.MemoryReport{TotalKB: 16 << 20, AvailableKB: 9 << 20},
		Runners: op.RunnersReport{Repo: "github.com/o/r", Runners: []op.RunnerState{
			{Runner: runners.Runner{Slot: "claude-1", Active: true}, Listed: true, Status: "online", Busy: true},
			{Runner: runners.Runner{Slot: "codex-1"}},
		}},
		Providers: []op.ProviderReport{
			{Provider: "claude", LoggedIn: true},
			{Provider: "codex", Error: &op.Error{Code: op.CodePodmanFailed, Err: errStop}},
		},
	}
}

func runStatus(t *testing.T, args []string, r op.Result) (string, []string) {
	t.Helper()
	seen := fakeRepo(t)
	old := ops
	t.Cleanup(func() { ops = old })
	var repos []string
	ops.Status = func(p op.RepoParams) op.Operation {
		repos = append(repos, p.Repo)
		return statusOp{r}
	}
	code, stdout, stderr := execute(t, args...)
	if code != 0 || stderr != "" {
		t.Fatalf("%v: exit %d, stderr %q", args, code, stderr)
	}
	return stdout, append(repos, *seen...)
}

// statusOp is a read-only operation reading back a fixed result.
type statusOp struct{ r op.Result }

func (statusOp) Plan(context.Context) (op.Plan, error) {
	return op.Plan{Operation: "status", ReadOnly: true}, nil
}
func (statusOp) Apply(context.Context, op.Plan) error                   { return nil }
func (o statusOp) ReadBack(context.Context, op.Plan) (op.Result, error) { return o.r, nil }

func TestStatusPersonText(t *testing.T) {
	yes := true
	ref := digestA
	r := op.Result{
		"host":        hostReport(),
		"latestImage": status.LatestImage{Ref: digestA},
		"githubHost":  "github.com",
		"user":        "me",
		"truncated":   false,
		"repos": []status.RepoIssues{
			{Repo: "o/r", Image: status.RepoImage{Set: "execution", Ref: &ref, Latest: &yes}, Issues: []status.Issue{
				{Number: 1, Title: "One\x1b[31m", Pause: true},
				{Number: 2, Title: "Two", Pending: true, PendingComment: "https://github.com/o/r/issues/2#issuecomment-9"},
				{Number: 3, Title: "Three", Pending: true},
				{Number: 4, Title: "Four", Pending: true, PendingCommentError: &op.Error{Code: op.CodeGitHubAPIFailed, Err: errStop}},
			}},
			{Repo: "o/peer", Image: status.RepoImage{Set: "peer"}, Issues: []status.Issue{}},
		},
	}
	out, repos := runStatus(t, []string{"status"}, r)
	for _, want := range []string{
		"host: podman machine m (wsl), running\n",
		"memory: 16.0 GB total, 9.0 GB available\n",
		"runners (github.com/o/r):\n  claude-1  active  github: online  busy\n  codex-1  inactive  github: not listed\n",
		"login claude: logged in\n",
		"login codex: podman_failed: stop\n",
		"latest image: sha256:aaaaaaaaaaaa\n",
		"scope Issues assigned to me on github.com:\n",
		"o/r  image: sha256:aaaaaaaaaaaa (latest)\n",
		"  #1 One[31m  [pause]\n",
		"  #2 Two  [pending https://github.com/o/r/issues/2#issuecomment-9]\n",
		"  #3 Three  [pending (no marked comment)]\n",
		"  #4 Four  [pending]\n    pending comment: github_api_failed: stop\n",
		"o/peer  image: peer\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// The person view resolves no repository.
	if len(repos) != 1 || repos[0] != "" {
		t.Errorf("repos = %q", repos)
	}
}

func TestStatusRepoText(t *testing.T) {
	no := false
	ref := digestB
	h := hostReport()
	h.Host = op.HostInfo{Describe: "podman machine m", Machine: "m"}
	stopped := &op.Error{Code: op.CodeMachineStopped, Err: errStop}
	h.Memory = op.MemoryReport{Error: stopped}
	h.Runners = op.RunnersReport{Runners: []op.RunnerState{}, Error: stopped}
	r := op.Result{
		"host":        h,
		"latestImage": status.LatestImage{Ref: digestA},
		"repo":        "github.com/o/r",
		"image":       status.RepoImage{Set: "execution", Ref: &ref, Latest: &no},
		"issues":      []status.Issue{{Number: 7, Title: "Seven"}},
		"nibfinRun": status.NibfinRun{Initialized: true, InitCommit: "abc", InitPR: &status.PullRequest{Number: 5, URL: "https://github.com/o/r/pull/5"},
			MergedAt: "2026-11-01T00:00:00Z", Succeeded: true, Run: "https://github.com/o/r/actions/runs/1", At: "2026-11-02T00:00:00Z"},
	}
	out, repos := runStatus(t, []string{"status", "-R", "o/r"}, r)
	for _, want := range []string{
		"host: podman machine m, stopped\n",
		"memory: machine_stopped: stop\n",
		"runners: machine_stopped: stop\n",
		"github.com/o/r  image: sha256:bbbbbbbbbbbb (not latest)\n",
		"  #7 Seven\n",
		"nibfin run: succeeded https://github.com/o/r/actions/runs/1 at 2026-11-02T00:00:00Z, after init PR #5 https://github.com/o/r/pull/5 merged at 2026-11-01T00:00:00Z\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if len(repos) != 2 || repos[0] != "github.com/o/r" || repos[1] != "o/r" {
		t.Errorf("repos = %q", repos)
	}

	out, _ = runStatus(t, []string{"status", "-R", "o/r", "--json"}, r)
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got["repo"] != "github.com/o/r" || got["nibfinRun"].(map[string]any)["succeeded"] != true {
		t.Errorf("json = %v", got)
	}
}

func TestStatusTextNotInitialized(t *testing.T) {
	var b bytes.Buffer
	writeRun(&b, status.NibfinRun{})
	if !strings.Contains(b.String(), "no init commit") {
		t.Errorf("run = %q", b.String())
	}
	b.Reset()
	writeRun(&b, status.NibfinRun{Initialized: true, InitCommit: "abc", MergedAt: "t"})
	if b.String() != "nibfin run: none succeeded since init commit abc merged at t\n" {
		t.Errorf("run = %q", b.String())
	}
}

func TestWriteAutostart(t *testing.T) {
	for _, tc := range []struct {
		a    op.AutostartReport
		want string
	}{
		{op.AutostartReport{Kind: "task", Name: "Nibfin podman machine", Present: true, Current: true}, "autostart: task \"Nibfin podman machine\" present\n"},
		{op.AutostartReport{Kind: "task", Name: "Nibfin podman machine", Present: true}, "autostart: task \"Nibfin podman machine\" present (outdated: run gh nibfin runner add)\n"},
		{op.AutostartReport{Kind: "launchagent", Name: "io.github.taco3064.nibfin.podman-machine", Present: true, Current: true}, "autostart: LaunchAgent \"io.github.taco3064.nibfin.podman-machine\" present\n"},
		{op.AutostartReport{Kind: "task", Name: "Nibfin podman machine"}, "autostart: absent\n"},
		{op.AutostartReport{}, "autostart: not needed (systemd user services with linger)\n"},
		{op.AutostartReport{Kind: "task", Error: &op.Error{Code: op.CodeAutostartFailed, Err: errStop}}, "autostart: autostart_failed: stop\n"},
	} {
		var b strings.Builder
		writeAutostart(&b, tc.a)
		if b.String() != tc.want {
			t.Errorf("%+v: %q, want %q", tc.a, b.String(), tc.want)
		}
	}
	h := hostReport()
	h.Autostart = op.AutostartReport{Kind: "task", Name: "Nibfin podman machine", Present: true, Current: true}
	out, _ := runStatus(t, []string{"status"}, op.Result{"host": h, "latestImage": status.LatestImage{}, "githubHost": "github.com", "user": "me", "repos": []status.RepoIssues{}})
	if !strings.Contains(out, "host: podman machine m (wsl), running\nautostart: task \"Nibfin podman machine\" present\nmemory:") {
		t.Errorf("status:\n%s", out)
	}
}

func TestStatusBlockedByText(t *testing.T) {
	perm := "ADMIN"
	no := "failure"
	r := op.Result{
		"host":        hostReport(),
		"latestImage": status.LatestImage{Ref: digestA},
		"repo":        "github.com/o/r",
		"image":       status.RepoImage{Set: "peer"},
		"permission":  &perm,
		"issues": []status.Issue{
			{Number: 1, Title: "One", BlockedBy: []status.Blocker{
				{Repo: "o/r", Number: 2, Title: "Two\x1b[31m", State: "OPEN"},
				{Repo: "x/y", Number: 3, Title: "Closed one", State: "CLOSED"},
				{Repo: "x/y", Number: 4, Title: "Four", State: "OPEN"},
			}, Runs: []status.Run{{Repo: "o/r", Name: "Nibfin · o/r#1 · wake", Conclusion: &no, URL: "https://github.com/o/r/actions/runs/77"}}},
			{Number: 5, Title: "Five", BlockedBy: []status.Blocker{{Repo: "o/r", Number: 6, State: "CLOSED"}}},
			{Number: 7, Title: "Seven", BlockedByError: &op.Error{Code: op.CodeGitHubAPIFailed, Err: errStop}},
		},
		"nibfinRun":   status.NibfinRun{},
		"nibfinRepos": []status.NibfinRepo{{Repo: "o/listed", URL: "https://github.com/o/listed"}},
	}
	out, _ := runStatus(t, []string{"status", "-R", "o/r"}, r)
	for _, want := range []string{
		"  #1 One\n    blocked by: o/r#2 Two[31m, x/y#4 Four\n  #5 Five\n  #7 Seven\n    blocked by: github_api_failed: stop\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"Closed one", "actions/runs/77", "ADMIN", "o/listed"} {
		if strings.Contains(out, not) {
			t.Errorf("output has %q:\n%s", not, out)
		}
	}
}

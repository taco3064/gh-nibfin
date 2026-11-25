package op

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/host"
)

func TestReadHost(t *testing.T) {
	f := &fakeHost{vm: true, wsl: true,
		probe:    defaultProbe + "memavailkb 9000000\n",
		volumes:  map[string]bool{"claude": true},
		loggedIn: map[string]bool{"claude": true}}
	fx := useRunnerHost(t, f)
	fx.gh.busy = map[string]bool{"h-codex-1": true}
	f.slots["claude-2"] = &fakeSlot{unit: true}

	rep := ReadHost(context.Background())
	if rep.Host.Describe != "fake" || !rep.Host.Running || rep.Host.Error != nil {
		t.Errorf("host = %+v", rep.Host)
	}
	if rep.Memory != (MemoryReport{TotalKB: 16000000, AvailableKB: 9000000}) {
		t.Errorf("memory = %+v", rep.Memory)
	}
	got := map[string]RunnerState{}
	for _, r := range rep.Runners.Runners {
		got[r.Slot] = r
	}
	if rep.Runners.Repo != "github.com/o/r" || rep.Runners.Error != nil || rep.Runners.GitHubError != nil || len(got) != 3 {
		t.Fatalf("runners = %+v", rep.Runners)
	}
	if c := got["claude-1"]; !c.Listed || c.Status != "online" || c.Busy || !c.Active {
		t.Errorf("claude-1 = %+v", c)
	}
	if c := got["codex-1"]; !c.Listed || !c.Busy {
		t.Errorf("codex-1 = %+v", c)
	}
	if c := got["claude-2"]; c.Listed || c.Configured || c.Active || !c.Unit {
		t.Errorf("claude-2 = %+v", c)
	}
	want := []ProviderReport{{Provider: "claude", LoggedIn: true}, {Provider: "codex"}}
	if !reflect.DeepEqual(rep.Providers, want) {
		t.Errorf("providers = %+v", rep.Providers)
	}
	// No volume for codex: no container. The claude check never pulls and
	// has its own name.
	var runs [][]string
	for _, c := range f.calls {
		switch {
		case c[0] == "run":
			runs = append(runs, c)
		case c[0] == "volume" && c[1] == "exists":
		default:
			t.Errorf("unexpected podman call %q", c)
		}
	}
	claude, _ := lookupProvider("claude")
	if len(runs) != 1 || !reflect.DeepEqual(runs[0], statusCheckArgs(claude)) {
		t.Fatalf("runs = %q", runs)
	}
	if !hasArg(runs[0], "--pull=never") || flag(runs[0], "--name") != "nibfin-status-claude" || hasArg(runs[0], "--pull=newer") {
		t.Errorf("status check argv = %q", runs[0])
	}
	for _, s := range f.shells {
		if s.script != "list" && s.script != "probe" {
			t.Errorf("unexpected script %q", s.script)
		}
	}
	if len(f.events) != 0 || fx.gh.tokens != 0 {
		t.Errorf("status changed the host: %v", f.events)
	}
	b, err := json.Marshal(rep)
	if err != nil || !strings.Contains(string(b), `"availableKB":9000000`) || !strings.Contains(string(b), `"busy":true`) {
		t.Errorf("json = %s, %v", b, err)
	}
}

func TestReadHostStoppedMachine(t *testing.T) {
	f := &fakeHost{stopped: true, vm: true}
	useRunnerHost(t, f)
	rep := ReadHost(context.Background())
	if rep.Host.Running || rep.Host.Error != nil || f.starts != 0 {
		t.Errorf("host = %+v, starts %d", rep.Host, f.starts)
	}
	if len(f.calls) != 0 || len(f.shells) != 0 {
		t.Errorf("status touched a stopped machine: %v %v", f.calls, f.shells)
	}
	errs := []*Error{rep.Memory.Error, rep.Runners.Error}
	for _, p := range rep.Providers {
		errs = append(errs, p.Error)
	}
	if len(errs) != 4 {
		t.Fatalf("providers = %+v", rep.Providers)
	}
	for _, e := range errs {
		if Code(e) != CodeMachineStopped {
			t.Errorf("err = %v", e)
		}
	}
}

func TestReadHostDetectErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{host.ErrPodmanMissing, CodePodmanMissing},
		{host.ErrMachineMissing, CodeMachineMissing},
		{host.ErrUnsupported, CodeUnsupportedHost},
		{host.ErrPodman, CodePodmanFailed},
	} {
		old := detectHost
		detectHost = func(context.Context) (runnerHost, error) { return nil, hostError(tc.err) }
		rep := ReadHost(context.Background())
		detectHost = old
		if Code(rep.Host.Error) != tc.code || Code(rep.Memory.Error) != tc.code || Code(rep.Runners.Error) != tc.code || len(rep.Providers) != 2 || Code(rep.Providers[1].Error) != tc.code {
			t.Errorf("%v: report = %+v", tc.err, rep)
		}
	}
}

func TestReadHostPartErrors(t *testing.T) {
	// GitHub cannot list the runners: the local fields stay.
	f := &fakeHost{volumes: map[string]bool{"claude": true, "codex": true}, checkExit: 125}
	fx := useRunnerHost(t, f)
	fx.gh.listErr = Errorf(CodeGitHubAPIFailed, "list the runners: HTTP 403: Must have admin rights")
	rep := ReadHost(context.Background())
	if Code(rep.Runners.GitHubError) != CodeGitHubAPIFailed || rep.Runners.Error != nil || len(rep.Runners.Runners) != 2 {
		t.Errorf("runners = %+v", rep.Runners)
	}
	for _, r := range rep.Runners.Runners {
		if r.Listed || !r.Active || r.Name == "" {
			t.Errorf("runner = %+v", r)
		}
	}
	// An absent image makes podman exit 125.
	for _, p := range rep.Providers {
		if Code(p.Error) != CodePodmanFailed || p.LoggedIn {
			t.Errorf("provider = %+v", p)
		}
	}
	if rep.Memory.Error != nil || rep.Memory.AvailableKB != 0 {
		t.Errorf("memory = %+v", rep.Memory)
	}

	// Runners serving two repositories: a conflict, local list kept.
	f = &fakeHost{slots: map[string]*fakeSlot{
		"claude-1": {configured: true, unit: true, enabled: true, active: true, name: "a", url: "https://github.com/o/r"},
		"codex-1":  {configured: true, unit: true, enabled: true, active: true, name: "b", url: "https://github.com/o/other"},
	}}
	useRunnerHost(t, f)
	f.shellExit = map[string]int{"probe": 1}
	rep = ReadHost(context.Background())
	if Code(rep.Runners.Error) != CodeRunnerRepoConflict || len(rep.Runners.Runners) != 2 || rep.Runners.Repo != "" {
		t.Errorf("runners = %+v", rep.Runners)
	}
	if rep.Memory.Error == nil {
		t.Errorf("memory = %+v", rep.Memory)
	}
}

func TestProbeMemAvailable(t *testing.T) {
	f := &fakeHost{probe: defaultProbe + "memavailkb 1234\n"}
	useRunnerHost(t, f)
	pr, err := probeHost(context.Background(), f)
	if err != nil || pr.memKB != 16000000 || pr.memAvailKB != 1234 {
		t.Errorf("probe = %+v, %v", pr, err)
	}
	if !strings.Contains(probeScript, "/^MemAvailable:/") {
		t.Error("probeScript does not read MemAvailable")
	}
}

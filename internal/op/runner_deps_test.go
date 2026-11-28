package op

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestRunnerAddMachineDeps covers the official runner's system dependencies
// in a podman machine VM: a VM missing libicu or libssl.so.3 gets one item
// that runs the archive's bin/installdependencies.sh before any configure,
// sharing the one download with the slots; a VM with both gets no change; a
// failed install, a failed download or an archive without the installer
// stops runner add with its cause before any slot; and a local Linux host
// never gets the item.
func TestRunnerAddMachineDeps(t *testing.T) {
	lingered := strings.Replace(defaultProbe, "linger 0", "linger 1", 1)
	noICU := strings.Replace(lingered, "icu 1", "icu 0", 1)
	newHost := func(probe string) *fakeHost {
		return &fakeHost{vm: true, probe: probe, slots: map[string]*fakeSlot{},
			volumes: map[string]bool{"nibfin-tools": true, "claude": true}, loggedIn: map[string]bool{"claude": true}}
	}
	slotEvents := []string{"extract claude-1", "configure claude-1", "env claude-1 claude", "unitdir " + unitDir, "unit nibfin-runner-claude-1.service",
		"extract claude-2", "configure claude-2", "env claude-2 claude", "unitdir " + unitDir, "unit nibfin-runner-claude-2.service"}
	depsItem := func(p Plan) (Step, bool) {
		for _, s := range p.Steps {
			if s.Action == actInstallDeps {
				return s, true
			}
		}
		return Step{}, false
	}
	noArchiveLeft := func(t *testing.T) {
		t.Helper()
		if left, _ := filepath.Glob(filepath.Join(os.TempDir(), "nibfin-runner-*")); len(left) != 0 {
			t.Errorf("archive left behind: %v", left)
		}
	}

	t.Run("missing", func(t *testing.T) {
		for _, c := range []struct{ probe, current string }{
			{noICU, "missing: libicu"},
			{strings.Replace(lingered, "ssl 1", "ssl 0", 1), "missing: libssl.so.3"},
			{strings.Replace(noICU, "ssl 1", "ssl 0", 1), "missing: libicu, libssl.so.3"},
		} {
			f := newHost(c.probe)
			fx := useRunnerHost(t, f)
			p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}).Plan(context.Background())
			want := []string{"install runner dependencies fake", "add runner claude-1", "add runner claude-2"}
			if err != nil || !reflect.DeepEqual(steps(p), want) {
				t.Fatalf("plan = %v, %v", steps(p), err)
			}
			if s, _ := depsItem(p); s.ID != "runner-deps" || s.Current != c.current || s.Next != "present" || s.NoChange {
				t.Errorf("deps step = %+v", s)
			}
			_, r, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}), &yes{})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			// The installer runs first; the slots reuse its download.
			if want := append([]string{"deps", "token"}, slotEvents...); !reflect.DeepEqual(f.events, want) {
				t.Errorf("events = %v", f.events)
			}
			if len(*fx.auth) != 1 {
				t.Errorf("downloads = %d", len(*fx.auth))
			}
			b, _ := json.Marshal(r)
			if !strings.Contains(string(b), "install runner dependencies") {
				t.Errorf("read-back has no deps item: %s", b)
			}
			for _, s := range f.shells {
				if s.script == "deps" && len(s.args) != 0 {
					t.Errorf("deps args = %q", s.args)
				}
			}
			noArchiveLeft(t)
		}
	})

	t.Run("present", func(t *testing.T) {
		f := newHost(lingered)
		fx := useRunnerHost(t, f)
		p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
		if err != nil || !reflect.DeepEqual(steps(p), []string{"add runner claude-1"}) {
			t.Fatalf("plan = %v, %v", steps(p), err)
		}
		if s, ok := depsItem(p); !ok || !s.NoChange || s.Current != "present" {
			t.Errorf("no no-change deps item: %v", allSteps(p))
		}
		if _, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, c := range f.shells {
			if c.script == "deps" || c.script == "depscheck" {
				t.Errorf("%s script ran on a VM with the dependencies", c.script)
			}
		}
		if len(*fx.auth) != 1 {
			t.Errorf("downloads = %d", len(*fx.auth))
		}
	})

	t.Run("failed", func(t *testing.T) {
		for _, c := range []struct{ stderr, remedy string }{
			// The installer itself fails (a refused sudo -n, a dnf error).
			{"sudo: a password is required\nbin/installdependencies.sh failed inside the VM", "sudo ./bin/installdependencies.sh"},
			// The installer succeeds, but the libssl fallback does not help.
			{"libssl.so.3 is still not found after dnf install openssl3-libs", "sudo dnf install -y openssl3-libs"},
		} {
			f := newHost(noICU)
			f.shellExit = map[string]int{"deps": 1}
			f.shellErr = map[string]string{"deps": c.stderr}
			f.shellOut = map[string]string{"deps": "Last metadata expiration check\n"}
			useRunnerHost(t, f)
			_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
			msg := fmt.Sprint(err)
			if Code(err) != CodeRunnerInstallFailed || !strings.Contains(msg, "install runner dependencies on fake") ||
				!strings.Contains(msg, "exit status 1") || !strings.Contains(msg, "stderr: "+strings.ReplaceAll(c.stderr, "\n", " | ")) ||
				!strings.Contains(msg, "stdout: Last metadata expiration check") ||
				!strings.Contains(msg, "podman machine ssh fake") || !strings.Contains(msg, c.remedy) {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(f.events, []string{"deps"}) {
				t.Errorf("events after a failed deps step = %v", f.events)
			}
			noArchiveLeft(t)
		}
		// A failed podman machine ssh keeps its own code.
		f := newHost(noICU)
		f.shellExit = map[string]int{"deps": 125}
		useRunnerHost(t, f)
		if _, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{}); Code(err) != CodePodmanFailed {
			t.Errorf("ssh failure: %v", err)
		}
	})

	t.Run("download failed", func(t *testing.T) {
		f := newHost(noICU)
		fx := useRunnerHost(t, f)
		fx.gh.downloads[1].SHA256 = shaHex("another archive")
		_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		if Code(err) != CodeRunnerDownloadFailed || len(f.events) != 0 {
			t.Errorf("err = %v, events %v", err, f.events)
		}
		noArchiveLeft(t)
	})

	t.Run("no installer in the archive", func(t *testing.T) {
		f := newHost(noICU)
		fx := useRunnerHost(t, f)
		other := string(tarGz(t, map[string]string{"bin/runsvc.sh": "#!/bin/sh\n", "bin/installdependencies.sh/x": "not a file"}))
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, other) }))
		t.Cleanup(srv.Close)
		httpClient = srv.Client()
		fx.gh.downloads[1].DownloadURL, fx.gh.downloads[1].SHA256 = srv.URL+"/runner.tar.gz", shaHex(other)
		_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		if Code(err) != CodeRunnerInstallFailed || !strings.Contains(fmt.Sprint(err), "the runner archive runner.tar.gz: has no bin/installdependencies.sh") || len(f.events) != 0 {
			t.Errorf("err = %v, events %v", err, f.events)
		}
		noArchiveLeft(t)
	})

	t.Run("local", func(t *testing.T) {
		f := &fakeHost{probe: strings.Replace(strings.Replace(lingered, "icu 1", "icu 0", 1), "ssl 1", "ssl 0", 1), slots: map[string]*fakeSlot{},
			volumes: map[string]bool{"nibfin-tools": true, "claude": true}, loggedIn: map[string]bool{"claude": true}}
		f.shellExit = map[string]int{"depscheck": 1}
		f.shellErr = map[string]string{"depscheck": "missing: libicu, libssl.so.3"}
		useRunnerHost(t, f)
		p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := depsItem(p); ok {
			t.Errorf("deps item on a local host: %v", allSteps(p))
		}
		// Apply refuses the item on a local host.
		bad := Plan{Repo: "github.com/cur/repo", Steps: []Step{depsStep("local podman")}}
		if err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Apply(context.Background(), bad); Code(err) != CodeInvalidArgument || !strings.Contains(fmt.Sprint(err), `unknown runner add step "install runner dependencies"`) {
			t.Errorf("local deps step: %v", err)
		}
		// A missing library stops the slot after the extract, before
		// config.sh, naming the library and the installer to run.
		_, _, err = Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		msg := fmt.Sprint(err)
		if Code(err) != CodeRunnerInstallFailed || !strings.Contains(msg, "missing: libicu, libssl.so.3") ||
			!strings.Contains(msg, "cd "+slotDir+"/claude-1 && sudo ./bin/installdependencies.sh") {
			t.Fatalf("err = %v", err)
		}
		if !reflect.DeepEqual(f.events, []string{"token", "extract claude-1"}) || f.slots["claude-1"] == nil || f.slots["claude-1"].configured {
			t.Errorf("events = %v, slot %+v", f.events, f.slots["claude-1"])
		}
		for _, c := range f.shells {
			if c.script == "deps" {
				t.Error("the installer ran on a local host")
			}
		}
		// Once the libraries are there, the next run reuses the slot.
		f.shellExit, f.shellErr, f.events = nil, nil, nil
		if _, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{}); err != nil {
			t.Fatalf("rerun: %v", err)
		}
		if want := []string{"token", "extract claude-1", "configure claude-1", "env claude-1 claude", "unit nibfin-runner-claude-1.service"}; !reflect.DeepEqual(f.events, want) {
			t.Errorf("rerun events = %v", f.events)
		}
		var order []string
		for _, c := range f.shells {
			if c.script == "extract" || c.script == "depscheck" || c.script == "configure" {
				order = append(order, c.script)
			}
		}
		if want := []string{"extract", "depscheck", "configure"}; !reflect.DeepEqual(order[len(order)-3:], want) {
			t.Errorf("order = %v", order)
		}
	})
}

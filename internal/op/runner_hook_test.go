package op

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/host"
	"github.com/taco3064/gh-nibfin/internal/runners"
)

const (
	hookAt        = "/home/u/.local/share/nibfin/hook"
	hookScriptAt  = hookAt + "/job-started.sh"
	githubHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
)

// fakeHookFiles are the constant hook files the tests install.
func fakeHookFiles() map[string][]byte {
	return map[string][]byte{
		"job-started.sh":                    []byte("# hook\n"),
		"check.mjs":                         []byte("// check\n"),
		"official/nibfin-shared-runner.yml": []byte("shared\n"),
		"official/nibfin-job.yml":           []byte("job\n"),
		"trusted_root.json":                 []byte("{\"root\":1}\n"),
	}
}

// useHookFiles sets the hook's constant files to fakeHookFiles, or to the
// error err.
func useHookFiles(t *testing.T, f *fakeHost, err error) {
	t.Helper()
	old := runnerHookFiles
	t.Cleanup(func() { runnerHookFiles = old })
	runnerHookFiles = func(_ context.Context, podman func(context.Context, ...string) *exec.Cmd) (map[string][]byte, error) {
		if podman == nil {
			return nil, errors.New("no podman to render the official files with")
		}
		if err != nil {
			return nil, err
		}
		return fakeHookFiles(), nil
	}
	f.hookTmp = t.TempDir()
}

// hookShell simulates the hook scripts on the fake host. A write's tar is
// saved and unpacked at the next read.
func (f *fakeHost) hookShell(name, slot string, args []string, exit int) (stdout, stdinSave string) {
	f.loadHookTar()
	switch name {
	case "hook-read":
		if f.hook == nil {
			return "", ""
		}
		b := func(s []byte) string { return base64.StdEncoding.EncodeToString(s) }
		mode := f.hookMode
		if mode == "" {
			mode = "700"
		}
		out := "mode " + mode + "\n"
		var names []string
		for n := range f.hook {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			out += "file " + b([]byte(n)) + " " + b(f.hook[n]) + "\n"
		}
		return out, ""
	case "hook-write":
		f.events = append(f.events, "hook "+args[0])
		if exit == 0 {
			f.hookTar = filepath.Join(f.hookTmp, fmt.Sprintf("hook-%d.tar", len(f.events)))
			return "", f.hookTar
		}
	case "hook-env":
		f.events = append(f.events, "hookenv "+slot+" "+args[1])
		if s := f.slots[slot]; exit == 0 && s != nil {
			s.hook = args[1] == hookScriptAt
		}
	case "hook-envcheck":
		if s := f.slots[slot]; s != nil && s.hook && args[1] == hookScriptAt {
			return "yes\n", ""
		}
		return "no\n", ""
	case "hook-present":
		if f.hook != nil {
			return "present\n", ""
		}
	case "hook-remove":
		f.events = append(f.events, "rm hook "+args[0])
		if exit == 0 {
			f.hook = nil
		}
	}
	return "", ""
}

// loadHookTar unpacks the last write's saved tar into hook.
func (f *fakeHost) loadHookTar() {
	if f.hookTar == "" {
		return
	}
	data, err := os.ReadFile(f.hookTar)
	if err != nil {
		return
	}
	f.hookTar = ""
	f.hook, f.hookMode = map[string][]byte{}, "700"
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		if h.Typeflag == tar.TypeReg {
			if h.Mode != 0o600 {
				f.hookMode = fmt.Sprintf("file %s mode %o", h.Name, h.Mode)
			}
			b, _ := io.ReadAll(tr)
			f.hook[h.Name] = b
		}
	}
}

func (g *fakeRunnerAPI) SSHHostKeys(context.Context) ([]string, error) {
	return []string{githubHostKey}, g.err
}

// wantHook are the files a public repository's hook holds.
func wantHook(private bool) map[string][]byte {
	files := fakeHookFiles()
	if private {
		files["config"] = []byte("host=github.com\nowner=cur\nname=repo\nuser-id=42\nprivate=1\ndeploy-key=" + deployKeyAt + "\n")
		files["known_hosts"] = []byte("github.com " + githubHostKey + "\n")
	} else {
		files["config"] = []byte("host=github.com\nowner=cur\nname=repo\nuser-id=42\nprivate=0\n")
	}
	return files
}

func findPlanStep(p Plan, id string) (Step, bool) {
	for _, s := range p.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

func itemOf(r Result, id string) (Check, bool) {
	items, _ := r[ResultItems].([]Check)
	for _, c := range items {
		if c.ID == id {
			return c, true
		}
	}
	return Check{}, false
}

func TestRunnerAddRequestInstallsHook(t *testing.T) {
	archiveURL(t)
	f := &fakeHost{}
	useRequest(t, f)
	p, r, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v (events %v)", err, f.events)
	}
	hs, ok := findPlanStep(p, "hook")
	if !ok || hs.NoChange || hs.Action != actInstallHook || hs.Target != hookScriptAt || hs.Current != "absent" ||
		hs.Detail != "only official Nibfin Jobs for alice; public, read without a credential; image signature checked against Sigstore Public Good" {
		t.Fatalf("hook step = %+v", hs)
	}
	var order []string
	for _, s := range p.Steps {
		order = append(order, s.ID)
	}
	if i, j, k := indexOf(order, "request"), indexOf(order, "hook"), indexOf(order, "runner:claude-1"); !(i < j && j < k) {
		t.Errorf("plan order = %v", order)
	}
	// The hook is written before any slot, and each slot's .env names it
	// before its service starts.
	want := []string{"linger", "hook " + hookAt, "request", "extract claude-1", "configure claude-1", "env claude-1 claude",
		"hookenv claude-1 " + hookScriptAt, "unit nibfin-runner-claude-1.service", "comment", "close completed"}
	if !reflect.DeepEqual(f.events, want) {
		t.Errorf("events = %v", f.events)
	}
	f.loadHookTar()
	if !reflect.DeepEqual(f.hook, wantHook(false)) || f.hookMode != "700" {
		t.Errorf("hook files = %q (mode %s)", f.hook, f.hookMode)
	}
	if d := p.Changes()[len(p.Changes())-2].Detail; !strings.HasSuffix(d, ", job-started hook") {
		t.Errorf("slot detail = %q", d)
	}
	c, ok := itemOf(r, "hook")
	if !ok || !c.Confirmed || r["hook"] != hookScriptAt {
		t.Errorf("hook item = %+v, result hook %v", c, r["hook"])
	}
	if c, _ := itemOf(r, "runner:claude-1"); !c.Confirmed || !strings.Contains(c.State, ".env names the job-started hook") {
		t.Errorf("slot item = %+v", c)
	}

	// A rerun with the files in place writes no hook and keeps the slot.
	f.events = nil
	p, _, err = Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil {
		t.Fatal(err)
	}
	if hs, _ := findPlanStep(p, "hook"); !hs.NoChange || hs.Current != "current" {
		t.Errorf("rerun hook step = %+v", hs)
	}
	for _, e := range f.events {
		if strings.HasPrefix(e, "hook ") {
			t.Errorf("rerun wrote the hook: %v", f.events)
		}
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func TestRunnerAddRequestHookPrivate(t *testing.T) {
	archiveURL(t)
	f := &fakeHost{}
	usePrivateRequest(t, f)
	p, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v (events %v)", err, f.events)
	}
	if hs, _ := findPlanStep(p, "hook"); !strings.Contains(hs.Detail, "private, read with the deploy key") {
		t.Errorf("hook step = %+v", hs)
	}
	f.loadHookTar()
	if !reflect.DeepEqual(f.hook, wantHook(true)) {
		t.Errorf("hook files = %q", f.hook)
	}
}

func TestRunnerAddRequestHookStops(t *testing.T) {
	t.Run("a private repository without git or ssh", func(t *testing.T) {
		for _, tools := range []string{"tools 0 1\n", "tools 1 0\n", ""} {
			f := &fakeHost{probe: strings.Replace(defaultProbe, "tools 1 1\n", tools, 1)}
			usePrivateRequest(t, f)
			_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
			if Code(err) != CodeUnsupportedHost || !strings.Contains(err.Error(), "install git and the OpenSSH client") || len(f.events) != 0 {
				t.Errorf("%q: err = %v, events %v", tools, err, f.events)
			}
		}
	})
	t.Run("a public repository needs neither", func(t *testing.T) {
		archiveURL(t)
		f := &fakeHost{probe: strings.Replace(defaultProbe, "tools 1 1\n", "tools 0 0\n", 1)}
		useRequest(t, f)
		if _, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{}); err != nil {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("another host", func(t *testing.T) {
		f := &fakeHost{}
		fx := useRequest(t, f)
		_ = fx
		old := currentRepo
		t.Cleanup(func() { currentRepo = old })
		currentRepo = func() (string, error) { return "ghe.example.com/cur/repo", nil }
		_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		if Code(err) != CodeUnsupportedHost || len(f.events) != 0 {
			t.Errorf("err = %v, events %v", err, f.events)
		}
	})
	t.Run("gh attestation trusted-root fails", func(t *testing.T) {
		f := &fakeHost{}
		useRequest(t, f)
		useHookFiles(t, f, errors.New("gh attestation trusted-root: tuf: no network"))
		_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		if Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "tuf: no network") || len(f.events) != 0 {
			t.Errorf("err = %v, events %v", err, f.events)
		}
	})
	t.Run("rendering the official files fails", func(t *testing.T) {
		f := &fakeHost{}
		useRequest(t, f)
		useHookFiles(t, f, Errorf(CodePodmanFailed, "render the workflow files: pull refused"))
		_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		if Code(err) != CodePodmanFailed || !strings.Contains(err.Error(), "pull refused") || len(f.events) != 0 {
			t.Errorf("err = %v, events %v", err, f.events)
		}
	})
	t.Run("the hook write fails", func(t *testing.T) {
		archiveURL(t)
		f := &fakeHost{shellExit: map[string]int{"hook-write": 1}, shellErr: map[string]string{"hook-write": "tar: no space"}}
		fx := useRequest(t, f)
		_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		if Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "tar: no space") {
			t.Errorf("err = %v", err)
		}
		for _, e := range f.events {
			if strings.HasPrefix(e, "extract") || strings.HasPrefix(e, "unit") {
				t.Errorf("a slot was installed after the hook failed: %v", f.events)
			}
		}
		if len(fx.req.bodies) != 0 {
			t.Errorf("a request was opened after the hook failed")
		}
	})
}

func TestRunnerAddAdminHasNoHook(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, loggedIn: map[string]bool{"claude": true}, volumes: map[string]bool{"claude": true, "nibfin-tools": true}}
	useRunnerHost(t, f)
	p, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findPlanStep(p, "hook"); ok {
		t.Error("the admin route plans a hook")
	}
	for _, s := range f.shells {
		if strings.HasPrefix(s.script, "hook") {
			t.Errorf("the admin route ran %s", s.script)
		}
	}
}

func TestRunnerAddHookReadBack(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": {configured: true, unit: true, enabled: true, active: true, env: "claude", hook: true, name: "n1", url: "https://github.com/cur/repo"}}}
	useRequest(t, f)
	f.hook = wantHook(false)
	o := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1})
	plan := Plan{Steps: []Step{slotStep("claude-1")}}
	if _, err := o.ReadBack(context.Background(), plan); err != nil {
		t.Fatalf("current: %v", err)
	}

	f.hook["check.mjs"] = []byte("old\n")
	f.hook["stray"] = []byte("x")
	r, _ := o.ReadBack(context.Background(), plan)
	if c, _ := itemOf(r, "hook"); c.Confirmed || c.State != "outdated: check.mjs differs, stray unexpected" {
		t.Errorf("outdated hook item = %+v", c)
	}

	f.hook = wantHook(false)
	f.hookMode = "755"
	r, _ = o.ReadBack(context.Background(), plan)
	if c, _ := itemOf(r, "hook"); c.Confirmed || !strings.Contains(c.State, "directory mode 0755") {
		t.Errorf("mode item = %+v", c)
	}

	f.hookMode = ""
	f.slots["claude-1"].hook = false
	_, err := o.ReadBack(context.Background(), plan)
	r, _ = o.ReadBack(context.Background(), plan)
	if c, _ := itemOf(r, "runner:claude-1"); Code(err) != CodeRunnerMissing || c.Confirmed || !strings.Contains(c.Reason, ".env lacks the job-started hook") {
		t.Errorf("err = %v, slot item = %+v", err, c)
	}
}

func TestRunnerAddNotesUnhookedSlots(t *testing.T) {
	archiveURL(t)
	f := &fakeHost{slots: map[string]*fakeSlot{
		"claude-1": {configured: true, unit: true, enabled: true, active: true, env: "claude", name: "n1", url: "https://github.com/cur/repo"},
		"codex-1":  {configured: true, unit: true, enabled: true, active: true, env: "codex", hook: true, name: "n2", url: "https://github.com/cur/repo"},
	}}
	useRequest(t, f)
	p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range p.Notes {
		if strings.Contains(n, "The Runners claude-1 were added before the job-started hook") {
			found = true
		}
		if strings.Contains(n, "codex-1") {
			t.Errorf("a hooked slot is named: %s", n)
		}
	}
	if !found {
		t.Errorf("notes = %q", p.Notes)
	}
}

func TestHookTar(t *testing.T) {
	b, err := hookTar(wantHook(true))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(b))
	var names []string
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, fmt.Sprintf("%s %o", h.Name, h.Mode))
	}
	want := []string{"check.mjs 600", "config 600", "job-started.sh 600", "known_hosts 600", "official/ 700",
		"official/nibfin-job.yml 600", "official/nibfin-shared-runner.yml 600", "trusted_root.json 600"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("tar = %v", names)
	}
	if _, err := hookTar(map[string][]byte{"../x": nil}); err == nil {
		t.Error("a name outside the directory was packed")
	}
}

// --- runner remove ---------------------------------------------------------

func TestRunnerRemoveHook(t *testing.T) {
	slot := func(provider string) *fakeSlot {
		return &fakeSlot{configured: true, unit: true, enabled: true, active: true, hook: true, url: "https://github.com/cur/repo"}
	}
	request := func(t *testing.T, f *fakeHost) {
		fx := useRunnerHost(t, f)
		fx.gh.perms = &repoPerms{Push: true}
		f.hook = wantHook(false)
	}
	hasHookStep := func(p Plan) bool {
		_, ok := findPlanStep(p, "hook")
		return ok
	}

	t.Run("the last runner on a local Linux host", func(t *testing.T) {
		f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": slot("claude")}}
		request(t, f)
		p, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
		if err != nil {
			t.Fatalf("Run: %v (events %v)", err, f.events)
		}
		hs, ok := findPlanStep(p, "hook")
		if !ok || hs.Action != actRemoveHook || hs.Target != hookAt || hs.Detail != "no runner remains on fake" {
			t.Errorf("hook step = %+v", hs)
		}
		if f.hook != nil || f.events[len(f.events)-1] != "rm hook "+hookAt || indexOf(f.events, "purge claude-1") > indexOf(f.events, "rm hook "+hookAt) {
			t.Errorf("events = %v", f.events)
		}
		if c, _ := itemOf(r, "hook"); !c.Confirmed || c.State != "absent" {
			t.Errorf("hook item = %+v", c)
		}
	})
	t.Run("with the autostart entry", func(t *testing.T) {
		f := &fakeHost{vm: true, slots: map[string]*fakeSlot{"claude-1": slot("claude")}, entry: host.Autostart{Kind: "launchd", Name: "nibfin", Present: true, Current: true}}
		request(t, f)
		p, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if _, ok := findPlanStep(p, "autostart"); !ok || !hasHookStep(p) || f.hook != nil || f.entry.Present {
			t.Errorf("plan %v, hook %v, entry %+v", steps(p), f.hook != nil, f.entry)
		}
	})
	t.Run("some runners remain", func(t *testing.T) {
		f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": slot("claude"), "claude-2": slot("claude")}}
		request(t, f)
		p, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 1}), &yes{})
		if err != nil || hasHookStep(p) || f.hook == nil {
			t.Errorf("err %v, plan %v, hook kept %v", err, steps(p), f.hook != nil)
		}
	})
	t.Run("another provider's runners remain", func(t *testing.T) {
		f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": slot("claude"), "codex-1": slot("codex")}}
		request(t, f)
		p, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
		if err != nil || hasHookStep(p) || f.hook == nil {
			t.Errorf("err %v, plan %v, hook kept %v", err, steps(p), f.hook != nil)
		}
	})
	t.Run("a rerun after the slots went", func(t *testing.T) {
		f := &fakeHost{slots: map[string]*fakeSlot{}}
		request(t, f)
		p, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "codex"}), &yes{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := steps(p); !reflect.DeepEqual(got, []string{"remove job-started hook " + hookAt}) || f.hook != nil {
			t.Errorf("plan = %v, hook %v", got, f.hook != nil)
		}
		if c, _ := itemOf(r, "hook"); !c.Confirmed {
			t.Errorf("hook item = %+v", c)
		}
	})
	t.Run("nothing left", func(t *testing.T) {
		f := &fakeHost{slots: map[string]*fakeSlot{}}
		request(t, f)
		f.hook = nil
		_, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
		if Code(err) != CodeNoRunners {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("the removal fails", func(t *testing.T) {
		f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": slot("claude")}, shellExit: map[string]int{"hook-remove": 2}, shellErr: map[string]string{"hook-remove": "refusing"}}
		request(t, f)
		_, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
		if Code(err) != CodeRunnerRemoveFailed || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("err = %v", err)
		}
	})
	_ = runners.HookDir
}

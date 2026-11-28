package op

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/host"
	"github.com/taco3064/gh-nibfin/internal/runners"
)

// upgradeShell simulates slotSpecScript and slotUpgradeScript on the fake
// host; it returns their stdout and the stdin they expect.
func (f *fakeHost) upgradeShell(name, slot string, args []string, exit int) (stdout, stdinSHA string) {
	s := f.slots[slot]
	if name == "spec" {
		if s == nil {
			return "env no\nunit -\njob no\n", ""
		}
		out := "env no\n"
		if s.env == args[2] {
			out = "env yes\n"
		}
		if s.unit {
			text, _ := unitText(slot, args[0])
			if s.oldUnit {
				text = "[Unit]\nDescription=old\n"
			}
			out += "unit b64:" + base64.StdEncoding.EncodeToString([]byte(text)) + "\n"
		} else {
			out += "unit -\n"
		}
		if s.job {
			return out + "job yes\n", ""
		}
		return out + "job no\n", ""
	}
	f.events = append(f.events, "upgrade "+slot+" env="+args[4]+" unit="+args[5])
	text := ""
	if args[5] == "1" {
		text, _ = unitText(slot, args[0])
	}
	if exit == 0 && s != nil {
		if args[4] == "1" {
			s.env = args[3]
		}
		if args[5] == "1" {
			s.unit, s.oldUnit = true, false
		}
		s.enabled, s.active = true, true
	}
	return "", shaHex(text)
}

func (g *fakeRunnerAPI) SetCustomLabels(_ context.Context, repo string, id int64, labels []string) error {
	if g.setErr != nil {
		return g.setErr
	}
	r := g.reg(repo, id)
	if r == nil {
		return errors.New("no such runner")
	}
	g.host.events = append(g.host.events, "labels "+r.Name+" "+strings.Join(labels, ","))
	var keep []ghLabel
	for _, l := range r.Labels {
		if l.Type != "custom" {
			keep = append(keep, l)
		}
	}
	for _, l := range labels {
		keep = append(keep, ghLabel{l, "custom"})
	}
	r.Labels = keep
	return nil
}

// upgradeHost is a host whose claude-1 predates the current spec (no .env
// line, an old unit, the old label), whose claude-2 is current and whose
// codex-1 runs a Job and lacks only its .env line and labels. All serve
// github.com/o/r; the gh login (user 42) is admin.
func upgradeHost(t *testing.T) (*fakeHost, *fakeRunnerAPI) {
	t.Helper()
	old := done(hostName("claude-1"), "https://github.com/o/r")
	old.oldUnit = true
	cur := done(hostName("claude-2"), "https://github.com/o/r")
	cur.env = "claude"
	busy := done(hostName("codex-1"), "https://github.com/o/r")
	busy.job = true
	f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": old, "claude-2": cur, "codex-1": busy}}
	fx := useRunnerHost(t, f)
	current := reg(2, hostName("claude-2"), "github.com/o/r", false)
	current.Labels = labelled("nibfin-42", "nibfin")
	fx.gh.regs = []*fakeReg{
		reg(1, hostName("claude-1"), "github.com/o/r", false),
		current,
		reg(3, hostName("codex-1"), "github.com/o/r", true),
	}
	return f, fx.gh
}

func ids(steps []Step) []string {
	out := []string{}
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}

func TestHostUpgradePlanApplyReadBack(t *testing.T) {
	f, gh := upgradeHost(t)
	ctx := context.Background()
	h := RunnersUpgrade()
	steps, notes, err := h.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"runner-env:claude-1", "runner-unit:claude-1", "runner-labels:claude-1",
		"runner-env:claude-2", "runner-unit:claude-2", "runner-labels:claude-2",
		"runner-env:codex-1", "runner-unit:codex-1", "runner-labels:codex-1",
	}
	if !reflect.DeepEqual(ids(steps), want) || len(notes) != 0 {
		t.Fatalf("steps %q, notes %q", ids(steps), notes)
	}
	changing := []string{"runner-env:claude-1", "runner-unit:claude-1", "runner-labels:claude-1", "runner-env:codex-1", "runner-labels:codex-1"}
	if got := ids(Changes(steps)); !reflect.DeepEqual(got, changing) {
		t.Errorf("changing %q", got)
	}
	byID := map[string]Step{}
	for _, s := range steps {
		byID[s.ID] = s
	}
	if s := byID["runner-labels:claude-1"]; s.Current != "nibfin, nibfin-42-claude" || s.Next != "nibfin, nibfin-42" || s.Action != actSetLabels {
		t.Errorf("labels item %+v", s)
	}
	if s := byID["runner-unit:claude-1"]; s.Current != "differs" {
		t.Errorf("unit item %+v", s)
	}
	if s := byID["runner-env:claude-1"]; s.Current != "missing" || !strings.Contains(s.Detail, "NIBFIN_PROVIDER=claude, other lines kept") || strings.Contains(s.Detail, waitsForJob) {
		t.Errorf("env item %+v", s)
	}
	if s := byID["runner-env:codex-1"]; !strings.Contains(s.Detail, waitsForJob) {
		t.Errorf("busy env item %+v", s)
	}
	for _, sh := range f.shells {
		if sh.script == "upgrade" {
			t.Fatalf("plan wrote: %+v", sh)
		}
	}
	if len(f.events) != 0 {
		t.Fatalf("plan wrote: %q", f.events)
	}

	var events []string
	pctx := WithProgress(ctx, func(e Event) { events = append(events, e.Phase+" "+e.ID) })
	if err := h.Apply(pctx, Changes(steps)); err != nil {
		t.Fatal(err)
	}
	wantEvents := []string{
		"upgrade claude-1 env=1 unit=1",
		"labels " + hostName("claude-1") + " nibfin,nibfin-42",
		// The runner running a Job comes last.
		"upgrade codex-1 env=1 unit=0",
		"labels " + hostName("codex-1") + " nibfin,nibfin-42",
	}
	if !reflect.DeepEqual(f.events, wantEvents) {
		t.Errorf("events %q", f.events)
	}
	wantProgress := []string{
		"started runner-env:claude-1", "started runner-unit:claude-1", "done runner-env:claude-1", "done runner-unit:claude-1",
		"started runner-labels:claude-1", "done runner-labels:claude-1",
		"started runner-env:codex-1", "done runner-env:codex-1",
		"started runner-labels:codex-1", "done runner-labels:codex-1",
	}
	if !reflect.DeepEqual(events, wantProgress) {
		t.Errorf("progress %q", events)
	}
	for _, sh := range f.shells {
		if sh.script == "upgrade" && sh.args[0] == slotDir+"/claude-1" {
			want := []string{slotDir + "/claude-1", unitDir, "nibfin-runner-claude-1.service", "claude", "1", "1"}
			if !reflect.DeepEqual(sh.args, want) {
				t.Errorf("upgrade args %q", sh.args)
			}
		}
		if sh.script == "unitdir" {
			t.Errorf("unit dir script on a local host")
		}
	}

	var checks Checks
	r := Result{}
	if err := h.ReadBack(ctx, &checks, r); err != nil {
		t.Fatal(err)
	}
	list := checks.List(nil)
	if len(list) != len(steps) {
		t.Fatalf("checks %+v", list)
	}
	for _, c := range list {
		if !c.Confirmed {
			t.Errorf("not confirmed: %+v", c)
		}
	}
	m := resultJSON(t, r)["runners"].(map[string]any)
	slots := m["slots"].([]any)
	if m["repo"] != "github.com/o/r" || len(slots) != 3 {
		t.Fatalf("runners %v", m)
	}
	first := slots[0].(map[string]any)
	if first["env"] != true || first["unit"] != "current" || first["status"] != "online" || !reflect.DeepEqual(first["readOnlyLabels"], []any{"self-hosted", "Linux", "X64"}) {
		t.Errorf("slot %v", first)
	}

	// A second plan has nothing to change.
	steps, _, err = RunnersUpgrade().Plan(ctx)
	if err != nil || len(Changes(steps)) != 0 {
		t.Errorf("replan %q, %v", ids(Changes(steps)), err)
	}
	_ = gh
}

func TestHostUpgradeMachine(t *testing.T) {
	f, _ := upgradeHost(t)
	f.vm = true
	h := RunnersUpgrade()
	steps, _, err := h.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Apply(context.Background(), Changes(steps)); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, sh := range f.shells {
		if sh.script == "unitdir" || sh.script == "upgrade" {
			order = append(order, sh.script+" "+sh.args[0])
		}
	}
	// The unit directory is fixed before a unit is written, as in runner add.
	want := []string{"unitdir " + unitDir, "upgrade " + slotDir + "/claude-1", "upgrade " + slotDir + "/codex-1"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order %q", order)
	}
}

func TestHostUpgradeNoAdmin(t *testing.T) {
	f, gh := upgradeHost(t)
	gh.perms = &repoPerms{Push: true}
	h := RunnersUpgrade()
	steps, notes, err := h.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if strings.HasPrefix(s.ID, upLabelsPrefix) {
			t.Errorf("labels item without admin: %+v", s)
		}
	}
	if !reflect.DeepEqual(notes, []string{"labels left as they are: changing them needs admin of github.com/o/r"}) {
		t.Errorf("notes %q", notes)
	}
	// A labels step is refused.
	if err := h.Apply(context.Background(), []Step{upLabelsStep("claude-1")}); Code(err) != CodeInvalidArgument {
		t.Errorf("labels apply: %v", err)
	}
	if err := h.Apply(context.Background(), Changes(steps)); err != nil {
		t.Fatal(err)
	}
	var checks Checks
	if err := h.ReadBack(context.Background(), &checks, Result{}); err != nil {
		t.Fatal(err)
	}
	if len(gh.lists) != 0 {
		t.Errorf("listed runners without admin: %v", gh.lists)
	}
	for _, e := range f.events {
		if strings.HasPrefix(e, "labels ") {
			t.Errorf("set labels without admin: %q", e)
		}
	}
}

func TestHostUpgradeNotes(t *testing.T) {
	f, gh := upgradeHost(t)
	// claude-2 is not registered; claude-3 is incomplete.
	gh.regs = append(gh.regs[:1], gh.regs[2])
	f.slots["claude-3"] = &fakeSlot{}
	steps, notes, err := RunnersUpgrade().Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"claude-3 is incomplete; run gh nibfin runner add claude",
		"claude-2 (" + hostName("claude-2") + ") is not registered on GitHub; run gh nibfin runner remove and gh nibfin runner add",
	}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("notes %q", notes)
	}
	for _, s := range steps {
		if s.ID == "runner-labels:claude-2" || strings.HasSuffix(s.ID, ":claude-3") {
			t.Errorf("item %+v", s)
		}
	}
}

func TestHostUpgradeNoHost(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		f    *fakeHost
		note string
	}{
		{"no podman", host.ErrPodmanMissing, nil, noRunnerHost},
		{"no machine", host.ErrMachineMissing, nil, noRunnerHost},
		{"unsupported", host.ErrUnsupported, nil, noRunnerHost},
		{"stopped", nil, &fakeHost{stopped: true}, "fake is stopped, so its Runners were not checked; start it and run gh nibfin upgrade again"},
		{"no runners", nil, &fakeHost{slots: map[string]*fakeSlot{}}, "no Runners on fake"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.f != nil {
				useRunnerHost(t, tc.f)
			} else {
				old := detectHost
				detectHost = func(context.Context) (runnerHost, error) { return nil, hostError(tc.err) }
				t.Cleanup(func() { detectHost = old })
			}
			h := RunnersUpgrade()
			steps, notes, err := h.Plan(context.Background())
			if err != nil || len(steps) != 0 || !reflect.DeepEqual(notes, []string{tc.note}) {
				t.Fatalf("steps %+v, notes %q, err %v", steps, notes, err)
			}
			if tc.f != nil && tc.f.starts != 0 {
				t.Errorf("started the machine")
			}
			var checks Checks
			r := Result{}
			if err := h.ReadBack(context.Background(), &checks, r); err != nil || len(checks.List(nil)) != 0 || r["runners"] == nil {
				t.Errorf("read back %v, %v", r, err)
			}
		})
	}
	// Any other host error fails the plan.
	old := detectHost
	detectHost = func(context.Context) (runnerHost, error) { return nil, hostError(host.ErrPodman) }
	t.Cleanup(func() { detectHost = old })
	if _, _, err := RunnersUpgrade().Plan(context.Background()); Code(err) != CodePodmanFailed {
		t.Errorf("podman failure: %v", err)
	}
}

func TestHostUpgradeRepoConflict(t *testing.T) {
	f, _ := upgradeHost(t)
	f.slots["codex-1"].url = "https://github.com/o/other"
	if _, _, err := RunnersUpgrade().Plan(context.Background()); Code(err) != CodeRunnerRepoConflict {
		t.Errorf("err %v", err)
	}
}

func TestHostUpgradeFailures(t *testing.T) {
	t.Run("script", func(t *testing.T) {
		f, _ := upgradeHost(t)
		f.shellExit = map[string]int{"upgrade": 1}
		f.shellErr = map[string]string{"upgrade": "boom"}
		h := RunnersUpgrade()
		steps, _, err := h.Plan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var failed []string
		ctx := WithProgress(context.Background(), func(e Event) {
			if e.Phase == PhaseFailed {
				failed = append(failed, e.ID+" "+e.Code)
			}
		})
		err = h.Apply(ctx, Changes(steps))
		if Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "claude-1") {
			t.Errorf("err %v", err)
		}
		// The first failure stops the run: codex-1 is not touched.
		if !reflect.DeepEqual(f.events, []string{"upgrade claude-1 env=1 unit=1"}) {
			t.Errorf("events %q", f.events)
		}
		if !reflect.DeepEqual(failed, []string{"runner-env:claude-1 runner_install_failed", "runner-unit:claude-1 runner_install_failed"}) {
			t.Errorf("failed %q", failed)
		}
	})
	t.Run("labels", func(t *testing.T) {
		f, gh := upgradeHost(t)
		gh.setErr = Errorf(CodeGitHubAPIFailed, "HTTP 422")
		h := RunnersUpgrade()
		steps, _, err := h.Plan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Apply(context.Background(), Changes(steps)); Code(err) != CodeGitHubAPIFailed {
			t.Errorf("err %v", err)
		}
		if !reflect.DeepEqual(f.events, []string{"upgrade claude-1 env=1 unit=1"}) {
			t.Errorf("events %q", f.events)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		upgradeHost(t)
		h := RunnersUpgrade()
		if err := h.Apply(context.Background(), []Step{upEnvStep("claude-1")}); Code(err) != CodeInvalidArgument {
			t.Errorf("apply before plan: %v", err)
		}
		if _, _, err := h.Plan(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, s := range []Step{
			{ID: "runner-env:claude-9", Target: "claude-9"},
			{ID: "runner-env:claude-1", Target: "claude-2"},
			{ID: "file:x", Target: "x"},
		} {
			if err := h.Apply(context.Background(), []Step{s}); Code(err) != CodeInvalidArgument {
				t.Errorf("%+v: %v", s, err)
			}
		}
	})
}

func TestHostUpgradeReadBack(t *testing.T) {
	t.Run("not applied", func(t *testing.T) {
		upgradeHost(t)
		h := RunnersUpgrade()
		if _, _, err := h.Plan(context.Background()); err != nil {
			t.Fatal(err)
		}
		var checks Checks
		err := h.ReadBack(context.Background(), &checks, Result{})
		if Code(err) != CodeNotApplied || !strings.Contains(err.Error(), "claude-1, codex-1") {
			t.Errorf("err %v", err)
		}
		for _, c := range checks.List(nil) {
			if c.ID == "runner-unit:claude-1" && (c.Confirmed || c.Reason != "service differs") {
				t.Errorf("unit check %+v", c)
			}
		}
	})
	t.Run("offline", func(t *testing.T) {
		f, gh := upgradeHost(t)
		delete(f.slots, "claude-1")
		delete(f.slots, "codex-1")
		gh.regs = gh.regs[1:2]
		gh.regs[0].Status = "offline"
		h := RunnersUpgrade()
		if _, _, err := h.Plan(context.Background()); err != nil {
			t.Fatal(err)
		}
		var checks Checks
		r := Result{}
		if err := h.ReadBack(context.Background(), &checks, r); err != nil {
			t.Fatalf("offline failed: %v", err)
		}
		for _, c := range checks.List(nil) {
			if c.ID == "runner-labels:claude-2" && (!c.Confirmed || !strings.Contains(c.State, "offline")) {
				t.Errorf("labels check %+v", c)
			}
		}
	})
}

func TestSharedRunnerDefinitions(t *testing.T) {
	// stopScript is the text it had before workersFn and waitWorkers were
	// extracted, without the busy guard runner remove no longer uses.
	const before = `r=$(cd -P -- "$1" 2>/dev/null && pwd -P) || r=$1
workers() {
  for p in /proc/[0-9]*; do
    e=$(readlink "$p/exe" 2>/dev/null) || continue
    case $e in
    "$r"/*/Runner.Worker|"$r"/*/"Runner.Worker (deleted)") printf '%s\n' "${p#/proc/}" ;;
    esac
  done
}
w=$(workers)
while [ -n "$w" ]; do
  for p in $w; do
    tail --pid="$p" -f /dev/null || exit 1
  done
  w=$(workers)
done
if [ -e "$2/$3" ]; then
  systemctl --user disable --now "$3"
fi
`
	if stopScript != before {
		t.Errorf("stopScript changed:\n%s", stopScript)
	}
	// drainScript is stopScript without the stop.
	if want := strings.TrimSuffix(before, "if [ -e \"$2/$3\" ]; then\n  systemctl --user disable --now \"$3\"\nfi\n"); drainScript != want || drainScript == before {
		t.Errorf("drainScript:\n%s", drainScript)
	}
	a := &runnerAdd{}
	if got := a.labels(&addEnv{admin: true, user: ghUser{ID: 42}}); got != "nibfin,nibfin-42" {
		t.Errorf("admin labels %q", got)
	}
	if got := a.labels(&addEnv{user: ghUser{ID: 42}}); got != "nibfin-42" {
		t.Errorf("labels %q", got)
	}
	if !sameSet([]string{"nibfin-42", "nibfin"}, runnerLabels(true, 42)) || sameSet([]string{"nibfin", "nibfin-42-claude"}, runnerLabels(true, 42)) {
		t.Error("sameSet")
	}
}

// TestUpgradeScriptsUnderSh runs the real slotSpecScript and
// slotUpgradeScript against a directory tree, with a stand-in for systemctl
// and a copy of the test binary named Runner.Worker as a Job.
func TestUpgradeScriptsUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip("no test binary")
	}
	if err := exec.Command("tail", "--pid=1", "--version").Run(); err != nil {
		t.Skip("no tail --pid")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	log := filepath.Join(root, "systemctl.log")
	alive := filepath.Join(root, "worker.pid")
	// systemctl records its arguments, and "alive" when the worker still runs.
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\n[ -f \"$PIDF\" ] && kill -0 \"$(cat \"$PIDF\")\" 2>/dev/null && echo alive >> \"$LOG\"\necho \"$*\" >> \"$LOG\"\n"), 0o755)
	h := &localHost{env: []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "LOG=" + log, "PIDF=" + alive}}
	ctx := context.Background()
	slot := filepath.Join(root, "data", "nibfin", "runners", "claude-1")
	units := filepath.Join(root, "units")
	os.MkdirAll(filepath.Join(slot, "bin"), 0o755)
	unit := runners.UnitName("claude-1")
	os.WriteFile(filepath.Join(slot, ".env"), []byte("LANG=C.UTF-8\nNIBFIN_PROVIDER=codex\n"), 0o644)
	inv := &runners.Inventory{Dir: filepath.Dir(slot), UnitDir: units}
	r := runners.Runner{Slot: "claude-1", Provider: "claude", Configured: true}
	u := &HostUpgrade{host: h}
	readLog := func() string {
		b, _ := os.ReadFile(log)
		os.Remove(log)
		return string(b)
	}

	sp, err := u.spec(ctx, inv, r, CodeRunnerInstallFailed)
	if err != nil || sp.env || sp.has || sp.job {
		t.Fatalf("spec %+v, %v", sp, err)
	}

	// A Job runs: the spec sees it, and the upgrade waits for it to end
	// before it stops the service.
	worker := filepath.Join(slot, "bin", "Runner.Worker")
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worker, b, 0o755); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(worker, "-test.run=^TestFakeWorker$")
	c.Env = append(os.Environ(), "GO_WANT_FAKE_WORKER=1s")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(alive, []byte(strconv.Itoa(c.Process.Pid)), 0o644)
	go c.Wait()
	if sp, err = u.spec(ctx, inv, r, CodeRunnerInstallFailed); err != nil || !sp.job {
		t.Fatalf("spec with a Job %+v, %v", sp, err)
	}
	os.MkdirAll(units, 0o755)
	os.WriteFile(filepath.Join(units, unit), []byte("[Unit]\nDescription=old\n"), 0o644)
	began := time.Now()
	if err := u.upgradeSlot(ctx, inv, "claude-1", "claude", true, true); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if took := time.Since(began); took < 500*time.Millisecond {
		t.Errorf("upgraded after %v, before the Job ended", took)
	}
	want := "--user stop " + unit + "\n--user daemon-reload\n--user enable --now " + unit + "\n"
	if l := readLog(); l != want {
		t.Errorf("systemctl = %q, want %q", l, want)
	}
	env, _ := os.ReadFile(filepath.Join(slot, ".env"))
	if string(env) != "LANG=C.UTF-8\nNIBFIN_PROVIDER=claude\n" {
		t.Errorf(".env = %q", env)
	}
	text, _ := unitText("claude-1", slot)
	got, _ := os.ReadFile(filepath.Join(units, unit))
	if string(got) != text {
		t.Errorf("unit = %q", got)
	}
	if sp, err = u.spec(ctx, inv, r, CodeRunnerInstallFailed); err != nil || !sp.env || sp.unit != text || sp.job {
		t.Errorf("spec after %+v, %v", sp, err)
	}

	// Only the .env: the unit stays, the service restarts.
	os.WriteFile(filepath.Join(slot, ".env"), []byte("A=1\n"), 0o644)
	if err := u.upgradeSlot(ctx, inv, "claude-1", "claude", true, false); err != nil {
		t.Fatal(err)
	}
	if l := readLog(); l != "--user stop "+unit+"\n--user enable --now "+unit+"\n" {
		t.Errorf("systemctl = %q", l)
	}
	if env, _ := os.ReadFile(filepath.Join(slot, ".env")); string(env) != "A=1\nNIBFIN_PROVIDER=claude\n" {
		t.Errorf(".env = %q", env)
	}

	// A failing stop writes nothing.
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho \"$*\" >> \"$LOG\"\n[ \"$2\" = stop ] && exit 1\nexit 0\n"), 0o755)
	os.WriteFile(filepath.Join(slot, ".env"), []byte("B=1\n"), 0o644)
	if err := u.upgradeSlot(ctx, inv, "claude-1", "claude", true, true); Code(err) != CodeRunnerInstallFailed {
		t.Errorf("failing stop: %v", err)
	}
	if env, _ := os.ReadFile(filepath.Join(slot, ".env")); string(env) != "B=1\n" {
		t.Errorf(".env written after a failed stop: %q", env)
	}
}

func TestSetCustomLabelsREST(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(b))
		if r.URL.Path == "/api/v3/repos/o/r/actions/runners/1/labels" {
			io.WriteString(w, `{"total_count":3,"labels":[]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Not Found"}`)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c, err := api.NewRESTClient(api.ClientOptions{Host: "ghe.example", AuthToken: "x", Transport: rewrite{u}})
	if err != nil {
		t.Fatal(err)
	}
	g := restGitHub{c}
	ctx := context.Background()
	if err := g.SetCustomLabels(ctx, "ghe.example/o/r", 1, []string{"nibfin", "nibfin-42"}); err != nil {
		t.Errorf("set: %v", err)
	}
	if err := g.SetCustomLabels(ctx, "ghe.example/o/r", 2, []string{"nibfin"}); Code(err) != CodeGitHubAPIFailed || !strings.Contains(err.Error(), "404") {
		t.Errorf("set 404: %v", err)
	}
	want := []string{
		`PUT /api/v3/repos/o/r/actions/runners/1/labels {"labels":["nibfin","nibfin-42"]}`,
		`PUT /api/v3/repos/o/r/actions/runners/2/labels {"labels":["nibfin"]}`,
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("requests = %q", seen)
	}
}

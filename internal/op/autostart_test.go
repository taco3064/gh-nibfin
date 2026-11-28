package op

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/host"
)

func (f *fakeHost) Autostart(context.Context) (host.Autostart, error) { return f.entry, f.entryErr }

func (f *fakeHost) EnsureAutostart(context.Context) (host.Autostart, error) {
	if f.registerErr != nil {
		return f.entry, f.registerErr
	}
	if f.entry.Kind != "" && !f.entry.Current {
		f.events = append(f.events, "register autostart")
		if !f.staleRead {
			f.entry.Present, f.entry.Current, f.entry.Outdated = true, true, ""
		}
	}
	return f.entry, nil
}

func (f *fakeHost) StartViaAutostart(context.Context) error {
	f.autoStarts++
	f.stopped = false
	f.events = append(f.events, "start via autostart")
	return nil
}

func (f *fakeHost) RemoveAutostart(context.Context) error {
	if f.entry.Present {
		f.events = append(f.events, "remove autostart")
	}
	f.entry.Present, f.entry.Current = false, false
	return nil
}

func (f *fakeHost) IdleTimeoutSet() (bool, error) { return f.idleSet, f.idleErr }

func (f *fakeHost) EnsureIdleTimeout() (bool, error) {
	if f.idleErr != nil || f.idleSet {
		return false, f.idleErr
	}
	f.idleSet = true
	f.events = append(f.events, "set wslconfig")
	return true, nil
}

func (f *fakeHost) CheckWSL(context.Context) error { return f.wslErr }

// winEntry is an absent Windows task.
func winEntry() host.Autostart {
	return host.Autostart{Kind: host.KindTask, Name: host.TaskName}
}

func current(a host.Autostart) host.Autostart {
	a.Present, a.Current = true, true
	return a
}

func TestRunnerAddHostSteps(t *testing.T) {
	f := &fakeHost{vm: true, wsl: true, entry: winEntry(), slots: map[string]*fakeSlot{},
		volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useRunnerHost(t, f)
	p, r, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := steps(p)
	want := []string{`set wslconfig %UserProfile%\.wslconfig`, `register autostart task "Nibfin podman machine"`}
	if !reflect.DeepEqual(got[:2], want) {
		t.Fatalf("plan = %q", got)
	}
	if d := p.Changes()[0].Detail; !strings.Contains(d, "[wsl2] vmIdleTimeout=-1") || !strings.Contains(d, "takes effect after the next wsl --shutdown; runner add does not shut down the VM") {
		t.Errorf("wslconfig detail = %q", d)
	}
	if d := p.Changes()[1].Detail; !strings.Contains(d, "at logon and on resume from sleep") {
		t.Errorf("autostart detail = %q", d)
	}
	if f.events[0] != "set wslconfig" || f.events[1] != "register autostart" || f.autoStarts != 0 {
		t.Errorf("events = %v, autoStarts %d", f.events, f.autoStarts)
	}
	m := resultJSON(t, r)
	if a := m["autostart"].(map[string]any); a["present"] != true || a["current"] != true || a["kind"] != "task" {
		t.Errorf("autostart = %v", a)
	}
	if w := m["wslconfig"].(map[string]any); w["vmIdleTimeout"] != "-1" || w["effective"] != "after wsl --shutdown" {
		t.Errorf("wslconfig = %v", w)
	}

	// A rerun plans no host step.
	f.events = nil
	p, err = RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Changes() {
		if s.Action == actSetWSLConfig || s.Action == actRegisterAutostart {
			t.Errorf("rerun plans %v", s)
		}
	}
}

func TestRunnerAddStoppedMachineStartsThroughEntry(t *testing.T) {
	f := &fakeHost{stopped: true, vm: true, wsl: true, entry: winEntry(), slots: map[string]*fakeSlot{},
		volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useRunnerHost(t, f)
	o := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1})
	p, err := o.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := steps(p)
	want := []string{`set wslconfig %UserProfile%\.wslconfig`, `register autostart task "Nibfin podman machine"`, "start fake"}
	if !reflect.DeepEqual(got[:3], want) || p.Changes()[2].Detail != `through task "Nibfin podman machine"` {
		t.Fatalf("plan = %q, %+v", got, p.Changes()[2])
	}
	if strings.Contains(p.Changes()[0].Detail, "wsl --shutdown") {
		t.Errorf("a stopped VM picks the key up at its start: %q", p.Changes()[0].Detail)
	}
	if err := o.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if want := []string{"set wslconfig", "register autostart", "start via autostart", "linger"}; !reflect.DeepEqual(f.events[:4], want) || f.starts != 0 {
		t.Errorf("events = %v, starts %d", f.events, f.starts)
	}
	r, err := o.ReadBack(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if w := resultJSON(t, r)["wslconfig"].(map[string]any); w["effective"] != "now" {
		t.Errorf("wslconfig = %v", w)
	}

	// A plan made while the entry is current still starts through it.
	f.stopped, f.events = true, nil
	o = RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1})
	if p, err = o.Plan(context.Background()); err != nil || steps(p)[0] != "start fake" {
		t.Fatalf("plan = %q, %v", steps(p), err)
	}
	if err := o.Apply(context.Background(), p); err != nil || f.autoStarts != 2 {
		t.Errorf("Apply: %v, autoStarts %d", err, f.autoStarts)
	}
}

func TestRunnerAddHostRefusals(t *testing.T) {
	f := &fakeHost{vm: true, wsl: true, entry: winEntry(), wslErr: errors.New("x")}
	useRunnerHost(t, f)
	f.wslErr = &hostErr{host.ErrUnsupported}
	if _, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background()); Code(err) != CodeUnsupportedHost {
		t.Errorf("old WSL: %v", err)
	}

	f = &fakeHost{vm: true, entry: winEntry(), registerErr: &hostErr{host.ErrAutostart},
		volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useRunnerHost(t, f)
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeAutostartFailed {
		t.Errorf("refused registration: %v", err)
	}
	for _, e := range f.events {
		if strings.HasPrefix(e, "extract") || e == "linger" {
			t.Errorf("a runner step ran after the refusal: %v", f.events)
		}
	}

	// Registered but reads back different.
	f = &fakeHost{vm: true, entry: winEntry(), staleRead: true,
		volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useRunnerHost(t, f)
	if _, _, err = Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{}); Code(err) != CodeAutostartFailed {
		t.Errorf("stale read: %v", err)
	}
}

func TestRunnerAddReadBackNotApplied(t *testing.T) {
	f := &fakeHost{vm: true, entry: current(winEntry())}
	useRunnerHost(t, f)
	f.loggedIn["claude"] = true
	f.volumes["claude"] = true
	o := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1})
	p := Plan{Steps: []Step{}}
	if _, err := o.ReadBack(context.Background(), p); err != nil {
		t.Fatalf("current: %v", err)
	}
	f.entry = winEntry()
	if _, err := o.ReadBack(context.Background(), p); Code(err) != CodeNotApplied || !strings.Contains(err.Error(), "is not registered") {
		t.Errorf("absent entry: %v", err)
	}
	f.entry, f.wsl = current(winEntry()), true
	if _, err := o.ReadBack(context.Background(), p); Code(err) != CodeNotApplied || !strings.Contains(err.Error(), "vmIdleTimeout") {
		t.Errorf("unset key: %v", err)
	}
}

func TestRunnerRemoveLastRunnerRemovesEntry(t *testing.T) {
	f, _ := removeHost(t)
	f.vm, f.wsl, f.entry = true, true, current(winEntry())

	// codex-1 remains: the entry stays.
	p, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
	if err != nil || hasStep(p.Steps, actRemoveAutostart) || !f.entry.Present {
		t.Fatalf("not last: %v, %q", err, steps(p))
	}

	// The last runner takes the entry; .wslconfig stays.
	f.events = nil
	p, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "codex"}), &yes{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"start fake", "remove runner codex-1", `remove autostart task "Nibfin podman machine"`, `keep wslconfig %UserProfile%\.wslconfig`}
	if !reflect.DeepEqual(allSteps(p), want) || !p.Steps[3].NoChange {
		t.Errorf("plan = %q", allSteps(p))
	}
	if f.entry.Present || f.events[len(f.events)-1] != "remove autostart" {
		t.Errorf("events = %v", f.events)
	}
	m := resultJSON(t, r)
	if a := m["autostart"].(map[string]any); a["present"] != false {
		t.Errorf("autostart = %v", a)
	}
	if !strings.Contains(m["wslconfig"].(string), "other WSL users may rely on vmIdleTimeout") {
		t.Errorf("wslconfig = %v", m["wslconfig"])
	}
}

func TestRunnerRemoveFinishesEntryRemoval(t *testing.T) {
	f := &fakeHost{vm: true, slots: map[string]*fakeSlot{}, entry: current(winEntry())}
	useRunnerHost(t, f)
	p, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
	if err != nil || !reflect.DeepEqual(steps(p), []string{`remove autostart task "Nibfin podman machine"`}) || p.ReadOnly {
		t.Fatalf("redo: %q, %v", steps(p), err)
	}
	if f.entry.Present {
		t.Error("entry left")
	}
	if _, err := RunnerRemove(RunnerRemoveParams{Provider: "claude"}).Plan(context.Background()); Code(err) != CodeNoRunners {
		t.Errorf("nothing to do: %v", err)
	}
}

func TestRunnerRemoveBusyKeepsEntry(t *testing.T) {
	f := &fakeHost{vm: true, entry: current(winEntry()), slots: map[string]*fakeSlot{
		"claude-1": done(hostName("claude-1"), "https://github.com/o/r"),
	}}
	fx := useRunnerHost(t, f)
	// GitHub refuses the delete of a runner it reports offline with a Job.
	r := reg(1, hostName("claude-1"), "github.com/o/r", true)
	r.Status = "offline"
	fx.gh.regs = []*fakeReg{r}
	_, res, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
	if Code(err) != CodeGitHubAPIFailed || !strings.Contains(err.Error(), "offline") || !f.entry.Present || res == nil {
		t.Errorf("busy: %v, entry %+v", err, f.entry)
	}
	for _, e := range f.events {
		if e == "stop claude-1" || e == "purge claude-1" {
			t.Errorf("went on: %q", f.events)
		}
	}
}

func TestRunnerRemoveStoppedPlansEntry(t *testing.T) {
	f := &fakeHost{stopped: true, vm: true, entry: current(winEntry()), slots: map[string]*fakeSlot{
		"claude-1": done(hostName("claude-1"), "https://github.com/o/r"),
	}}
	fx := useRunnerHost(t, f)
	fx.gh.regs = []*fakeReg{reg(1, hostName("claude-1"), "github.com/o/r", false)}
	p, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
	if err != nil || !hasStep(p.Steps, actRemoveAutostart) || f.starts != 1 || f.entry.Present {
		t.Errorf("stopped: %v, %q, starts %d, entry %+v", err, steps(p), f.starts, f.entry)
	}
}

func TestReadHostAutostart(t *testing.T) {
	f := &fakeHost{stopped: true, vm: true, entry: current(winEntry())}
	useRunnerHost(t, f)
	rep := ReadHost(context.Background())
	if rep.Autostart != (AutostartReport{Kind: "task", Name: host.TaskName, Present: true, Current: true}) || f.starts != 0 || f.autoStarts != 0 {
		t.Errorf("autostart = %+v", rep.Autostart)
	}
	f.entryErr = &hostErr{host.ErrAutostart}
	if rep = ReadHost(context.Background()); Code(rep.Autostart.Error) != CodeAutostartFailed {
		t.Errorf("error = %+v", rep.Autostart)
	}
	f2 := &fakeHost{}
	useRunnerHost(t, f2)
	if rep = ReadHost(context.Background()); rep.Autostart != (AutostartReport{}) {
		t.Errorf("linux = %+v", rep.Autostart)
	}
}

// hostErr is a host error of the given kind.
type hostErr struct{ kind error }

func (e *hostErr) Error() string { return e.kind.Error() + ": details" }
func (e *hostErr) Unwrap() error { return e.kind }

// lastWSLRunner is a WSL machine whose only runner, codex-1, runner remove
// takes, with the autostart task present and the codex login volume.
func lastWSLRunner(t *testing.T, deleteLogin bool) (*fakeHost, *fakeRunnerAPI, Operation, Plan) {
	t.Helper()
	f := &fakeHost{vm: true, wsl: true, entry: current(winEntry()), slots: map[string]*fakeSlot{
		"codex-1": done(hostName("codex-1"), "https://github.com/o/r"),
	}, volumes: map[string]bool{"codex": true}}
	fx := useRunnerHost(t, f)
	fx.gh.regs = []*fakeReg{reg(1, hostName("codex-1"), "github.com/o/r", false)}
	o := RunnerRemove(RunnerRemoveParams{Provider: "codex", DeleteLogin: deleteLogin})
	p, err := o.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return f, fx.gh, o, p
}

// items maps the read-back items of r by id.
func items(t *testing.T, r Result) map[string]Check {
	t.Helper()
	m := map[string]Check{}
	for _, c := range r[ResultItems].([]Check) {
		m[c.ID] = c
	}
	return m
}

const volumeFlake = `Error: read cli flags: connection "nibfin" not found`

// A failed volume read marks only the volume item; the autostart task and
// .wslconfig are still read and confirmed.
func TestRunnerRemoveReadBackVolumeFails(t *testing.T) {
	f, _, o, p := lastWSLRunner(t, true)
	f.existsExit, f.existsErr = 125, volumeFlake
	r, err := o.ReadBack(context.Background(), p)
	if Code(err) != CodePodmanFailed || !strings.Contains(err.Error(), volumeFlake) {
		t.Fatalf("ReadBack: %v", err)
	}
	it := items(t, r)
	if v := it["volume:nibfin-home-codex"]; v.Confirmed || !strings.Contains(v.Reason, "could not be read: podman_failed: podman volume exists nibfin-home-codex exited with status 125: "+volumeFlake) {
		t.Errorf("volume = %+v", v)
	}
	for _, id := range []string{"host", "runner:codex-1", "autostart", "wslconfig"} {
		if c, ok := it[id]; !ok || !c.Confirmed {
			t.Errorf("%s = %+v (%v)", id, c, ok)
		}
	}
	if a := it["autostart"]; a.Target != `task "Nibfin podman machine"` || a.State != "absent" {
		t.Errorf("autostart = %+v", a)
	}
	if l, _ := r["login"].(map[string]string); l["volume"] != "unknown" {
		t.Errorf("login = %v", l)
	}
}

// Without --delete-login the volume only fills login: its failed read is
// reported there and fails nothing.
func TestRunnerRemoveReadBackVolumeFailsWithoutDeleteLogin(t *testing.T) {
	f, _, o, p := lastWSLRunner(t, false)
	f.existsExit, f.existsErr = 125, volumeFlake
	r, err := o.ReadBack(context.Background(), p)
	if err != nil {
		t.Fatalf("ReadBack: %v", err)
	}
	it := items(t, r)
	if _, ok := it["volume:nibfin-home-codex"]; ok || len(it) != 4 {
		t.Errorf("items = %+v", it)
	}
	for id, c := range it {
		if !c.Confirmed {
			t.Errorf("%s = %+v", id, c)
		}
	}
	l, _ := r["login"].(map[string]string)
	if l["volume"] != "unknown" || !strings.HasPrefix(l["reason"], "could not be read: podman_failed: ") || !strings.Contains(l["reason"], volumeFlake) {
		t.Errorf("login = %v", l)
	}
}

// A failed autostart read marks only the autostart item; the volume and
// .wslconfig are still read and confirmed.
func TestRunnerRemoveReadBackAutostartFails(t *testing.T) {
	f, _, o, p := lastWSLRunner(t, true)
	f.entryErr = &hostErr{host.ErrAutostart}
	r, err := o.ReadBack(context.Background(), p)
	if Code(err) != CodeAutostartFailed {
		t.Fatalf("ReadBack: %v", err)
	}
	it := items(t, r)
	if a := it["autostart"]; a.Confirmed || !strings.HasPrefix(a.Reason, "could not be read: autostart_failed: ") {
		t.Errorf("autostart = %+v", a)
	}
	for _, id := range []string{"host", "runner:codex-1", "volume:nibfin-home-codex", "wslconfig"} {
		if c, ok := it[id]; !ok || !c.Confirmed {
			t.Errorf("%s = %+v (%v)", id, c, ok)
		}
	}
	if _, ok := r["autostart"]; ok {
		t.Errorf("autostart = %v", r["autostart"])
	}
	if l, _ := r["login"].(map[string]string); l["volume"] != "deleted" || f.volumes["codex"] {
		t.Errorf("login = %v", l)
	}
}

// A failed GitHub read marks only the slots of that repository; the
// volume, the autostart task and .wslconfig are still read.
func TestRunnerRemoveReadBackGitHubFails(t *testing.T) {
	_, gh, o, p := lastWSLRunner(t, true)
	gh.listErr = Errorf(CodeGitHubAPIFailed, "list runners: HTTP 502")
	r, err := o.ReadBack(context.Background(), p)
	if Code(err) != CodeGitHubAPIFailed {
		t.Fatalf("ReadBack: %v", err)
	}
	it := items(t, r)
	if s := it["runner:codex-1"]; s.Confirmed || s.Reason != "could not be read: github_api_failed: list runners: HTTP 502" {
		t.Errorf("slot = %+v", s)
	}
	for _, id := range []string{"host", "volume:nibfin-home-codex", "autostart", "wslconfig"} {
		if c, ok := it[id]; !ok || !c.Confirmed {
			t.Errorf("%s = %+v (%v)", id, c, ok)
		}
	}
}

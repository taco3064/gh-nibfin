package op

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/taco3064/gh-nibfin/internal/runners"
)

// fakeSlot is one slot on the fake runner host.
type fakeSlot struct {
	configured, unit, enabled, active bool
	name, url                         string
	// env is the NIBFIN_PROVIDER value of its .env.
	env string
	// hook is true when its .env names the job-started hook.
	hook bool
	// oldUnit marks a unit file that differs from unitText; job a running
	// Runner.Worker (see runner_upgrade_test.go).
	oldUnit, job bool
}

type shellCall struct {
	script string
	args   []string
}

const (
	slotDir = "/home/u/.local/share/nibfin/runners"
	unitDir = "/home/u/.config/systemd/user"
	// defaultProbe is a Linux host with every controller delegated.
	defaultProbe = "arch x86_64\nnode my.host_01\nmachine 0123abcd\nlinger 0\ncgroup cpuset cpu io memory pids\nmemkb 16000000\ntools 1 1\nicu 1\n"
)

var scriptNames = map[string]string{
	runners.ListScript:   "list",
	probeScript:          "probe",
	lingerScript:         "linger",
	libicuScript:         "libicu",
	extractScript:        "extract",
	configureScript:      "configure",
	unitScript:           "unit",
	unitDirScript:        "unitdir",
	envScript:            "env",
	envCheckScript:       "envcheck",
	stopScript:           "stop",
	purgeScript:          "purge",
	slotSpecScript:       "spec",
	slotUpgradeScript:    "upgrade",
	deployKeyReadScript:  "deploy-read",
	deployKeyWriteScript: "deploy-write",
	deployKeyCheckScript: "deploy-check",
	hookReadScript:       "hook-read",
	hookWriteScript:      "hook-write",
	hookEnvScript:        "hook-env",
	hookEnvCheckScript:   "hook-envcheck",
	hookPresentScript:    "hook-present",
	hookRemoveScript:     "hook-remove",
}

// shellHelper is the child of a fake Shell: it saves its stdin to
// HELPER_STDIN_SAVE and checks it against HELPER_STDIN_SHA when set, prints
// HELPER_STDOUT (base64) and HELPER_STDERR and exits with HELPER_EXIT.
func shellHelper() {
	if save := os.Getenv("HELPER_STDIN_SAVE"); save != "" {
		in, _ := io.ReadAll(os.Stdin)
		os.WriteFile(save, in, 0o600)
	}
	if want := os.Getenv("HELPER_STDIN_SHA"); want != "" {
		in, _ := io.ReadAll(os.Stdin)
		sum := sha256.Sum256(in)
		if hex.EncodeToString(sum[:]) != want {
			fmt.Fprint(os.Stderr, "unexpected stdin")
			os.Exit(3)
		}
	}
	out, _ := base64.StdEncoding.DecodeString(os.Getenv("HELPER_STDOUT"))
	os.Stdout.Write(out)
	fmt.Fprint(os.Stderr, os.Getenv("HELPER_STDERR"))
	exit, _ := strconv.Atoi(os.Getenv("HELPER_EXIT"))
	os.Exit(exit)
}

func (f *fakeHost) Local() bool { return !f.vm }
func (f *fakeHost) WSL() bool   { return f.wsl }

func shaHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (f *fakeHost) listing() string {
	b := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	out := "dirs " + b(slotDir) + " " + b(unitDir) + "\n"
	for name, s := range f.slots {
		r := "-"
		if s.configured {
			r = "b64:" + b(fmt.Sprintf(`{"AgentName":%q,"GitHubUrl":%q}`, s.name, s.url))
		}
		u, e, a := "0", "disabled", "inactive"
		if s.unit {
			u = "1"
		}
		if s.enabled {
			e = "enabled"
		}
		if s.active {
			a = "active"
		}
		out += fmt.Sprintf("slot %s %s %s %s %s\n", name, r, u, e, a)
	}
	return out
}

func flag(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// Shell simulates the runner-host scripts. Their effect on the fake host
// happens when the command is built.
func (f *fakeHost) Shell(ctx context.Context, script string, args ...string) *exec.Cmd {
	name, ok := scriptNames[script]
	if !ok {
		name = "unknown"
	}
	f.shells = append(f.shells, shellCall{name, args})
	stdout, stdinSHA, stdinSave := "", "", ""
	exit := f.shellExit[name]
	slot := ""
	if len(args) > 0 {
		slot = strings.TrimPrefix(args[0], slotDir+"/")
	}
	switch name {
	case "list":
		stdout = f.listing()
	case "probe":
		stdout = f.probe
		if stdout == "" {
			stdout = defaultProbe
		}
		if f.icu {
			stdout = strings.Replace(stdout, "icu 0", "icu 1", 1)
		}
	case "libicu":
		f.events = append(f.events, "libicu")
		if exit == 0 {
			f.icu = true
		}
	case "linger":
		f.events = append(f.events, "linger")
		f.linger = true
	case "extract":
		f.events = append(f.events, "extract "+slot)
		stdinSHA = f.archiveSHA
		if _, has := f.slots[slot]; has && args[1] == "new" {
			exit = 1
		}
		if f.slots[slot] == nil {
			f.slots[slot] = &fakeSlot{}
		}
	case "configure":
		f.events = append(f.events, "configure "+slot)
		stdinSHA = shaHex(f.token + "\n")
		if exit == 0 {
			s := f.slots[slot]
			s.configured, s.name, s.url = true, flag(args, "--name"), flag(args, "--url")
		}
	case "unitdir":
		f.events = append(f.events, "unitdir "+args[0])
	case "env":
		f.events = append(f.events, "env "+slot+" "+args[1])
		if s := f.slots[slot]; exit == 0 && s != nil {
			s.env = args[1]
		}
	case "envcheck":
		stdout = "no\n"
		if s := f.slots[slot]; s != nil && s.env == args[1] {
			stdout = "yes\n"
		}
	case "unit":
		f.events = append(f.events, "unit "+args[1])
		if exit == 0 {
			s := f.slots[strings.TrimSuffix(strings.TrimPrefix(args[1], "nibfin-runner-"), ".service")]
			s.unit, s.enabled, s.active = true, true, true
		}
	case "stop":
		if e, ok := f.stopExit[slot]; ok {
			exit = e
		}
		f.events = append(f.events, "stop "+slot+" "+args[3])
		if s := f.slots[slot]; exit == 0 && s != nil {
			s.enabled, s.active = false, false
		}
	case "spec", "upgrade":
		stdout, stdinSHA = f.upgradeShell(name, slot, args, exit)
	case "deploy-read":
		stdout = f.deployPub
	case "deploy-write":
		f.events = append(f.events, "deploy-key "+args[0])
		stdinSave = f.deployPriv
		if exit == 0 {
			f.deployPub, f.deployMode = args[1]+"\n", "600"
		}
	case "deploy-check":
		if f.deployPub != "" {
			stdout = "mode " + f.deployMode + "\n" + f.deployPub
		}
	case "hook-read", "hook-write", "hook-env", "hook-envcheck", "hook-present", "hook-remove":
		stdout, stdinSave = f.hookShell(name, slot, args, exit)
	case "purge":
		f.events = append(f.events, "purge "+args[1])
		if exit == 0 && !f.purgeNoop {
			delete(f.slots, args[1])
		}
	}
	if out, ok := f.shellOut[name]; ok {
		stdout = out
	}
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoginHelper$")
	c.Env = append(os.Environ(), "GO_WANT_LOGIN_HELPER=1", "HELPER_MODE=shell",
		"HELPER_STDOUT="+base64.StdEncoding.EncodeToString([]byte(stdout)),
		"HELPER_STDERR="+f.shellErr[name], "HELPER_EXIT="+strconv.Itoa(exit), "HELPER_STDIN_SHA="+stdinSHA, "HELPER_STDIN_SAVE="+stdinSave)
	return c
}

type fakeRunnerAPI struct {
	host      *fakeHost
	user      int64
	downloads []runnerDownload
	tokens    int
	unlisted  bool
	// listErr fails ListRunners; busy marks runners busy by name.
	listErr error
	busy    map[string]bool
	err     error
	// regs, when set, are the registrations instead of one per configured
	// slot (see runner_remove_test.go).
	regs      []*fakeReg
	lists     map[string]int
	keepRegs  bool
	onStrip   func()
	deleteErr error
	// setErr fails SetCustomLabels (see runner_upgrade_test.go).
	setErr error

	// The request route (see runner_request_test.go).
	login    string
	perms    *repoPerms
	permsErr error
	req      *fakeRequest
}

func (g *fakeRunnerAPI) User(context.Context) (ghUser, error) {
	login := g.login
	if login == "" {
		login = "alice"
	}
	return ghUser{ID: g.user, Login: login}, g.err
}
func (g *fakeRunnerAPI) RepoPermissions(context.Context, string) (repoPerms, error) {
	if g.perms == nil {
		return repoPerms{Admin: true, Push: true}, g.permsErr
	}
	return *g.perms, g.permsErr
}
func (g *fakeRunnerAPI) Downloads(_ context.Context, repo string) ([]runnerDownload, error) {
	return g.downloads, nil
}
func (g *fakeRunnerAPI) RegistrationToken(_ context.Context, repo string) (string, error) {
	g.tokens++
	g.host.events = append(g.host.events, "token")
	return g.host.token, nil
}
func (g *fakeRunnerAPI) ListRunners(_ context.Context, repo string) ([]ghRunner, error) {
	if g.listErr != nil {
		return nil, g.listErr
	}
	var out []ghRunner
	if g.regs != nil {
		return g.listRegs(repo), nil
	}
	if g.unlisted {
		return out, nil
	}
	for _, s := range g.host.slots {
		if s.configured {
			out = append(out, ghRunner{Name: strings.ToUpper(s.name), Status: "online", Busy: g.busy[s.name]})
		}
	}
	return out, nil
}

const archive = "fake runner archive"

type addFixture struct {
	host *fakeHost
	gh   *fakeRunnerAPI
	auth *[]string
	// hosts records the hosts newGitHub was asked for.
	hosts *[]string
}

// useRunnerHost installs the fakes: the host, GitHub, a TLS server serving
// the archive, and the current repository github.com/cur/repo.
func useRunnerHost(t *testing.T, f *fakeHost) addFixture {
	t.Helper()
	if f.token == "" {
		f.token = "AABBREGTOKEN"
	}
	f.archiveSHA = shaHex(archive)
	useHost(t, f)
	auth := &[]string{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*auth = append(*auth, r.Header.Get("Authorization"))
		io.WriteString(w, archive)
	}))
	t.Cleanup(srv.Close)
	gh := &fakeRunnerAPI{host: f, user: 42, downloads: []runnerDownload{
		{OS: "osx", Architecture: "x64", DownloadURL: srv.URL + "/osx", SHA256: shaHex("other")},
		{OS: "linux", Architecture: "x64", DownloadURL: srv.URL + "/runner.tar.gz", Filename: "runner.tar.gz", SHA256: shaHex(archive)},
	}}
	hosts := &[]string{}
	oldGH, oldHTTP, oldRepo := newGitHub, httpClient, currentRepo
	t.Cleanup(func() { newGitHub, httpClient, currentRepo = oldGH, oldHTTP, oldRepo })
	newGitHub = func(host string) (github, error) { *hosts = append(*hosts, host); return gh, nil }
	httpClient = srv.Client()
	currentRepo = func() (string, error) { return "github.com/cur/repo", nil }
	tmp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}
	return addFixture{f, gh, auth, hosts}
}

type yes struct{ plans []Plan }

func (y *yes) Confirm(p Plan) (bool, error) { y.plans = append(y.plans, p); return true, nil }

func TestRunnerAddFirstRunners(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, loginWorks: true}
	fx := useRunnerHost(t, f)
	in := &recInteract{}
	c := &yes{}
	p, r, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2, Interact: in}), c)
	if err != nil {
		t.Fatalf("Run: %v (events %v)", err, f.events)
	}
	want := []string{"enable linger fake", "create volume nibfin-tools", "create volume nibfin-home-claude", "log in claude", "add runner claude-1", "add runner claude-2"}
	if !reflect.DeepEqual(steps(p), want) || !p.Confirm || p.Repo != "github.com/cur/repo" || len(c.plans) != 1 {
		t.Fatalf("plan = %v, %+v", steps(p), p)
	}
	if d := p.Changes()[4].Detail; d != "github.com/cur/repo, labels nibfin,nibfin-42, .env NIBFIN_PROVIDER=claude" {
		t.Errorf("detail = %q", d)
	}
	wantEvents := []string{"linger", "create volume nibfin-tools", "create volume nibfin-home-claude", "log in claude", "token",
		"extract claude-1", "configure claude-1", "env claude-1 claude", "unit nibfin-runner-claude-1.service",
		"extract claude-2", "configure claude-2", "env claude-2 claude", "unit nibfin-runner-claude-2.service"}
	if !reflect.DeepEqual(f.events, wantEvents) {
		t.Errorf("events = %v", f.events)
	}
	if fx.gh.tokens != 1 || !reflect.DeepEqual(*fx.auth, []string{""}) {
		t.Errorf("tokens %d, download auth %q", fx.gh.tokens, *fx.auth)
	}
	for _, h := range *fx.hosts {
		if h != "github.com" {
			t.Errorf("gh host %q", h)
		}
	}
	var configure []string
	for _, s := range f.shells {
		for _, a := range s.args {
			if strings.Contains(a, f.token) {
				t.Errorf("token in the argv of %s: %q", s.script, s.args)
			}
		}
		if s.script == "configure" && configure == nil {
			configure = s.args
		}
		if s.script == "extract" && s.args[1] != "new" {
			t.Errorf("extract mode = %q", s.args)
		}
	}
	name := "my-host-01-" + shaHex("gh-nibfin runner\n0123abcd")[:8] + "-claude-1"
	wantConf := []string{slotDir + "/claude-1", "--unattended", "--url", "https://github.com/cur/repo", "--name", name, "--labels", "nibfin,nibfin-42", "--replace"}
	if !reflect.DeepEqual(configure, wantConf) {
		t.Errorf("configure = %q\nwant %q", configure, wantConf)
	}
	b, _ := json.Marshal(r)
	var got struct {
		Host, Repo string
		Runners    []map[string]any
		Login      map[string]map[string]bool
		Advice     []string
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Host != "fake" || got.Repo != "github.com/cur/repo" || len(got.Runners) != 2 || !got.Login["claude"]["loggedIn"] || got.Advice == nil || len(got.Advice) != 0 {
		t.Errorf("result = %s", b)
	}
	if r0 := got.Runners[0]; r0["slot"] != "claude-1" || r0["status"] != "online" || r0["active"] != true || r0["listed"] != true || r0["repo"] != "github.com/cur/repo" {
		t.Errorf("runner = %v", r0)
	}
	if strings.Contains(string(b), f.token) {
		t.Error("result has the token")
	}
	if left, _ := filepath.Glob(filepath.Join(os.TempDir(), "nibfin-runner-*")); len(left) != 0 {
		t.Errorf("archive left behind: %v", left)
	}
}

func TestRunnerAddPlanNoLoginNoLinger(t *testing.T) {
	f := &fakeHost{probe: strings.Replace(defaultProbe, "linger 0", "linger 1", 1),
		volumes: map[string]bool{"nibfin-tools": true, "claude": true}, loggedIn: map[string]bool{"claude": true}}
	f.slots = map[string]*fakeSlot{
		"claude-1": {configured: true, unit: true, enabled: true, name: "a", url: "https://ghe.example/o/r"},
		"claude-3": {configured: true, name: "b", url: "https://ghe.example/o/r"},
		"codex-7":  {configured: true, unit: true, enabled: true, name: "c", url: "https://ghe.example/O/R"},
	}
	fx := useRunnerHost(t, f)
	fx.gh.user = 9
	p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}).Plan(context.Background())
	want := []string{"add runner claude-3", "add runner claude-4"}
	if err != nil || !reflect.DeepEqual(steps(p), want) || p.Repo != "ghe.example/o/r" {
		t.Fatalf("plan = %v, %+v, %v", steps(p), p, err)
	}
	if (*fx.hosts)[0] != "ghe.example" {
		t.Errorf("gh host = %v", *fx.hosts)
	}
	for _, e := range f.events {
		t.Errorf("plan changed something: %s", e)
	}

	// Only claude-3 is needed: it has .runner, so nothing is downloaded and
	// no token is fetched; its unit is written.
	f.events = nil
	_, _, err = Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil || !reflect.DeepEqual(f.events, []string{"env claude-3 claude", "unit nibfin-runner-claude-3.service"}) || fx.gh.tokens != 0 || len(*fx.auth) != 0 {
		t.Errorf("Run: %v, events %v, tokens %d", err, f.events, fx.gh.tokens)
	}
	for _, s := range f.shells {
		if s.script == "unit" && !reflect.DeepEqual(s.args, []string{unitDir, "nibfin-runner-claude-3.service"}) {
			t.Errorf("unit args = %q", s.args)
		}
	}
}

func TestRunnerAddRepoRules(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{
		"claude-1": {configured: true, unit: true, enabled: true, name: "a", url: "https://github.com/o/one"},
		"codex-1":  {configured: true, unit: true, enabled: true, name: "b", url: "https://github.com/o/two"},
	}}
	useRunnerHost(t, f)
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerRepoConflict || !strings.Contains(err.Error(), "github.com/o/one") || !strings.Contains(err.Error(), "github.com/o/two") || len(f.events) != 0 {
		t.Errorf("conflict: %v, events %v", err, f.events)
	}

	f = &fakeHost{slots: map[string]*fakeSlot{}}
	useRunnerHost(t, f)
	currentRepo = func() (string, error) { return "", Errorf(CodeRepoUnresolved, "not a repository") }
	if _, err := RunnerAdd(RunnerAddParams{Provider: "codex", Count: 1}).Plan(context.Background()); Code(err) != CodeRepoUnresolved {
		t.Errorf("unresolved: %v", err)
	}

	// Later runners use the runners' repository, not the current one.
	f = &fakeHost{slots: map[string]*fakeSlot{"codex-1": {configured: true, unit: true, enabled: true, name: "b", url: "https://github.com/o/two"}}}
	useRunnerHost(t, f)
	currentRepo = func() (string, error) { t.Error("resolved the current repository"); return "", nil }
	p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
	if err != nil || p.Repo != "github.com/o/two" {
		t.Errorf("reuse: %+v, %v", p, err)
	}

	// A plan for another repository is refused at Apply.
	p.Repo = "github.com/evil/repo"
	if err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Apply(context.Background(), p); Code(err) != CodeRunnerRepoConflict {
		t.Errorf("stale plan: %v", err)
	}
}

func TestRunnerAddInvalid(t *testing.T) {
	useRunnerHost(t, &fakeHost{})
	for _, p := range []RunnerAddParams{{Provider: "gemini", Count: 1}, {Provider: "claude", Count: 0}} {
		if _, err := RunnerAdd(p).Plan(context.Background()); Code(err) != CodeInvalidArgument {
			t.Errorf("%+v: %v", p, err)
		}
	}
	// Apply refuses steps it did not plan.
	f := &fakeHost{}
	useRunnerHost(t, f)
	p := Plan{Repo: "github.com/o/r", Steps: []Step{{Action: actAddRunner, Target: "codex-1"}}}
	if err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Apply(context.Background(), p); Code(err) != CodeInvalidArgument {
		t.Errorf("foreign slot: %v", err)
	}
}

func TestRunnerAddStoppedMachine(t *testing.T) {
	f := &fakeHost{stopped: true, vm: true, slots: map[string]*fakeSlot{}, loginWorks: true,
		volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	fx := useRunnerHost(t, f)
	o := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2, Interact: &recInteract{}})
	p, err := o.Plan(context.Background())
	want := []string{"start fake", "enable linger fake", "install libicu fake", "create volume nibfin-tools", "create volume nibfin-home-claude", "log in claude", "add runners claude ×2"}
	if err != nil || !reflect.DeepEqual(steps(p), want) {
		t.Fatalf("plan = %v, %v", steps(p), err)
	}
	if len(f.calls) != 0 || len(f.shells) != 0 {
		t.Errorf("plan touched a stopped machine: %v %v", f.calls, f.shells)
	}
	if err := o.Apply(context.Background(), p); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// After the start: logged in already, so no login; two new slots.
	wantEvents := []string{"start via autostart", "linger", "create volume nibfin-tools", "token",
		"extract claude-1", "configure claude-1", "env claude-1 claude", "unitdir " + unitDir, "unit nibfin-runner-claude-1.service",
		"extract claude-2", "configure claude-2", "env claude-2 claude", "unitdir " + unitDir, "unit nibfin-runner-claude-2.service"}
	if f.autoStarts != 1 || f.starts != 0 || !reflect.DeepEqual(f.events, wantEvents) || fx.gh.tokens != 1 {
		t.Errorf("starts %d %d, events %v", f.autoStarts, f.starts, f.events)
	}
	if _, err := o.ReadBack(context.Background(), p); err != nil {
		t.Errorf("ReadBack: %v", err)
	}
}

// TestRunnerAddMachineLibicu covers the libicu the official runner needs in
// a podman machine VM (#511): a VM without it gets the install before any
// configure, one with it gets no change, a failed install fails the item
// with its cause and configures nothing, and a local Linux host never gets
// the item.
func TestRunnerAddMachineLibicu(t *testing.T) {
	noICU := strings.Replace(strings.Replace(defaultProbe, "linger 0", "linger 1", 1), "icu 1", "icu 0", 1)
	newHost := func(probe string) *fakeHost {
		return &fakeHost{vm: true, probe: probe, slots: map[string]*fakeSlot{},
			volumes: map[string]bool{"nibfin-tools": true, "claude": true}, loggedIn: map[string]bool{"claude": true}}
	}

	t.Run("missing", func(t *testing.T) {
		f := newHost(noICU)
		useRunnerHost(t, f)
		o := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2})
		p, err := o.Plan(context.Background())
		want := []string{"install libicu fake", "add runner claude-1", "add runner claude-2"}
		if err != nil || !reflect.DeepEqual(steps(p), want) {
			t.Fatalf("plan = %v, %v", steps(p), err)
		}
		for _, s := range p.Steps {
			if s.Action == actInstallLibicu && (s.Current != "absent" || s.Next != "present") {
				t.Errorf("libicu step = %+v", s)
			}
		}
		_, r, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}), &yes{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		wantEvents := []string{"libicu", "token",
			"extract claude-1", "configure claude-1", "env claude-1 claude", "unitdir " + unitDir, "unit nibfin-runner-claude-1.service",
			"extract claude-2", "configure claude-2", "env claude-2 claude", "unitdir " + unitDir, "unit nibfin-runner-claude-2.service"}
		if !reflect.DeepEqual(f.events, wantEvents) {
			t.Errorf("events = %v", f.events)
		}
		if b, _ := json.Marshal(r); !strings.Contains(string(b), "install libicu") {
			t.Errorf("read-back has no libicu item: %s", b)
		}
	})

	t.Run("present", func(t *testing.T) {
		f := newHost(strings.Replace(defaultProbe, "linger 0", "linger 1", 1))
		useRunnerHost(t, f)
		p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
		if err != nil || !reflect.DeepEqual(steps(p), []string{"add runner claude-1"}) {
			t.Fatalf("plan = %v, %v", steps(p), err)
		}
		found := false
		for _, s := range p.Steps {
			if s.Action == actInstallLibicu {
				found = s.NoChange && s.Current == "present"
			}
		}
		if !found {
			t.Errorf("no no-change libicu item: %v", allSteps(p))
		}
		if _, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, c := range f.shells {
			if c.script == "libicu" {
				t.Errorf("libicu script ran on a VM with libicu")
			}
		}
	})

	t.Run("failed", func(t *testing.T) {
		f := newHost(noICU)
		f.shellExit = map[string]int{"libicu": 1}
		f.shellErr = map[string]string{"libicu": "sudo: a password is required"}
		f.shellOut = map[string]string{"libicu": "Last metadata expiration check\n"}
		useRunnerHost(t, f)
		_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		msg := fmt.Sprint(err)
		if Code(err) != CodeRunnerInstallFailed || !strings.Contains(msg, "install libicu on fake") ||
			!strings.Contains(msg, "exit status 1") || !strings.Contains(msg, "stderr: sudo: a password is required") ||
			!strings.Contains(msg, "stdout: Last metadata expiration check") ||
			!strings.Contains(msg, "sudo dnf install -y libicu") {
			t.Fatalf("err = %v", err)
		}
		if !reflect.DeepEqual(f.events, []string{"libicu"}) {
			t.Errorf("events after a failed libicu step = %v", f.events)
		}
	})

	t.Run("local", func(t *testing.T) {
		f := &fakeHost{probe: strings.Replace(defaultProbe, "icu 1", "icu 0", 1), slots: map[string]*fakeSlot{},
			volumes: map[string]bool{"nibfin-tools": true, "claude": true}, loggedIn: map[string]bool{"claude": true}}
		useRunnerHost(t, f)
		p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range p.Steps {
			if s.Action == actInstallLibicu {
				t.Errorf("libicu item on a local host: %v", allSteps(p))
			}
		}
		// Apply refuses the item on a local host.
		bad := Plan{Repo: "github.com/cur/repo", Steps: []Step{libicuStep("local podman")}}
		if err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Apply(context.Background(), bad); Code(err) != CodeInvalidArgument || !strings.Contains(fmt.Sprint(err), `unknown runner add step "install libicu"`) {
			t.Errorf("local libicu step: %v", err)
		}
	})
}

// TestRunnerAddMachineUnitDir covers a podman machine whose unit directory
// is root-owned (#387): the unit directory step runs before each unit, a
// refused step fails the slot before the unit, and a rerun completes the
// half-installed slot with only that step and its unit.
func TestRunnerAddMachineUnitDir(t *testing.T) {
	f := &fakeHost{vm: true, probe: strings.Replace(defaultProbe, "linger 0", "linger 1", 1),
		slots: map[string]*fakeSlot{}, volumes: map[string]bool{"nibfin-tools": true, "claude": true}, loggedIn: map[string]bool{"claude": true}}
	f.shellExit = map[string]int{"unitdir": 1}
	f.shellErr = map[string]string{"unitdir": "sudo: a password is required"}
	fx := useRunnerHost(t, f)
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}), &yes{})
	if Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "a password is required") ||
		!strings.Contains(err.Error(), "sudo chown $(id -u):$(id -g) "+unitDir+" "+unitDir+"/default.target.wants") {
		t.Fatalf("refused: %v", err)
	}
	want := []string{"token", "extract claude-1", "configure claude-1", "env claude-1 claude", "unitdir " + unitDir}
	if !reflect.DeepEqual(f.events, want) {
		t.Errorf("events = %v", f.events)
	}

	// The rerun picks the configured claude-1 first: no download, only the
	// .env, the unit directory step and its unit, then the new claude-2.
	f.shellExit, f.shellErr, f.events, fx.gh.tokens = nil, nil, nil, 0
	_, _, err = Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}), &yes{})
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	want = []string{"token", "env claude-1 claude", "unitdir " + unitDir, "unit nibfin-runner-claude-1.service",
		"extract claude-2", "configure claude-2", "env claude-2 claude", "unitdir " + unitDir, "unit nibfin-runner-claude-2.service"}
	if !reflect.DeepEqual(f.events, want) || fx.gh.tokens != 1 {
		t.Errorf("rerun events = %v, tokens %d", f.events, fx.gh.tokens)
	}
	for _, s := range f.shells {
		if s.script == "unitdir" && !reflect.DeepEqual(s.args, []string{unitDir}) {
			t.Errorf("unitdir args = %q", s.args)
		}
	}

	// Only the half-installed slot: no download, no token.
	f.slots["claude-3"] = &fakeSlot{configured: true, name: "c", url: "https://github.com/cur/repo"}
	f.events, fx.gh.tokens = nil, 0
	downloads := len(*fx.auth)
	_, _, err = Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	want = []string{"env claude-3 claude", "unitdir " + unitDir, "unit nibfin-runner-claude-3.service"}
	if err != nil || !reflect.DeepEqual(f.events, want) || fx.gh.tokens != 0 || len(*fx.auth) != downloads {
		t.Errorf("incomplete only: %v, events %v, tokens %d", err, f.events, fx.gh.tokens)
	}
}

func TestRunnerAddChecksumMismatch(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, loggedIn: map[string]bool{"claude": true}, volumes: map[string]bool{"claude": true}}
	fx := useRunnerHost(t, f)
	fx.gh.downloads[1].SHA256 = shaHex("tampered")
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerDownloadFailed || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("err = %v", err)
	}
	for _, e := range f.events {
		if strings.HasPrefix(e, "extract") || e == "token" {
			t.Errorf("went on after a mismatch: %v", f.events)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(os.TempDir(), "nibfin-runner-*")); len(left) != 0 {
		t.Errorf("archive left behind: %v", left)
	}
}

func TestRunnerAddScrubsToken(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, loggedIn: map[string]bool{"claude": true}, volumes: map[string]bool{"claude": true}, token: "SECRETTOKEN"}
	f.shellExit = map[string]int{"configure": 1}
	f.shellErr = map[string]string{"configure": "Http response code: Forbidden for token SECRETTOKEN"}
	useRunnerHost(t, f)
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerInstallFailed || strings.Contains(err.Error(), "SECRETTOKEN") || !strings.Contains(err.Error(), "Forbidden") {
		t.Errorf("err = %v", err)
	}
}

// configureFailure runs runner add on a podman machine whose configure
// script exits 1 with the given stderr and stdout, and returns the error.
func configureFailure(t *testing.T, stderr, stdout string) error {
	t.Helper()
	f := &fakeHost{vm: true, probe: strings.Replace(defaultProbe, "linger 0", "linger 1", 1),
		slots: map[string]*fakeSlot{}, volumes: map[string]bool{"nibfin-tools": true, "claude": true},
		loggedIn: map[string]bool{"claude": true}, token: "SECRETTOKEN"}
	f.shellExit = map[string]int{"configure": 1}
	f.shellErr = map[string]string{"configure": stderr}
	f.shellOut = map[string]string{"configure": stdout}
	useRunnerHost(t, f)
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerInstallFailed {
		t.Fatalf("code = %q, err = %v", Code(err), err)
	}
	return err
}

func TestRunnerAddConfigureKeepsStderr(t *testing.T) {
	err := configureFailure(t, "Http response code: NotFound\n", "")
	msg := err.Error()
	for _, want := range []string{"configure claude-1 on ", "exit status 1", "stderr: Http response code: NotFound"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in %q", want, msg)
		}
	}
	if strings.Contains(msg, "stdout:") {
		t.Errorf("empty stdout shown: %q", msg)
	}
}

func TestRunnerAddConfigureKeepsStdout(t *testing.T) {
	err := configureFailure(t, "", "Must not run with sudo\n")
	msg := err.Error()
	for _, want := range []string{"configure claude-1 on ", "exit status 1", "stdout: Must not run with sudo"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in %q", want, msg)
		}
	}
	if strings.Contains(msg, "stderr:") {
		t.Errorf("empty stderr shown: %q", msg)
	}
}

func TestRunnerAddConfigureMasksTokenInOutput(t *testing.T) {
	err := configureFailure(t, "denied for SECRETTOKEN\n", "token=SECRETTOKEN rejected\n")
	b, jerr := json.Marshal(err)
	if jerr != nil {
		t.Fatal(jerr)
	}
	for _, s := range []string{err.Error(), string(b)} {
		if strings.Contains(s, "SECRETTOKEN") || !strings.Contains(s, "stderr: denied for [token]") ||
			!strings.Contains(s, "stdout: token=[token] rejected") {
			t.Errorf("not masked: %s", s)
		}
	}
}

func TestRunnerAddConfigureKeepsOutputTail(t *testing.T) {
	var out strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&out, "line %03d %s\n", i, strings.Repeat("x", 40))
	}
	msg := configureFailure(t, "", out.String()).Error()
	_, tail, ok := strings.Cut(msg, "stdout: ")
	if !ok {
		t.Fatalf("no stdout in %q", msg)
	}
	if !strings.HasPrefix(tail, childCutMark) || !strings.Contains(tail, "line 200 ") || strings.Contains(tail, "line 001 ") ||
		strings.Contains(tail, "line 180 ") || strings.Count(tail, "line ") > childTailLines {
		t.Errorf("tail = %q", tail)
	}
	if len(tail) > childTailBytes+len(childCutMark) {
		t.Errorf("tail is %d bytes", len(tail))
	}
}

func TestOutputTailBounds(t *testing.T) {
	if got := outputTail(" \n\n"); got != "" {
		t.Errorf("blank = %q", got)
	}
	if got := outputTail("a\r\nb\n"); got != "a | b" {
		t.Errorf("short = %q", got)
	}
	long := strings.Repeat("é", childTailBytes) + "END"
	got := outputTail(long)
	if !strings.HasPrefix(got, childCutMark) || !strings.HasSuffix(got, "END") || !utf8.ValidString(got) ||
		len(got) > childTailBytes+len(childCutMark) {
		t.Errorf("long line kept %d bytes: valid=%v", len(got), utf8.ValidString(got))
	}
	many := strings.Repeat(strings.Repeat("y", 300)+"\n", 10)
	if got := outputTail(many); !strings.HasPrefix(got, childCutMark) || len(got) > childTailBytes+len(childCutMark) {
		t.Errorf("bytes cap: %d", len(got))
	}
}

func TestRunnerAddStdinReachesScripts(t *testing.T) {
	// A wrong archive on stdin makes the fake extract exit 3.
	f := &fakeHost{slots: map[string]*fakeSlot{}, loggedIn: map[string]bool{"claude": true}, volumes: map[string]bool{"claude": true}}
	useRunnerHost(t, f)
	f.archiveSHA = shaHex("something else")
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "unexpected stdin") {
		t.Errorf("err = %v", err)
	}
}

func TestRunnerAddSSHFailure(t *testing.T) {
	f := &fakeHost{vm: true, slots: map[string]*fakeSlot{}}
	f.shellExit = map[string]int{"probe": 125}
	useRunnerHost(t, f)
	if _, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background()); Code(err) != CodePodmanFailed {
		t.Errorf("err = %v", err)
	}
	f.shellExit = map[string]int{"list": 255}
	if _, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background()); Code(err) != CodePodmanFailed {
		t.Errorf("list: %v", err)
	}
}

func TestRunnerAddGitHubError(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}}
	fx := useRunnerHost(t, f)
	fx.gh.err = Errorf(CodeGitHubAPIFailed, "HTTP 401")
	if _, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background()); Code(err) != CodeGitHubAPIFailed {
		t.Errorf("err = %v", err)
	}
}

func TestRunnerAddAdvice(t *testing.T) {
	f := &fakeHost{probe: strings.Replace(defaultProbe, "cgroup cpuset cpu io memory pids", "cgroup memory pids", 1)}
	useRunnerHost(t, f)
	p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	last := p.Notes[len(p.Notes)-1]
	if !strings.Contains(last, "cpu are not delegated") || !strings.Contains(last, "Delegate=cpu cpuset io memory pids") || !strings.Contains(last, "NIBFIN_CONTAINER_LIMITS=off") {
		t.Errorf("advice = %+v", last)
	}

	f = &fakeHost{vm: true, wsl: true, probe: strings.Replace(defaultProbe, "memkb 16000000", "memkb 8000000", 1)}
	useRunnerHost(t, f)
	p, err = RunnerAdd(RunnerAddParams{Provider: "codex", Count: 2}).Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	advice := p.Notes
	// Two existing runners and two new ones: 12 GB.
	if len(advice) != 2 || !strings.Contains(advice[0], "NIBFIN_CONTAINER_LIMITS=off") ||
		!strings.Contains(advice[1], `%UserProfile%\.wslconfig, leaving at least 12 GB for 4 runners`) ||
		!strings.Contains(advice[1], "the VM has 7.6 GB, below that minimum") || !strings.Contains(advice[1], "wsl --shutdown") {
		t.Errorf("advice = %q", advice)
	}

	// No advice on a non-WSL machine.
	f = &fakeHost{vm: true, probe: "arch aarch64\nnode n\nmachine ab\nlinger 1\ncgroup ?\nmemkb 1\n"}
	useRunnerHost(t, f)
	p, _ = RunnerAdd(RunnerAddParams{Provider: "codex", Count: 1}).Plan(context.Background())
	if len(p.Notes) != 0 {
		t.Errorf("advice on applehv: %v", p.Notes)
	}
}

func TestRunnerAddReadBackMissing(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, loggedIn: map[string]bool{"claude": true}, volumes: map[string]bool{"claude": true}}
	fx := useRunnerHost(t, f)
	fx.gh.unlisted = true
	_, r, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerMissing || !strings.Contains(err.Error(), "claude-1") || r == nil {
		t.Errorf("unlisted: %v", err)
	}
	// A planned slot that does not exist is missing too.
	o := RunnerAdd(RunnerAddParams{Provider: "codex", Count: 1})
	_, err = o.ReadBack(context.Background(), Plan{Steps: []Step{{Action: actAddRunner, Target: "codex-9"}}})
	if Code(err) != CodeRunnerMissing || !strings.Contains(err.Error(), "codex-9") {
		t.Errorf("absent: %v", err)
	}
}

func TestRunnerAddArch(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, probe: strings.Replace(defaultProbe, "x86_64", "riscv64", 1), loggedIn: map[string]bool{"claude": true}, volumes: map[string]bool{"claude": true}}
	useRunnerHost(t, f)
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeUnsupportedHost {
		t.Errorf("err = %v", err)
	}
}

func TestRunnerName(t *testing.T) {
	pr := hostProbe{node: "..weird.Host_name.." + strings.Repeat("x", 40), machine: "abc"}
	n, err := pr.runnerName("codex-3")
	want := "weird-Host-name-xxxxxxxxxxxxxxxx-" + shaHex("gh-nibfin runner\nabc")[:8] + "-codex-3"
	if err != nil || n != want {
		t.Errorf("name = %q, want %q (%v)", n, want, err)
	}
	if _, err := (hostProbe{node: "x"}).runnerName("claude-1"); Code(err) != CodeRunnerInstallFailed {
		t.Errorf("no machine-id: %v", err)
	}
}

func TestUnitText(t *testing.T) {
	u, err := unitText("claude-2", "/home/u/.local/share/nibfin/runners/claude-2")
	want := "[Unit]\nDescription=Nibfin GitHub Actions runner claude-2\nAfter=network.target\n\n" +
		"[Service]\nExecStart=/home/u/.local/share/nibfin/runners/claude-2/runsvc.sh\nWorkingDirectory=/home/u/.local/share/nibfin/runners/claude-2\n" +
		"KillMode=process\nKillSignal=SIGTERM\nTimeoutStopSec=5min\n\n[Install]\nWantedBy=default.target\n"
	if err != nil || u != want {
		t.Errorf("unit = %q, %v", u, err)
	}
	if u, _ := unitText("c-1", "/a%b/c-1"); !strings.Contains(u, "ExecStart=/a%%b/c-1/runsvc.sh") {
		t.Errorf("escape: %q", u)
	}
	for _, d := range []string{"/home/a b/x", "/x\ny", `/x"y`} {
		if _, err := unitText("c-1", d); Code(err) != CodeRunnerInstallFailed {
			t.Errorf("%q accepted", d)
		}
	}
}

func TestRunnerAddParamsJSON(t *testing.T) {
	b, err := json.Marshal(RunnerAddParams{Provider: "claude", Count: 2, Terminal: &Terminal{}, Interact: &recInteract{}})
	if err != nil || string(b) != `{"provider":"claude","count":2}` {
		t.Errorf("json = %s, %v", b, err)
	}
}

func TestDownloadArchive(t *testing.T) {
	tmp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, archive)
	}))
	defer srv.Close()
	old := httpClient
	t.Cleanup(func() { httpClient = old })
	httpClient = srv.Client()
	path, cleanup, err := downloadArchive(context.Background(), runnerDownload{DownloadURL: srv.URL + "/a", SHA256: strings.ToUpper(shaHex(archive))})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != archive {
		t.Errorf("content = %q", b)
	}
	cleanup()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("not removed: %v", err)
	}
	for _, d := range []runnerDownload{
		{DownloadURL: "http://example.com/a", SHA256: shaHex(archive)},
		{DownloadURL: srv.URL + "/a", SHA256: ""},
		{DownloadURL: srv.URL + "/missing", SHA256: shaHex(archive)},
	} {
		if _, _, err := downloadArchive(context.Background(), d); Code(err) != CodeRunnerDownloadFailed {
			t.Errorf("%+v: %v", d, err)
		}
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

func TestRepoPath(t *testing.T) {
	if repoPath("ghe.example/o/r") != "repos/o/r" || repoHost("ghe.example/o/r") != "ghe.example" {
		t.Error("repo path")
	}
}

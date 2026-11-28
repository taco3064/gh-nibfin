package op

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/host"
)

// Observed output of `claude auth login` with no TTY: the prompt has no
// trailing newline.
const claudeFixture = "Opening browser to sign in…\nIf the browser didn't open, visit:\nhttps://claude.ai/oauth/authorize?code=true&client_id=abc&state=xyz\nPaste code here if prompted > "

const codexFixture = "\n\x1b[94mWelcome to Codex\x1b[0m\n\n" +
	"1. Open this link in your browser and sign in to your account\n" +
	"   \x1b[94mhttps://auth.openai.com/codex/device\x1b[0m\n\n" +
	"2. Enter this one-time code \x1b[90m(expires in 15 minutes)\x1b[0m\n" +
	"   \x1b[94mABCD-EFGH1\x1b[0m\n\n" +
	"\x1b[90mDevice codes are a common phishing target. Never share this code.\x1b[0m\n"

// TestLoginHelper is the fake podman child. HELPER_MODE picks what it does.
func TestLoginHelper(t *testing.T) {
	if os.Getenv("GO_WANT_LOGIN_HELPER") != "1" {
		return
	}
	exit, _ := strconv.Atoi(os.Getenv("HELPER_EXIT"))
	switch os.Getenv("HELPER_MODE") {
	case "claude":
		fmt.Print(claudeFixture)
		// ReadAll returns only once stdin is closed.
		in, _ := io.ReadAll(os.Stdin)
		if string(in) != os.Getenv("HELPER_WANT")+"\n" {
			os.Exit(3)
		}
	case "codex":
		fmt.Fprint(os.Stderr, codexFixture)
	case "shell":
		shellHelper()
	case "terminal":
		fmt.Print("tty-ok")
	case "stderr":
		fmt.Fprint(os.Stderr, os.Getenv("HELPER_OUT"))
	case "out":
		fmt.Print(os.Getenv("HELPER_OUT"))
		if exit != 0 {
			fmt.Fprint(os.Stderr, "ps failed")
		}
	}
	os.Exit(exit)
}

type fakeHost struct {
	stopped   bool
	starts    int
	volumes   map[string]bool
	loggedIn  map[string]bool
	checkExit int // when non-zero, every check exits with it
	loginExit int
	// loginWorks marks the provider logged in when its login exits 0.
	loginWorks bool
	want       string
	calls      [][]string

	// The runner host, for Shell (see runner_add_test.go). A nil slots
	// map means one complete runner per provider.
	slots   map[string]*fakeSlot
	vm, wsl bool
	probe   string
	linger  bool
	// icu is true once the libicu script succeeded; the probe then finds
	// libicu.
	icu bool
	// ssl is true once the libssl script succeeded; the probe then finds
	// libssl.
	ssl       bool
	shells    []shellCall
	shellExit map[string]int
	shellErr  map[string]string
	// shellOut overrides the stdout of the named script.
	shellOut   map[string]string
	archiveSHA string
	token      string
	units      map[string]string
	// stopExit is the stop script's exit status per slot; purgeNoop makes the
	// purge script succeed without removing anything; rmExit is podman
	// volume rm's exit status.
	stopExit map[string]int
	// onDrain runs when the drain script of a slot succeeds: the Jobs on
	// the host ended.
	onDrain   func(slot string)
	purgeNoop bool
	rmExit    int
	// existsExit, when non-zero, is podman volume exists's exit status,
	// with existsErr on its stderr.
	existsExit int
	existsErr  string
	// events records the order of the changing steps.
	events []string
	// The deploy key (see runner_request_test.go): deployPub is the
	// content of its .pub, "" when the pair is absent; deployMode the
	// private key's mode; deployPriv the file the write script's stdin is
	// saved to.
	deployPub, deployMode, deployPriv string

	// The job-started hook (see runner_hook_test.go): hook is its files,
	// nil when the directory is absent; hookMode the directory's mode;
	// hookTar the file a write's stdin is saved to until the next read;
	// hookTmp holds those files.
	hook             map[string][]byte
	hookMode         string
	hookTar, hookTmp string

	// The autostart entry (see autostart_test.go): entry is its state;
	// autoStarts counts starts through it.
	entry       host.Autostart
	entryErr    error
	registerErr error
	staleRead   bool
	idleSet     bool
	idleErr     error
	wslErr      error
	autoStarts  int

	// psOut and inspectOut are what podman ps and podman container inspect
	// print (see host_slots_test.go); psExit is podman ps's exit status.
	psOut, inspectOut string
	psExit            int

	// The image pull (see pull_test.go): the n-th podman pull exits with
	// pullExits[n] (0 past its end) and pullErr on its stderr; podman image
	// exists exits with imageExit.
	pullExits []int
	pullErr   string
	pulls     int
	imageExit int
}

func (f *fakeHost) NeedsStart() bool { return f.stopped }
func (f *fakeHost) Start(context.Context) error {
	f.starts++
	f.stopped = false
	return nil
}
func (f *fakeHost) Describe() string { return "fake" }

func (f *fakeHost) Podman(ctx context.Context, args ...string) *exec.Cmd {
	f.calls = append(f.calls, args)
	mode, exit := "plain", 0
	switch {
	case args[0] == "pull":
		exit = 0
		if f.pulls < len(f.pullExits) {
			exit = f.pullExits[f.pulls]
		}
		f.pulls++
		c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoginHelper$")
		c.Env = append(os.Environ(), "GO_WANT_LOGIN_HELPER=1", "HELPER_MODE=stderr", "HELPER_EXIT="+strconv.Itoa(exit), "HELPER_OUT="+f.pullErr)
		return c
	case args[0] == "image" && args[1] == "exists":
		exit = f.imageExit
	case args[0] == "volume" && args[1] == "exists":
		if !f.volumes[strings.TrimPrefix(args[2], "nibfin-home-")] {
			exit = 1
		}
		if f.existsExit != 0 {
			c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoginHelper$")
			c.Env = append(os.Environ(), "GO_WANT_LOGIN_HELPER=1", "HELPER_MODE=stderr", "HELPER_EXIT="+strconv.Itoa(f.existsExit), "HELPER_OUT="+f.existsErr)
			return c
		}
	case args[0] == "volume" && args[1] == "rm":
		exit = f.rmExit
		if exit == 0 {
			delete(f.volumes, strings.TrimPrefix(args[2], "nibfin-home-"))
		}
		f.events = append(f.events, "rm volume "+args[2])
	case args[0] == "volume" && args[1] == "create":
		f.volumes[strings.TrimPrefix(args[3], "nibfin-home-")] = true
		f.events = append(f.events, "create volume "+args[3])
	case args[0] == "ps":
		mode, exit = "out", f.psExit
		c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoginHelper$")
		c.Env = append(os.Environ(), "GO_WANT_LOGIN_HELPER=1", "HELPER_MODE="+mode, "HELPER_EXIT="+strconv.Itoa(exit), "HELPER_OUT="+f.psOut)
		return c
	case args[0] == "container" && args[1] == "inspect":
		c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoginHelper$")
		c.Env = append(os.Environ(), "GO_WANT_LOGIN_HELPER=1", "HELPER_MODE=out", "HELPER_EXIT=0", "HELPER_OUT="+f.inspectOut)
		return c
	case args[0] == "run":
		p := args[len(args)-3]
		if args[len(args)-1] == "status" {
			if !f.loggedIn[p] {
				exit = 1
			}
			if f.checkExit != 0 {
				exit = f.checkExit
			}
			break
		}
		exit = f.loginExit
		mode = p
		f.events = append(f.events, "log in "+p)
		if hasArg(args, "-t") {
			mode = "terminal"
		}
		if exit == 0 && f.loginWorks {
			f.loggedIn[p] = true
		}
	}
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoginHelper$")
	c.Env = append(os.Environ(), "GO_WANT_LOGIN_HELPER=1", "HELPER_MODE="+mode, "HELPER_EXIT="+strconv.Itoa(exit), "HELPER_WANT="+f.want)
	return c
}

func hasArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

func useHost(t *testing.T, f *fakeHost) {
	t.Helper()
	if f.volumes == nil {
		f.volumes = map[string]bool{}
	}
	if f.slots == nil {
		f.slots = map[string]*fakeSlot{
			"claude-1": {configured: true, unit: true, enabled: true, active: true, name: "h-claude-1", url: "https://github.com/o/r"},
			"codex-1":  {configured: true, unit: true, enabled: true, active: true, name: "h-codex-1", url: "https://github.com/o/r"},
		}
	}
	if f.loggedIn == nil {
		f.loggedIn = map[string]bool{}
	}
	old := detectHost
	detectHost = func(context.Context) (runnerHost, error) { return f, nil }
	t.Cleanup(func() { detectHost = old })
}

type recInteract struct {
	steps []LoginStep
	code  string
	err   error
}

func (r *recInteract) Step(s LoginStep) { r.steps = append(r.steps, s) }
func (r *recInteract) Code(context.Context) (string, error) {
	return r.code, r.err
}

func kinds(steps []LoginStep) []string {
	out := []string{}
	for _, s := range steps {
		out = append(out, s.Kind)
	}
	return out
}

func TestRunArgs(t *testing.T) {
	claude, _ := lookupProvider("claude")
	codex, _ := lookupProvider("codex")
	base := func(p, name, env string) []string {
		return []string{"run", "--rm", "--pull=never", "--userns=keep-id:uid=1000,gid=1000", "--name", name, "--replace",
			"--volume", "nibfin-home-" + p + ":/nibfin/home", "--env", env + "=/nibfin/home"}
	}
	for _, tc := range []struct {
		got, want []string
	}{
		{loginArgs(claude, true), append(base("claude", "nibfin-login-claude", "CLAUDE_CONFIG_DIR"), "-i", "-t", LoginImage, "claude", "auth", "login")},
		{loginArgs(codex, false), append(base("codex", "nibfin-login-codex", "CODEX_HOME"), "-i", LoginImage, "codex", "login", "--device-auth")},
		{checkArgs(claude), append(base("claude", "nibfin-check-claude", "CLAUDE_CONFIG_DIR"), LoginImage, "claude", "auth", "status")},
		{checkArgs(codex), append(base("codex", "nibfin-check-codex", "CODEX_HOME"), LoginImage, "codex", "login", "status")},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("argv = %q\nwant %q", tc.got, tc.want)
		}
	}
	if LoginImage != "ghcr.io/taco3064/nibfin:main" {
		t.Errorf("image = %q", LoginImage)
	}
}

// steps lists the changing items of p; allSteps every item.
func steps(p Plan) []string { return stepNames(p.Changes()) }

func allSteps(p Plan) []string { return stepNames(p.Steps) }

func stepNames(list []Step) []string {
	out := []string{}
	for _, s := range list {
		out = append(out, s.Action+" "+s.Target)
	}
	return out
}

func TestPlanStoppedMachine(t *testing.T) {
	f := &fakeHost{stopped: true}
	useHost(t, f)
	p, err := Login(LoginParams{}).Plan(context.Background())
	want := []string{"start fake", "create volume nibfin-home-claude", "log in claude", "create volume nibfin-home-codex", "log in codex"}
	if err != nil || !reflect.DeepEqual(steps(p), want) || p.Confirm || p.ReadOnly {
		t.Fatalf("plan = %v, %+v, %v", steps(p), p, err)
	}
	if len(f.calls) != 0 {
		t.Errorf("plan ran podman on a stopped machine: %q", f.calls)
	}
}

func TestPlanSelectsNotLoggedIn(t *testing.T) {
	f := &fakeHost{volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useHost(t, f)
	p, err := Login(LoginParams{}).Plan(context.Background())
	want := []string{"create volume nibfin-home-codex", "log in codex"}
	if err != nil || !reflect.DeepEqual(steps(p), want) {
		t.Fatalf("plan = %v, %v", steps(p), err)
	}
	// A named provider always logs in, even when logged in.
	p, err = Login(LoginParams{Provider: "claude"}).Plan(context.Background())
	if err != nil || !reflect.DeepEqual(steps(p), []string{"log in claude"}) {
		t.Fatalf("named plan = %v, %v", steps(p), err)
	}
}

func TestLoginAllLoggedIn(t *testing.T) {
	f := &fakeHost{volumes: map[string]bool{"claude": true, "codex": true}, loggedIn: map[string]bool{"claude": true, "codex": true}}
	useHost(t, f)
	p, r, err := Run(context.Background(), Login(LoginParams{}), nil)
	if err != nil || !p.ReadOnly || len(p.Changes()) != 0 {
		t.Fatalf("Run = %+v, %v", p, err)
	}
	want := map[string]any{"claude": map[string]bool{"loggedIn": true}, "codex": map[string]bool{"loggedIn": true}}
	if !reflect.DeepEqual(r["providers"], want) || r["host"] != "fake" {
		t.Errorf("result = %v", r)
	}
	for _, c := range f.calls {
		if hasArg(c, "login") && !hasArg(c, "status") {
			t.Errorf("ran a login: %q", c)
		}
	}
}

func TestLoginStructuredClaude(t *testing.T) {
	f := &fakeHost{loginWorks: true, want: "the-code"}
	useHost(t, f)
	in := &recInteract{code: "the-code"}
	_, r, err := Run(context.Background(), Login(LoginParams{Provider: "claude", Interact: in}), nil)
	if err != nil {
		t.Fatalf("Run: %v (steps %v)", err, in.steps)
	}
	if got := kinds(in.steps); !reflect.DeepEqual(got, []string{StepOpenURL, StepCodeRequested, StepSucceeded}) {
		t.Errorf("steps = %v", got)
	}
	if in.steps[0].URL != "https://claude.ai/oauth/authorize?code=true&client_id=abc&state=xyz" {
		t.Errorf("url = %q", in.steps[0].URL)
	}
	if !reflect.DeepEqual(r["providers"], map[string]any{"claude": map[string]bool{"loggedIn": true}}) {
		t.Errorf("result = %v", r)
	}
	if !f.volumes["claude"] {
		t.Error("volume not created")
	}
}

func TestLoginStructuredCodex(t *testing.T) {
	f := &fakeHost{loginWorks: true}
	useHost(t, f)
	in := &recInteract{}
	if _, _, err := Run(context.Background(), Login(LoginParams{Provider: "codex", Interact: in}), nil); err != nil {
		t.Fatal(err)
	}
	want := []LoginStep{
		{Provider: "codex", Kind: StepOpenURL, URL: "https://auth.openai.com/codex/device", Message: "open this URL in a browser and sign in"},
		{Provider: "codex", Kind: StepDeviceCode, Code: "ABCD-EFGH1", Message: "enter this one-time code at the URL"},
		{Provider: "codex", Kind: StepSucceeded, Message: "codex login finished"},
	}
	if !reflect.DeepEqual(in.steps, want) {
		t.Errorf("steps = %+v", in.steps)
	}
}

func TestLoginCodeCancelled(t *testing.T) {
	f := &fakeHost{loginWorks: true, want: "x"}
	useHost(t, f)
	in := &recInteract{err: Errorf(CodeCancelled, "no code")}
	_, _, err := Run(context.Background(), Login(LoginParams{Provider: "claude", Interact: in}), nil)
	if Code(err) != CodeCancelled || kinds(in.steps)[len(in.steps)-1] != StepFailed {
		t.Errorf("err = %v, steps %v", err, kinds(in.steps))
	}
	rm := []string{"rm", "--force", "--ignore", "--time", "0", "nibfin-login-claude"}
	at, run := -1, -1
	for i, c := range f.calls {
		if reflect.DeepEqual(c, rm) {
			at = i
		}
		if c[0] == "run" && c[len(c)-1] == "login" {
			run = i
		}
	}
	if at < 0 || run < 0 || at < run {
		t.Errorf("rm at %d, login run at %d; calls %v", at, run, f.calls)
	}
}

func TestLoginSucceededRemovesNothing(t *testing.T) {
	f := &fakeHost{loginWorks: true, want: "the-code"}
	useHost(t, f)
	in := &recInteract{code: "the-code"}
	if _, _, err := Run(context.Background(), Login(LoginParams{Provider: "claude", Interact: in}), nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if c[0] == "rm" {
			t.Errorf("unexpected %v", c)
		}
	}
}

func TestLoginWrongCodeFails(t *testing.T) {
	f := &fakeHost{want: "right"}
	useHost(t, f)
	in := &recInteract{code: "wrong"}
	_, _, err := Run(context.Background(), Login(LoginParams{Provider: "claude", Interact: in}), nil)
	if Code(err) != CodeLoginFailed || !strings.Contains(err.Error(), "status 3") {
		t.Errorf("err = %v", err)
	}
}

func TestLoginExitMapping(t *testing.T) {
	for exit, code := range map[int]string{1: CodeLoginFailed, 125: CodePodmanFailed} {
		useHost(t, &fakeHost{loginExit: exit})
		_, _, err := Run(context.Background(), Login(LoginParams{Provider: "codex", Interact: &recInteract{}}), nil)
		if Code(err) != code {
			t.Errorf("exit %d: %v", exit, err)
		}
	}
	// The CLI exits 0 but the check still says not logged in.
	useHost(t, &fakeHost{})
	_, r, err := Run(context.Background(), Login(LoginParams{Provider: "codex", Interact: &recInteract{}}), nil)
	if Code(err) != CodeNotLoggedIn || r == nil {
		t.Errorf("not logged in: %v, %v", r, err)
	}
}

func TestCheckExitMapping(t *testing.T) {
	codex, _ := lookupProvider("codex")
	for exit, want := range map[int]string{125: CodePodmanFailed, 7: CodeLoginCheckFailed} {
		f := &fakeHost{checkExit: exit}
		l := &login{host: f}
		if _, err := l.check(context.Background(), codex); Code(err) != want {
			t.Errorf("exit %d: %v", exit, err)
		}
	}
	f := &fakeHost{loggedIn: map[string]bool{"codex": true}}
	l := &login{host: f}
	if in, err := l.check(context.Background(), codex); !in || err != nil {
		t.Errorf("exit 0: %v, %v", in, err)
	}
	f.loggedIn = map[string]bool{}
	if in, err := l.check(context.Background(), codex); in || err != nil {
		t.Errorf("exit 1: %v, %v", in, err)
	}
}

func TestLoginStoppedMachineRechecks(t *testing.T) {
	f := &fakeHost{stopped: true, loginWorks: true, volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useHost(t, f)
	in := &recInteract{}
	_, r, err := Run(context.Background(), Login(LoginParams{Interact: in}), nil)
	if err != nil || f.starts != 1 {
		t.Fatalf("Run: %v, starts %d", err, f.starts)
	}
	for _, s := range in.steps {
		if s.Provider != "codex" {
			t.Errorf("logged in %s again", s.Provider)
		}
	}
	want := map[string]any{"claude": map[string]bool{"loggedIn": true}, "codex": map[string]bool{"loggedIn": true}}
	if !reflect.DeepEqual(r["providers"], want) {
		t.Errorf("result = %v", r)
	}
}

func TestLoginTerminal(t *testing.T) {
	f := &fakeHost{loginWorks: true}
	useHost(t, f)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _, err = Run(context.Background(), Login(LoginParams{Provider: "claude", Terminal: &Terminal{In: r, Out: w, Err: w}}), nil)
	w.Close()
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(r)
	if string(out) != "tty-ok" {
		t.Errorf("terminal got %q", out)
	}
	var login []string
	for _, c := range f.calls {
		if c[0] == "run" && c[len(c)-1] == "login" {
			login = c
		}
	}
	if !hasArg(login, "-i") || !hasArg(login, "-t") {
		t.Errorf("login argv = %q", login)
	}
}

func TestLoginNeedsTerminalOrInteractor(t *testing.T) {
	useHost(t, &fakeHost{})
	_, _, err := Run(context.Background(), Login(LoginParams{Provider: "claude"}), nil)
	if Code(err) != CodeInvalidArgument {
		t.Errorf("err = %v", err)
	}
	_, _, err = Run(context.Background(), Login(LoginParams{Provider: "gemini"}), nil)
	if Code(err) != CodeInvalidArgument {
		t.Errorf("gemini: %v", err)
	}
}

func TestHostErrorCodes(t *testing.T) {
	for target, code := range map[error]string{
		host.ErrPodmanMissing:  CodePodmanMissing,
		host.ErrMachineMissing: CodeMachineMissing,
		host.ErrMachineStart:   CodeMachineStartFailed,
		host.ErrUnsupported:    CodeUnsupportedHost,
		host.ErrPodman:         CodePodmanFailed,
	} {
		err := hostError(fmt.Errorf("%w: detail", target))
		if Code(err) != code || !strings.Contains(err.Error(), "detail") {
			t.Errorf("%v: %q %v", target, Code(err), err)
		}
	}
}

func TestScannerPromptSplitAcrossReads(t *testing.T) {
	var got []LoginStep
	s := &loginScanner{provider: "claude", emit: func(l LoginStep) { got = append(got, l) }}
	chunks := []string{"Opening browser…\nIf the browser didn't open, visit:\nhttps://claude.ai/oauth/auth", "orize?x=1\nPaste code ", "here if prompted > "}
	asked := []bool{}
	for _, c := range chunks {
		asked = append(asked, s.feed([]byte(c)))
	}
	if !reflect.DeepEqual(asked, []bool{false, false, true}) {
		t.Errorf("asked = %v", asked)
	}
	if !reflect.DeepEqual(kinds(got), []string{StepOpenURL, StepCodeRequested}) || got[0].URL != "https://claude.ai/oauth/authorize?x=1" {
		t.Errorf("steps = %+v", got)
	}
	// The prompt's line completing later does not ask again.
	if s.feed([]byte("\nmore\n")) || len(got) != 2 {
		t.Errorf("asked twice: %+v", got)
	}
}

func TestScannerCodexANSI(t *testing.T) {
	var got []LoginStep
	s := &loginScanner{provider: "codex", emit: func(l LoginStep) { got = append(got, l) }}
	for i := 0; i < len(codexFixture); i += 7 {
		end := min(i+7, len(codexFixture))
		if s.feed([]byte(codexFixture[i:end])) {
			t.Error("codex asked for a code")
		}
	}
	if !reflect.DeepEqual(kinds(got), []string{StepOpenURL, StepDeviceCode}) || got[0].URL != "https://auth.openai.com/codex/device" || got[1].Code != "ABCD-EFGH1" {
		t.Errorf("steps = %+v", got)
	}
}

func TestLoginParamsJSON(t *testing.T) {
	b, err := json.Marshal(LoginParams{Provider: "claude", Terminal: &Terminal{}, Interact: &recInteract{}})
	if err != nil || string(b) != `{"provider":"claude"}` {
		t.Errorf("json = %s, %v", b, err)
	}
}

func TestLoginNeedsRunners(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": {}}}
	useHost(t, f)
	_, err := Login(LoginParams{Provider: "codex"}).Plan(context.Background())
	if Code(err) != CodeNoRunners || !strings.Contains(err.Error(), "gh nibfin runner add codex") {
		t.Errorf("named: %v", err)
	}
	// Without a provider only the providers with runners here are logged in.
	p, err := Login(LoginParams{}).Plan(context.Background())
	want := []string{"create volume nibfin-home-claude", "log in claude"}
	if err != nil || !reflect.DeepEqual(steps(p), want) {
		t.Errorf("plan = %v, %v", steps(p), err)
	}
	f.slots = map[string]*fakeSlot{}
	if _, err := Login(LoginParams{}).Plan(context.Background()); Code(err) != CodeNoRunners {
		t.Errorf("none: %v", err)
	}
	if _, err := Login(LoginParams{}).ReadBack(context.Background(), Plan{}); Code(err) != CodeNoRunners {
		t.Errorf("read back: %v", err)
	}
}

func TestLoginStoppedMachineNeedsRunners(t *testing.T) {
	f := &fakeHost{stopped: true, slots: map[string]*fakeSlot{"claude-1": {}}, loginWorks: true}
	useHost(t, f)
	in := &recInteract{}
	_, _, err := Run(context.Background(), Login(LoginParams{Provider: "codex", Interact: in}), nil)
	if Code(err) != CodeNoRunners || f.starts != 1 || len(f.events) != 0 {
		t.Errorf("named: %v, starts %d, events %v", err, f.starts, f.events)
	}
	// Without a provider the steps of providers without runners are skipped.
	f.stopped = true
	f.events = nil
	_, r, err := Run(context.Background(), Login(LoginParams{Interact: in}), nil)
	want := map[string]any{"claude": map[string]bool{"loggedIn": true}}
	if err != nil || !reflect.DeepEqual(f.events, []string{"create volume nibfin-home-claude", "log in claude"}) || !reflect.DeepEqual(r["providers"], want) {
		t.Errorf("all: %v, events %v, result %v", err, f.events, r)
	}
}

func TestLoginIfNeeded(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{}, volumes: map[string]bool{"claude": true}, loggedIn: map[string]bool{"claude": true}}
	useHost(t, f)
	l := &login{p: LoginParams{Provider: "claude"}, host: f, ifNeeded: true}
	p, err := l.Plan(context.Background())
	if err != nil || len(p.Changes()) != 0 {
		t.Errorf("logged in: %v, %v", steps(p), err)
	}
	f.loggedIn["claude"] = false
	if p, err = l.Plan(context.Background()); err != nil || !reflect.DeepEqual(steps(p), []string{"log in claude"}) {
		t.Errorf("logged out: %v, %v", steps(p), err)
	}
}

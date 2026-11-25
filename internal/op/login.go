package op

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/host"
	"github.com/taco3064/gh-nibfin/internal/runners"
)

// LoginImage is the released image the Jobs run; login runs the provider CLI
// in it so the login lands where the Jobs read it.
const LoginImage = "ghcr.io/taco3064/nibfin:main"

// loginHome is the provider's config home inside the container, the mount
// point of the nibfin-home-<provider> volume.
const loginHome = "/nibfin/home"

type provider struct {
	name  string
	env   string
	login []string
	check []string
}

// providerTable lists the providers in login order.
var providerTable = []provider{
	{"claude", "CLAUDE_CONFIG_DIR", []string{"claude", "auth", "login"}, []string{"claude", "auth", "status"}},
	{"codex", "CODEX_HOME", []string{"codex", "login", "--device-auth"}, []string{"codex", "login", "status"}},
}

func lookupProvider(name string) (provider, bool) {
	for _, p := range providerTable {
		if p.name == name {
			return p, true
		}
	}
	return provider{}, false
}

func (p provider) volume() string { return "nibfin-home-" + p.name }

// LoginParams are the inputs of login. Exactly one of Terminal and Interact
// is set by the caller; neither is serialised.
type LoginParams struct {
	Provider string `json:"provider,omitempty"`
	// Terminal attaches the provider CLI to this TTY.
	Terminal *Terminal `json:"-"`
	// Interact receives the login as structured steps and supplies a pasted
	// code.
	Interact Interactor `json:"-"`
}

// Terminal is a TTY to attach the provider CLI to.
type Terminal struct {
	In, Out, Err *os.File
}

// Interactor is the structured side of a login, used when no TTY is
// attached (the Dashboard, or a CLI not on a terminal).
type Interactor interface {
	// Step reports one login step.
	Step(LoginStep)
	// Code returns the code the user pasted after a code_requested step.
	Code(ctx context.Context) (string, error)
}

// Login step kinds.
const (
	StepOpenURL       = "open_url"
	StepDeviceCode    = "device_code"
	StepCodeRequested = "code_requested"
	StepSucceeded     = "succeeded"
	StepFailed        = "failed"
)

// LoginStep is one structured login step.
type LoginStep struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	URL      string `json:"url,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
}

func providerNames() []string {
	out := make([]string, len(providerTable))
	for i, p := range providerTable {
		out[i] = p.name
	}
	return out
}

// runnerHost is the part of host.Host the operations use; tests replace
// detectHost.
type runnerHost interface {
	NeedsStart() bool
	Start(ctx context.Context) error
	Podman(ctx context.Context, args ...string) *exec.Cmd
	Shell(ctx context.Context, script string, args ...string) *exec.Cmd
	Local() bool
	WSL() bool
	Describe() string
	// The autostart entry and the .wslconfig key (see internal/host).
	Autostart(ctx context.Context) (host.Autostart, error)
	EnsureAutostart(ctx context.Context) (host.Autostart, error)
	StartViaAutostart(ctx context.Context) error
	RemoveAutostart(ctx context.Context) error
	IdleTimeoutSet() (bool, error)
	EnsureIdleTimeout() (bool, error)
	CheckWSL(ctx context.Context) error
}

// listRunners discovers the runners on the host.
func listRunners(ctx context.Context, h runnerHost) (*runners.Inventory, error) {
	inv, err := runners.List(ctx, h, providerNames())
	if err != nil {
		if ctx.Err() != nil {
			return nil, Errorf(CodeCancelled, "cancelled")
		}
		return nil, Errorf(CodePodmanFailed, "%s: %w", h.Describe(), err)
	}
	return inv, nil
}

var detectHost = func(ctx context.Context) (runnerHost, error) {
	h, err := host.Detect(ctx)
	if err != nil {
		return nil, hostError(err)
	}
	return h, nil
}

func hostError(err error) error {
	for _, m := range []struct {
		target error
		code   string
	}{
		{host.ErrPodmanMissing, CodePodmanMissing},
		{host.ErrMachineMissing, CodeMachineMissing},
		{host.ErrMachineStart, CodeMachineStartFailed},
		{host.ErrUnsupported, CodeUnsupportedHost},
		{host.ErrPodman, CodePodmanFailed},
		{host.ErrAutostart, CodeAutostartFailed},
	} {
		if errors.Is(err, m.target) {
			return Wrap(m.code, err)
		}
	}
	return err
}

// Login is the entry point of login.
func Login(p LoginParams) Operation { return &login{p: p} }

const (
	actStart        = "start"
	actCreateVolume = "create volume"
	actLogIn        = "log in"
)

type login struct {
	p    LoginParams
	host runnerHost
	// ifNeeded is runner add's login: a named provider logs in only when its
	// check says it is not logged in, and the runner rule does not apply.
	ifNeeded bool
}

// allowed returns the providers that have runners on the host, which are
// the only ones login acts on; nil when the rule does not apply. A named
// provider without runners, or no runners at all, is no_runners.
func (l *login) allowed(ctx context.Context) (map[string]bool, error) {
	if l.ifNeeded {
		return nil, nil
	}
	inv, err := listRunners(ctx, l.host)
	if err != nil {
		return nil, err
	}
	have := inv.Providers()
	if l.p.Provider != "" && !have[l.p.Provider] {
		return nil, Errorf(CodeNoRunners, "no %s runners on %s; add runners first: gh nibfin runner add %s", l.p.Provider, l.host.Describe(), l.p.Provider)
	}
	if len(have) == 0 {
		return nil, Errorf(CodeNoRunners, "no runners on %s; add runners first: gh nibfin runner add <claude|codex>", l.host.Describe())
	}
	return have, nil
}

func filterProviders(ps []provider, have map[string]bool) []provider {
	if have == nil {
		return ps
	}
	var out []provider
	for _, p := range ps {
		if have[p.name] {
			out = append(out, p)
		}
	}
	return out
}

func (l *login) detect(ctx context.Context) error {
	if l.host != nil {
		return nil
	}
	h, err := detectHost(ctx)
	if err != nil {
		return err
	}
	l.host = h
	return nil
}

// targets are the providers this login acts on: the named one, or all.
func (l *login) targets() ([]provider, error) {
	if l.p.Provider == "" {
		return providerTable, nil
	}
	p, ok := lookupProvider(l.p.Provider)
	if !ok {
		return nil, Errorf(CodeInvalidArgument, "unknown provider %q; use claude or codex", l.p.Provider)
	}
	return []provider{p}, nil
}

func (l *login) Plan(ctx context.Context) (Plan, error) {
	plan := Plan{Operation: "login", Steps: []Step{}}
	targets, err := l.targets()
	if err != nil {
		return plan, err
	}
	if err := l.detect(ctx); err != nil {
		return plan, err
	}
	where := l.host.Describe()
	// A stopped machine cannot be asked yet: every target is planned and
	// Apply checks again after the start.
	stopped := l.host.NeedsStart()
	if stopped {
		plan.Steps = append(plan.Steps, Step{Action: actStart, Target: where})
	} else {
		have, err := l.allowed(ctx)
		if err != nil {
			return plan, err
		}
		targets = filterProviders(targets, have)
	}
	for _, p := range targets {
		has := false
		if !stopped {
			if has, err = volumeExists(ctx, l.host, p.volume()); err != nil {
				return plan, err
			}
		}
		if !has {
			plan.Steps = append(plan.Steps, Step{Action: actCreateVolume, Target: p.volume(), Detail: where})
		}
		// A named provider always logs in: the request may re-login or
		// switch accounts. Runner add's login is only done when needed.
		need := (l.p.Provider != "" && !l.ifNeeded) || !has
		if !need {
			in, err := l.check(ctx, p)
			if err != nil {
				return plan, err
			}
			need = !in
		}
		if need {
			plan.Steps = append(plan.Steps, Step{Action: actLogIn, Target: p.name, Detail: where})
		}
	}
	plan.ReadOnly = len(plan.Steps) == 0
	return plan, nil
}

func (l *login) Apply(ctx context.Context, plan Plan) error {
	if err := l.detect(ctx); err != nil {
		return err
	}
	recheck := false
	// The runner rule is applied once the host runs, before any volume or
	// login step.
	var have map[string]bool
	ruled := false
	rule := func() error {
		if ruled {
			return nil
		}
		var err error
		have, err = l.allowed(ctx)
		ruled = err == nil
		return err
	}
	for _, s := range plan.Steps {
		switch s.Action {
		case actStart:
			if err := l.host.Start(ctx); err != nil {
				return hostError(err)
			}
			recheck = l.p.Provider == "" || l.ifNeeded
		case actCreateVolume:
			p, ok := providerOfVolume(s.Target)
			if !ok {
				return Errorf(CodeInvalidArgument, "unknown login volume %q", s.Target)
			}
			if err := rule(); err != nil {
				return err
			}
			if have != nil && !have[p.name] {
				continue
			}
			if err := createVolume(ctx, l.host, p.volume()); err != nil {
				return err
			}
		case actLogIn:
			p, ok := lookupProvider(s.Target)
			if !ok {
				return Errorf(CodeInvalidArgument, "unknown provider %q", s.Target)
			}
			if err := rule(); err != nil {
				return err
			}
			if have != nil && !have[p.name] {
				continue
			}
			if recheck {
				in, err := l.check(ctx, p)
				if err != nil {
					return err
				}
				if in {
					continue
				}
			}
			if err := l.runLogin(ctx, p); err != nil {
				return err
			}
		default:
			return Errorf(CodeInvalidArgument, "unknown login step %q", s.Action)
		}
	}
	return nil
}

func (l *login) ReadBack(ctx context.Context, plan Plan) (Result, error) {
	targets, err := l.targets()
	if err != nil {
		return nil, err
	}
	if err := l.detect(ctx); err != nil {
		return nil, err
	}
	if l.host.NeedsStart() {
		return nil, Errorf(CodePodmanFailed, "%s is not running", l.host.Describe())
	}
	have, err := l.allowed(ctx)
	if err != nil {
		return nil, err
	}
	targets = filterProviders(targets, have)
	planned := map[string]bool{}
	for _, s := range plan.Steps {
		if s.Action == actLogIn {
			planned[s.Target] = true
		}
	}
	states := map[string]any{}
	var missing []string
	for _, p := range targets {
		has, err := volumeExists(ctx, l.host, p.volume())
		if err != nil {
			return nil, err
		}
		in := false
		if has {
			if in, err = l.check(ctx, p); err != nil {
				return nil, err
			}
		}
		states[p.name] = map[string]bool{"loggedIn": in}
		if !in && planned[p.name] {
			missing = append(missing, p.name)
		}
	}
	r := Result{"providers": states, "host": l.host.Describe()}
	if len(missing) > 0 {
		return r, Errorf(CodeNotLoggedIn, "%s is not logged in after login", strings.Join(missing, ", "))
	}
	return r, nil
}

func providerOfVolume(v string) (provider, bool) {
	for _, p := range providerTable {
		if p.volume() == v {
			return p, true
		}
	}
	return provider{}, false
}

// Pull policies of runArgs: login and its check refresh the image; status
// never pulls.
const (
	pullNewer = "newer"
	pullNever = "never"
)

// runArgs is the one argv builder for every container login and check runs.
func runArgs(p provider, pull, name string, interactive, tty bool, argv []string) []string {
	a := []string{
		"run", "--rm", "--pull=" + pull,
		"--userns=keep-id:uid=1000,gid=1000",
		"--name", name, "--replace",
		"--volume", p.volume() + ":" + loginHome,
		"--env", p.env + "=" + loginHome,
	}
	if interactive {
		a = append(a, "-i")
	}
	if tty {
		a = append(a, "-t")
	}
	a = append(a, LoginImage)
	return append(a, argv...)
}

func loginArgs(p provider, tty bool) []string {
	return runArgs(p, pullNewer, "nibfin-login-"+p.name, true, tty, p.login)
}

func checkArgs(p provider) []string {
	return runArgs(p, pullNewer, "nibfin-check-"+p.name, false, false, p.check)
}

// statusCheckArgs is status's login check: it never pulls the image and has
// its own container name, so it never replaces a check login or runner add
// is running.
func statusCheckArgs(p provider) []string {
	return runArgs(p, pullNever, "nibfin-status-"+p.name, false, false, p.check)
}

// exitCode returns the child's exit status, or -1 when it did not exit.
func exitCode(err error) int {
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return -1
}

func stderrText(err error, stderr *bytes.Buffer) string {
	if s := strings.TrimSpace(stderr.String()); s != "" {
		return s
	}
	return err.Error()
}

// quiet runs a podman command on h with its output discarded and stderr kept for
// the error message. It returns the exit status.
func quiet(ctx context.Context, h runnerHost, args ...string) (int, *bytes.Buffer, error) {
	var stderr bytes.Buffer
	c := h.Podman(ctx, args...)
	c.Stdout = io.Discard
	c.Stderr = &stderr
	err := c.Run()
	if err == nil {
		return 0, &stderr, nil
	}
	if ctx.Err() != nil {
		return -1, &stderr, Errorf(CodeCancelled, "cancelled")
	}
	code := exitCode(err)
	if code < 0 {
		return code, &stderr, Errorf(CodePodmanFailed, "podman %s: %s", args[0], stderrText(err, &stderr))
	}
	return code, &stderr, nil
}

func volumeExists(ctx context.Context, h runnerHost, volume string) (bool, error) {
	code, stderr, err := quiet(ctx, h, "volume", "exists", volume)
	switch {
	case err != nil:
		return false, err
	case code == 0:
		return true, nil
	case code == 1:
		return false, nil
	}
	return false, Errorf(CodePodmanFailed, "podman volume exists %s exited with status %d: %s", volume, code, strings.TrimSpace(stderr.String()))
}

func createVolume(ctx context.Context, h runnerHost, volume string) error {
	code, stderr, err := quiet(ctx, h, "volume", "create", "--ignore", volume)
	if err != nil {
		return err
	}
	if code != 0 {
		return Errorf(CodePodmanFailed, "podman volume create %s exited with status %d: %s", volume, code, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// check runs the provider CLI's own login check. Only its exit status is
// used; its output can carry account fields and is discarded.
func (l *login) check(ctx context.Context, p provider) (bool, error) {
	return runCheck(ctx, l.host, p, checkArgs(p))
}

// runCheck runs the login check argv on h; only its exit status is used.
func runCheck(ctx context.Context, h runnerHost, p provider, argv []string) (bool, error) {
	code, stderr, err := quiet(ctx, h, argv...)
	switch {
	case err != nil:
		return false, err
	case code == 0:
		return true, nil
	case code == 1:
		return false, nil
	case code == 125:
		return false, Errorf(CodePodmanFailed, "podman could not run the %s login check: %s", p.name, strings.TrimSpace(stderr.String()))
	}
	return false, Errorf(CodeLoginCheckFailed, "%s login check exited with status %d", p.name, code)
}

func (l *login) runLogin(ctx context.Context, p provider) error {
	switch {
	case l.p.Terminal != nil:
		t := l.p.Terminal
		c := l.host.Podman(ctx, loginArgs(p, true)...)
		c.Stdin, c.Stdout, c.Stderr = t.In, t.Out, t.Err
		return loginExit(ctx, p, c.Run())
	case l.p.Interact != nil:
		return l.structured(ctx, p)
	}
	return Errorf(CodeInvalidArgument, "login needs a terminal or an interactor")
}

func loginExit(ctx context.Context, p provider, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return Errorf(CodeCancelled, "%s login cancelled", p.name)
	}
	switch code := exitCode(err); code {
	case -1:
		return Errorf(CodePodmanFailed, "podman could not run the %s login: %v", p.name, err)
	case 125:
		return Errorf(CodePodmanFailed, "podman could not run the %s login (exit status 125)", p.name)
	default:
		return Errorf(CodeLoginFailed, "%s login exited with status %d", p.name, code)
	}
}

// structured runs the login without a TTY and turns the CLI's output into
// steps. The output itself is never returned or logged.
func (l *login) structured(parent context.Context, p provider) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	c := l.host.Podman(ctx, loginArgs(p, false)...)
	stdin, err := c.StdinPipe()
	if err != nil {
		return Errorf(CodePodmanFailed, "podman could not run the %s login: %v", p.name, err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		return Errorf(CodePodmanFailed, "podman could not run the %s login: %v", p.name, err)
	}
	defer r.Close()
	c.Stdout, c.Stderr = w, w
	err = c.Start()
	w.Close()
	if err != nil {
		return Errorf(CodePodmanFailed, "podman could not run the %s login: %v", p.name, err)
	}

	sc := &loginScanner{provider: p.name, emit: l.p.Interact.Step}
	var codeErr error
	buf := make([]byte, 4096)
	for {
		n, rerr := r.Read(buf)
		if n > 0 && sc.feed(buf[:n]) && codeErr == nil {
			code, err := l.p.Interact.Code(ctx)
			if err != nil {
				codeErr = err
				cancel()
			} else {
				// A failed write means the CLI has exited; its status decides.
				_, _ = io.WriteString(stdin, code+"\n")
			}
			stdin.Close()
		}
		if rerr != nil {
			break
		}
	}
	stdin.Close()
	werr := c.Wait()

	if codeErr == nil {
		codeErr = loginExit(parent, p, werr)
	}
	if codeErr != nil {
		l.p.Interact.Step(LoginStep{Provider: p.name, Kind: StepFailed, Message: codeErr.Error()})
		return codeErr
	}
	l.p.Interact.Step(LoginStep{Provider: p.name, Kind: StepSucceeded, Message: p.name + " login finished"})
	return nil
}

var (
	ansiRE       = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)
	urlRE        = regexp.MustCompile(`https://[^\s"'<>]+`)
	deviceCodeRE = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4,}$`)
)

const (
	pastePrompt = "Paste code here"
	oneTimeCode = "one-time code"
	maxTail     = 64 << 10
)

// loginScanner extracts the URL, the device code and the paste prompt from
// the CLI's output. It reads chunks, so a prompt with no trailing newline is
// seen in the unterminated tail.
type loginScanner struct {
	provider string
	emit     func(LoginStep)
	tail     []byte

	sawURL, sawCode, afterOneTime, requested bool
}

// feed takes the next chunk and reports whether a code was just requested.
func (s *loginScanner) feed(b []byte) bool {
	s.tail = append(s.tail, b...)
	asked := false
	for {
		i := bytes.IndexAny(s.tail, "\r\n")
		if i < 0 {
			break
		}
		line := clean(s.tail[:i])
		s.tail = s.tail[i+1:]
		if s.line(line) {
			asked = true
		}
	}
	if len(s.tail) > maxTail {
		s.tail = append([]byte(nil), s.tail[len(s.tail)-maxTail:]...)
	}
	if s.prompt(clean(s.tail)) {
		asked = true
	}
	return asked
}

func clean(b []byte) string { return ansiRE.ReplaceAllString(string(b), "") }

func (s *loginScanner) line(l string) bool {
	if !s.sawURL {
		if u := urlRE.FindString(l); u != "" {
			s.sawURL = true
			s.emit(LoginStep{Provider: s.provider, Kind: StepOpenURL, URL: u, Message: "open this URL in a browser and sign in"})
		}
	}
	if t := strings.TrimSpace(l); s.afterOneTime && !s.sawCode && deviceCodeRE.MatchString(t) {
		s.sawCode = true
		s.emit(LoginStep{Provider: s.provider, Kind: StepDeviceCode, Code: t, Message: "enter this one-time code at the URL"})
	}
	if strings.Contains(strings.ToLower(l), oneTimeCode) {
		s.afterOneTime = true
	}
	return s.prompt(l)
}

func (s *loginScanner) prompt(l string) bool {
	if s.requested || !strings.Contains(l, pastePrompt) {
		return false
	}
	s.requested = true
	s.emit(LoginStep{Provider: s.provider, Kind: StepCodeRequested, Message: "paste the code shown in your browser"})
	return true
}

package op

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/runners"
)

// RunnerAddParams are the inputs of runner add. Terminal and Interact are
// handed to the provider login when it is needed; neither is serialised.
type RunnerAddParams struct {
	Provider string     `json:"provider"`
	Count    int        `json:"count"`
	Terminal *Terminal  `json:"-"`
	Interact Interactor `json:"-"`
}

// RunnerAdd is the entry point of runner add.
func RunnerAdd(p RunnerAddParams) Operation { return &runnerAdd{p: p} }

const (
	actLinger     = "enable linger"
	actAddRunner  = "add runner"
	actAddRunners = "add runners"
	actAdvise     = "advise"

	toolsVolume = "nibfin-tools"
	// memoryPerRunnerGB is the NIBFIN_MEMORY default of a stage run.
	memoryPerRunnerGB = 3
)

// The runner-host scripts. They are constants; only data travels as their
// arguments.
const (
	// probeScript prints what runner add and status need to know about the
	// host.
	probeScript = `u=$(id -u)
n=$(id -un)
printf 'arch %s\n' "$(uname -m | tr -cd 'A-Za-z0-9_')"
printf 'node %s\n' "$(uname -n | tr -cd 'A-Za-z0-9.-')"
printf 'machine %s\n' "$(tr -cd '0-9a-fA-F' < /etc/machine-id 2>/dev/null)"
if [ -e "/var/lib/systemd/linger/$n" ]; then echo 'linger 1'; else echo 'linger 0'; fi
c="/sys/fs/cgroup/user.slice/user-$u.slice/user@$u.service/cgroup.controllers"
if [ -r "$c" ]; then printf 'cgroup %s\n' "$(tr -cd 'a-z_ ' < "$c")"; else echo 'cgroup ?'; fi
printf 'memkb %s\n' "$(awk '/^MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null | tr -cd '0-9')"
printf 'memavailkb %s\n' "$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo 2>/dev/null | tr -cd '0-9')"
`
	// lingerScript keeps the user's systemd manager running after logout.
	lingerScript = `loginctl enable-linger`
	// extractScript unpacks the runner archive from stdin into the slot $1.
	// A new slot ($2 = new) must not exist yet.
	extractScript = `set -e
mkdir -p "${1%/*}"
if [ "$2" = new ]; then mkdir "$1"; else mkdir -p "$1"; fi
tar -xzf - -C "$1"
cp "$1/bin/runsvc.sh" "$1/runsvc.sh"
`
	// configureScript registers the runner in the slot $1 with the flags
	// that follow. The registration token arrives on stdin and reaches
	// config.sh only as ACTIONS_RUNNER_INPUT_TOKEN, which the runner unsets
	// once read.
	configureScript = `cd "$1" || exit 1
shift
IFS= read -r ACTIONS_RUNNER_INPUT_TOKEN || [ -n "$ACTIONS_RUNNER_INPUT_TOKEN" ] || exit 1
export ACTIONS_RUNNER_INPUT_TOKEN
exec ./config.sh "$@" </dev/null
`
	// unitScript writes the unit from stdin into the directory $1 as $2,
	// atomically, and enables and starts it.
	unitScript = `set -e
mkdir -p "$1"
cat > "$1/$2.tmp"
mv -f "$1/$2.tmp" "$1/$2"
systemctl --user daemon-reload
systemctl --user enable --now "$2"
`
)

// currentRepo resolves the current directory's repository; tests replace it.
var currentRepo = func() (string, error) { return ResolveRepo("") }

type runnerAdd struct {
	p    RunnerAddParams
	prov provider
	host runnerHost
	// applied are the slots Apply acted on, for ReadBack.
	applied []string
}

// addEnv is what runner add learns from the host and GitHub.
type addEnv struct {
	inv   *runners.Inventory
	probe hostProbe
	repo  string
	gh    github
	user  int64
}

type hostProbe struct {
	arch, node, machine string
	linger              bool
	cgroupRead          bool
	controllers         map[string]bool
	memKB               int64
	memAvailKB          int64
}

func (a *runnerAdd) validate() error {
	p, ok := lookupProvider(a.p.Provider)
	if !ok {
		return Errorf(CodeInvalidArgument, "unknown provider %q; use claude or codex", a.p.Provider)
	}
	if a.p.Count < 1 {
		return Errorf(CodeInvalidArgument, "count must be at least 1, got %d", a.p.Count)
	}
	a.prov = p
	return nil
}

func (a *runnerAdd) detect(ctx context.Context) error {
	if a.host != nil {
		return nil
	}
	h, err := detectHost(ctx)
	if err != nil {
		return err
	}
	a.host = h
	return nil
}

func (a *runnerAdd) login() *login {
	return &login{
		p:        LoginParams{Provider: a.prov.name, Terminal: a.p.Terminal, Interact: a.p.Interact},
		host:     a.host,
		ifNeeded: true,
	}
}

func (a *runnerAdd) Plan(ctx context.Context) (Plan, error) {
	plan := Plan{Operation: "runner add", Steps: []Step{}, Confirm: true}
	if err := a.validate(); err != nil {
		return plan, err
	}
	if err := a.detect(ctx); err != nil {
		return plan, err
	}
	where := a.host.Describe()
	if a.host.NeedsStart() {
		// Nothing can be asked of a stopped machine; Apply derives the
		// rest after the start.
		plan.Steps = append(plan.Steps,
			Step{Action: actStart, Target: where},
			Step{Action: actLinger, Target: where},
			Step{Action: actCreateVolume, Target: toolsVolume, Detail: where},
		)
		lp, err := a.login().Plan(ctx)
		if err != nil {
			return plan, err
		}
		for _, s := range lp.Steps {
			if s.Action != actStart {
				plan.Steps = append(plan.Steps, s)
			}
		}
		plan.Steps = append(plan.Steps, Step{Action: actAddRunners, Target: fmt.Sprintf("%s ×%d", a.prov.name, a.p.Count), Detail: "repository and slots chosen after start"})
		return plan, nil
	}
	env, err := a.load(ctx)
	if err != nil {
		return plan, err
	}
	plan.Repo = env.repo
	plan.Steps, err = a.steps(ctx, env)
	return plan, err
}

// load discovers the runners, picks the repository, probes the host and
// asks GitHub for the user id.
func (a *runnerAdd) load(ctx context.Context) (*addEnv, error) {
	inv, err := listRunners(ctx, a.host)
	if err != nil {
		return nil, err
	}
	repo, err := runners.Repo(inv.Runners)
	if err != nil {
		return nil, Wrap(CodeRunnerRepoConflict, err)
	}
	if repo == "" {
		if repo, err = currentRepo(); err != nil {
			return nil, err
		}
	}
	pr, err := a.probe(ctx)
	if err != nil {
		return nil, err
	}
	gh, err := newGitHub(repoHost(repo))
	if err != nil {
		return nil, err
	}
	user, err := gh.User(ctx)
	if err != nil {
		return nil, err
	}
	return &addEnv{inv: inv, probe: pr, repo: repo, gh: gh, user: user}, nil
}

func (a *runnerAdd) labels(user int64) string {
	return fmt.Sprintf("nibfin,nibfin-%d-%s", user, a.prov.name)
}

// slots picks the provider's incomplete slots first, then new ones numbered
// after the highest existing slot.
func (a *runnerAdd) slots(inv *runners.Inventory) (slots []string, fresh int) {
	high := 0
	for _, r := range inv.Of(a.prov.name) {
		high = max(high, r.Number)
		if !r.Complete() && len(slots) < a.p.Count {
			slots = append(slots, r.Slot)
		}
	}
	for len(slots) < a.p.Count {
		high++
		fresh++
		slots = append(slots, fmt.Sprintf("%s-%d", a.prov.name, high))
	}
	return slots, fresh
}

func (a *runnerAdd) steps(ctx context.Context, env *addEnv) ([]Step, error) {
	where := a.host.Describe()
	steps := []Step{}
	if !env.probe.linger {
		steps = append(steps, Step{Action: actLinger, Target: where})
	}
	has, err := volumeExists(ctx, a.host, toolsVolume)
	if err != nil {
		return nil, err
	}
	if !has {
		steps = append(steps, Step{Action: actCreateVolume, Target: toolsVolume, Detail: where})
	}
	lp, err := a.login().Plan(ctx)
	if err != nil {
		return nil, err
	}
	steps = append(steps, lp.Steps...)
	slots, fresh := a.slots(env.inv)
	detail := env.repo + ", labels " + a.labels(env.user)
	for _, s := range slots {
		steps = append(steps, Step{Action: actAddRunner, Target: s, Detail: detail})
	}
	for _, adv := range a.advice(env.probe, len(env.inv.Runners)+fresh) {
		steps = append(steps, Step{Action: actAdvise, Target: where, Detail: adv})
	}
	return steps, nil
}

// advice is the non-blocking host advice for runners on the host.
func (a *runnerAdd) advice(pr hostProbe, runnersOnHost int) []string {
	out := []string{}
	if a.host.Local() {
		var missing []string
		for _, c := range []string{"cpu", "memory", "pids"} {
			if !pr.controllers[c] {
				missing = append(missing, c)
			}
		}
		what := ""
		switch {
		case !pr.cgroupRead:
			what = "the delegated cgroup controllers cannot be read"
		case len(missing) > 0:
			what = "the cgroup v2 controllers " + strings.Join(missing, ", ") + " are not delegated to this user"
		}
		if what != "" {
			out = append(out, what+", so container limits will fail: "+
				"delegate them with /etc/systemd/system/user@.service.d/delegate.conf containing "+
				"\"[Service]\" and \"Delegate=cpu cpuset io memory pids\", then run sudo systemctl daemon-reload and log in again or reboot "+
				"(https://rootlesscontaine.rs/getting-started/common/cgroup2/), "+
				"or set the repository variable NIBFIN_CONTAINER_LIMITS=off")
		}
	}
	if a.host.WSL() {
		need := runnersOnHost * memoryPerRunnerGB
		out = append(out, "a WSL podman machine cannot apply per-container limits: set the repository variable NIBFIN_CONTAINER_LIMITS=off")
		msg := fmt.Sprintf("cap the VM memory with [wsl2] memory= in %%UserProfile%%\\.wslconfig, leaving at least %d GB for %d runners (%d GB each, the NIBFIN_MEMORY default), then run wsl --shutdown", need, runnersOnHost, memoryPerRunnerGB)
		if pr.memKB > 0 {
			gb := float64(pr.memKB) / (1 << 20)
			if pr.memKB < int64(need)<<20 {
				msg += fmt.Sprintf("; the VM has %.1f GB, below that minimum", gb)
			} else {
				msg += fmt.Sprintf("; the VM has %.1f GB", gb)
			}
		}
		out = append(out, msg)
	}
	return out
}

func (a *runnerAdd) probe(ctx context.Context) (hostProbe, error) {
	return probeHost(ctx, a.host)
}

// probeHost runs probeScript on h and parses its lines.
func probeHost(ctx context.Context, h runnerHost) (hostProbe, error) {
	out, err := runScript(ctx, h, "probe the runner host", probeScript, nil, "")
	if err != nil {
		return hostProbe{}, err
	}
	pr := hostProbe{controllers: map[string]bool{}}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		key, val, _ := strings.Cut(sc.Text(), " ")
		switch key {
		case "arch":
			pr.arch = val
		case "node":
			pr.node = val
		case "machine":
			pr.machine = strings.ToLower(val)
		case "linger":
			pr.linger = val == "1"
		case "cgroup":
			if val != "?" {
				pr.cgroupRead = true
				for _, c := range strings.Fields(val) {
					pr.controllers[c] = true
				}
			}
		case "memkb":
			pr.memKB, _ = strconv.ParseInt(val, 10, 64)
		case "memavailkb":
			pr.memAvailKB, _ = strconv.ParseInt(val, 10, 64)
		default:
			return pr, Errorf(CodeRunnerInstallFailed, "probe the runner host: unexpected line %q", sc.Text())
		}
	}
	return pr, nil
}

var notNameChar = regexp.MustCompile(`[^A-Za-z0-9-]+`)

// runnerName is <hostname>-<hostid>-<provider>-<n>: unique to this host, so
// --replace only ever replaces this slot's own earlier registration.
func (pr hostProbe) runnerName(slot string) (string, error) {
	if pr.machine == "" {
		return "", Errorf(CodeRunnerInstallFailed, "the runner host has no /etc/machine-id to name its runners by")
	}
	node := strings.Trim(notNameChar.ReplaceAllString(pr.node, "-"), "-")
	if len(node) > 32 {
		node = strings.Trim(node[:32], "-")
	}
	if node == "" {
		node = "host"
	}
	sum := sha256.Sum256([]byte("gh-nibfin runner\n" + pr.machine))
	return node + "-" + hex.EncodeToString(sum[:])[:8] + "-" + slot, nil
}

// script runs a runner-host script. Its stdout is returned; a failure is
// runner_install_failed with the script's stderr, or podman_failed when
// podman machine ssh itself failed. secret is scrubbed from the error.
func (a *runnerAdd) script(ctx context.Context, what, script string, stdin io.Reader, secret string, args ...string) (string, error) {
	return runScript(ctx, a.host, what, script, stdin, secret, args...)
}

func runScript(ctx context.Context, h runnerHost, what, script string, stdin io.Reader, secret string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	c := h.Shell(ctx, script, args...)
	c.Stdin = stdin
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	if err == nil {
		return stdout.String(), nil
	}
	if ctx.Err() != nil {
		return "", Errorf(CodeCancelled, "cancelled")
	}
	msg := stderrText(err, &stderr)
	if secret != "" {
		msg = strings.ReplaceAll(msg, secret, "[token]")
	}
	if !h.Local() && exitCode(err) == 125 {
		return "", Errorf(CodePodmanFailed, "podman machine ssh (%s): %s", what, msg)
	}
	return "", Errorf(CodeRunnerInstallFailed, "%s on %s: %s", what, h.Describe(), msg)
}

func (a *runnerAdd) Apply(ctx context.Context, plan Plan) error {
	if err := a.validate(); err != nil {
		return err
	}
	if err := a.detect(ctx); err != nil {
		return err
	}
	steps := plan.Steps
	var env *addEnv
	var err error
	if len(steps) > 0 && steps[0].Action == actStart {
		if err := a.host.Start(ctx); err != nil {
			return hostError(err)
		}
		if env, err = a.load(ctx); err != nil {
			return err
		}
		if steps, err = a.steps(ctx, env); err != nil {
			return err
		}
	} else {
		if env, err = a.load(ctx); err != nil {
			return err
		}
		if !strings.EqualFold(env.repo, plan.Repo) {
			return Errorf(CodeRunnerRepoConflict, "the plan registers to %q but the runners here use %s; plan again", plan.Repo, env.repo)
		}
	}

	// The order is fixed: linger, the tools volume, the login, the slots.
	var pre, logins []Step
	var slots []string
	slotRE := regexp.MustCompile(`^` + a.prov.name + `-[1-9][0-9]{0,5}$`)
	for _, s := range steps {
		switch {
		case s.Action == actLinger, s.Action == actCreateVolume && s.Target == toolsVolume:
			pre = append(pre, s)
		case s.Action == actCreateVolume && s.Target == a.prov.volume(), s.Action == actLogIn && s.Target == a.prov.name:
			logins = append(logins, s)
		case s.Action == actAddRunner && slotRE.MatchString(s.Target):
			slots = append(slots, s.Target)
		case s.Action == actAdvise:
		default:
			return Errorf(CodeInvalidArgument, "unknown runner add step %q %q", s.Action, s.Target)
		}
	}
	for _, s := range pre {
		if s.Action == actLinger {
			if _, err := a.script(ctx, "enable linger", lingerScript, nil, ""); err != nil {
				return Errorf(CodeRunnerInstallFailed, "%v; enable it with: sudo loginctl enable-linger $USER", err)
			}
			continue
		}
		if err := createVolume(ctx, a.host, toolsVolume); err != nil {
			return err
		}
	}
	if len(logins) > 0 {
		if err := a.login().Apply(ctx, Plan{Operation: "login", Steps: logins}); err != nil {
			return err
		}
	}
	return a.install(ctx, env, slots)
}

func (a *runnerAdd) install(ctx context.Context, env *addEnv, slots []string) error {
	needFiles := false
	for _, s := range slots {
		if r, ok := env.inv.Find(s); !ok || !r.Configured {
			needFiles = true
		}
	}
	var archive, token string
	if needFiles {
		d, err := a.pickDownload(ctx, env)
		if err != nil {
			return err
		}
		path, cleanup, err := downloadArchive(ctx, d)
		if err != nil {
			return err
		}
		defer cleanup()
		archive = path
		// Fetched after the login, so a long login cannot expire it.
		if token, err = env.gh.RegistrationToken(ctx, env.repo); err != nil {
			return err
		}
	}
	url := "https://" + env.repo
	labels := a.labels(env.user)
	for _, s := range slots {
		a.applied = append(a.applied, s)
		r, exists := env.inv.Find(s)
		dir := env.inv.SlotDir(s)
		if !r.Configured {
			name, err := env.probe.runnerName(s)
			if err != nil {
				return err
			}
			f, err := os.Open(archive)
			if err != nil {
				return Errorf(CodeRunnerDownloadFailed, "open the runner archive: %v", err)
			}
			mode := "existing"
			if !exists {
				mode = "new"
			}
			_, err = a.script(ctx, "extract the runner into "+s, extractScript, f, "", dir, mode)
			f.Close()
			if err != nil {
				return err
			}
			_, err = a.script(ctx, "configure "+s, configureScript, strings.NewReader(token+"\n"), token,
				dir, "--unattended", "--url", url, "--name", name, "--labels", labels, "--replace")
			if err != nil {
				return err
			}
		}
		unit, err := unitText(s, dir)
		if err != nil {
			return err
		}
		if _, err := a.script(ctx, "install the service of "+s, unitScript, strings.NewReader(unit), "", env.inv.UnitDir, runners.UnitName(s)); err != nil {
			return err
		}
	}
	return nil
}

func (a *runnerAdd) pickDownload(ctx context.Context, env *addEnv) (runnerDownload, error) {
	arch := map[string]string{"x86_64": "x64", "amd64": "x64", "aarch64": "arm64", "arm64": "arm64"}[env.probe.arch]
	if arch == "" {
		return runnerDownload{}, Errorf(CodeUnsupportedHost, "the runner host's architecture %q has no GitHub runner", env.probe.arch)
	}
	list, err := env.gh.Downloads(ctx, env.repo)
	if err != nil {
		return runnerDownload{}, err
	}
	for _, d := range list {
		if d.OS == "linux" && d.Architecture == arch {
			return d, nil
		}
	}
	return runnerDownload{}, Errorf(CodeRunnerDownloadFailed, "GitHub lists no linux-%s runner download", arch)
}

// unitText is the runner's own service template as a user unit: no User=,
// started by the user manager's default.target.
func unitText(slot, dir string) (string, error) {
	if strings.ContainsAny(dir, " \t\"'\\") || strings.ContainsFunc(dir, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", Errorf(CodeRunnerInstallFailed, "the runner directory %q has characters a unit file cannot hold", dir)
	}
	dir = strings.ReplaceAll(dir, "%", "%%")
	return "[Unit]\n" +
		"Description=Nibfin GitHub Actions runner " + slot + "\n" +
		"After=network.target\n\n" +
		"[Service]\n" +
		"ExecStart=" + dir + "/runsvc.sh\n" +
		"WorkingDirectory=" + dir + "\n" +
		"KillMode=process\n" +
		"KillSignal=SIGTERM\n" +
		"TimeoutStopSec=5min\n\n" +
		"[Install]\n" +
		"WantedBy=default.target\n", nil
}

// RunnerState is one runner of the host as read back: its local slot and
// service joined with what GitHub lists under its name.
type RunnerState struct {
	runners.Runner
	// Listed is true when GitHub lists a runner of this name.
	Listed bool   `json:"listed"`
	Status string `json:"status,omitempty"`
	Busy   bool   `json:"busy"`
}

func (a *runnerAdd) ReadBack(ctx context.Context, plan Plan) (Result, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	if err := a.detect(ctx); err != nil {
		return nil, err
	}
	if a.host.NeedsStart() {
		return nil, Errorf(CodePodmanFailed, "%s is not running", a.host.Describe())
	}
	inv, err := listRunners(ctx, a.host)
	if err != nil {
		return nil, err
	}
	repo, err := runners.Repo(inv.Runners)
	if err != nil {
		return nil, Wrap(CodeRunnerRepoConflict, err)
	}
	var list []ghRunner
	if repo != "" {
		gh, err := newGitHub(repoHost(repo))
		if err != nil {
			return nil, err
		}
		if list, err = gh.ListRunners(ctx, repo); err != nil {
			return nil, err
		}
	}

	// The slots to verify: those Apply acted on, else the plan's, else
	// (a plan made on a stopped machine) all of the provider's.
	planned := map[string]bool{}
	for _, s := range a.applied {
		planned[s] = true
	}
	if len(planned) == 0 {
		for _, s := range plan.Steps {
			if s.Action == actAddRunner {
				planned[s.Target] = true
			}
		}
	}
	if len(planned) == 0 {
		for _, r := range inv.Of(a.prov.name) {
			planned[r.Slot] = true
		}
	}

	states := joinRunners(inv.Runners, list)
	var missing []string
	for _, st := range states {
		r := st.Runner
		if planned[r.Slot] {
			delete(planned, r.Slot)
			// Offline right after the start is reported, not failed.
			if !r.Configured || !r.Active || !st.Listed {
				missing = append(missing, r.Slot)
			}
		}
	}
	for s := range planned {
		missing = append(missing, s)
	}

	l := a.login()
	in := false
	has, err := volumeExists(ctx, a.host, a.prov.volume())
	if err != nil {
		return nil, err
	}
	if has {
		if in, err = l.check(ctx, a.prov); err != nil {
			return nil, err
		}
	}
	pr, err := a.probe(ctx)
	if err != nil {
		return nil, err
	}
	r := Result{
		"host":    a.host.Describe(),
		"repo":    repo,
		"runners": states,
		"login":   map[string]any{a.prov.name: map[string]bool{"loggedIn": in}},
		"advice":  a.advice(pr, len(inv.Runners)),
	}
	if len(missing) > 0 {
		return r, Errorf(CodeRunnerMissing, "runners not configured, not running or not registered on GitHub: %s", strings.Join(missing, ", "))
	}
	if !in {
		return r, Errorf(CodeNotLoggedIn, "%s is not logged in after runner add", a.prov.name)
	}
	return r, nil
}

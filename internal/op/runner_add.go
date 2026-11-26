package op

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"filippo.io/age"

	"github.com/taco3064/gh-nibfin/internal/host"
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

	toolsVolume = "nibfin-tools"
	// memoryPerRunnerGB is the NIBFIN_MEMORY default of a stage run.
	memoryPerRunnerGB = 3
)

// The runner-host scripts. They are constants; only data travels as their
// arguments.
const (
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
	// unitDirScript gives the user ownership of the unit directory $1 and
	// its default.target.wants when they exist but are not writable, as
	// podman's WSL provisioning leaves them (created by root). It never
	// recurses, follows a symlinked directory or touches a file, and uses
	// sudo only without a prompt.
	unitDirScript = `set -e
for d in "$1" "$1/default.target.wants"; do
  if [ -d "$d" ] && [ ! -L "$d" ] && [ ! -w "$d" ]; then
    sudo -n chown "$(id -u):$(id -g)" "$d"
  fi
done
`
	// envScript sets NIBFIN_PROVIDER=$2 in the .env of the slot $1, which the
	// listener reads at start: every other line is kept, earlier
	// NIBFIN_PROVIDER lines are dropped, and the file is replaced atomically.
	envScript = `set -e
f="$1/.env"
if [ -f "$f" ]; then grep -v '^NIBFIN_PROVIDER=' "$f" > "$f.tmp" || [ $? -eq 1 ]; else : > "$f.tmp"; fi
printf 'NIBFIN_PROVIDER=%s\n' "$2" >> "$f.tmp"
mv -f "$f.tmp" "$f"
`
	// envCheckScript prints yes when the .env of the slot $1 has the line
	// NIBFIN_PROVIDER=$2, otherwise no.
	envCheckScript = `if grep -qxF "NIBFIN_PROVIDER=$2" "$1/.env" 2>/dev/null; then echo yes; else echo no; fi
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
	// entry is the autostart entry as Plan read it.
	entry host.Autostart
	// wslPending is true when Apply set the .wslconfig key while the VM
	// ran, so it takes effect after the next wsl --shutdown.
	wslPending bool
	// route is routeAdmin or routeRequest, as load found it.
	route string
	// key is the SSH key the request route uses; req is the request Issue
	// Apply opened; registered are the runner names it configured.
	key        *localKey
	req        *runnerRequest
	registered []string
	// deployPath and deploySent are the deploy key Apply wrote or reused
	// and the public key line it sent (private repositories only).
	deployPath, deploySent string
	// hookWant are the job-started hook files Plan or Apply expected on
	// the host (request route).
	hookWant map[string][]byte
}

// addEnv is what runner add learns from the host and GitHub.
type addEnv struct {
	inv   *runners.Inventory
	probe hostProbe
	repo  string
	gh    github
	user  ghUser
	// admin is true when the gh login is admin of repo (the admin route);
	// otherwise it has write access (the request route).
	admin bool
	// private is true when repo is private: the request route then also
	// sends a read-only deploy key.
	private bool
	// key is the matched SSH key, once the request route needs it.
	key *localKey
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
	Checking(ctx, "host")
	if err := a.detect(ctx); err != nil {
		return plan, err
	}
	where := a.host.Describe()
	hostSteps, err := a.hostSteps(ctx)
	if err != nil {
		return plan, err
	}
	plan.Steps = append(plan.Steps, hostSteps...)
	Checked(ctx, hostSteps...)
	if a.host.NeedsStart() {
		// Nothing can be asked of a stopped machine; Apply derives the
		// rest after the start.
		start := hostStep(where, true)
		if a.entry.Kind != "" {
			start.Detail = "through " + entryName(a.entry)
		}
		linger, tools := lingerStep(where), volumeStep(toolsVolume)
		linger.AfterStart, tools.AfterStart, tools.Detail = true, true, where
		plan.Steps = append(plan.Steps, start, linger, tools)
		Checked(ctx, start, linger, tools)
		logins, err := a.loginSteps(ctx)
		if err != nil {
			return plan, err
		}
		plan.Steps = append(plan.Steps, logins...)
		later := Step{ID: "runners", Action: actAddRunners, Target: fmt.Sprintf("%s ×%d", a.prov.name, a.p.Count),
			Detail: "repository and slots chosen after start", Next: "configured and registered", AfterStart: true}
		plan.Steps = append(plan.Steps, later)
		Checked(ctx, later)
		return plan, nil
	}
	plan.Steps = append(plan.Steps, hostStep(where, false))
	Checked(ctx, hostStep(where, false))
	env, err := a.load(ctx)
	if err != nil {
		return plan, err
	}
	plan.Repo = env.repo
	steps, notes, err := a.steps(ctx, env)
	plan.Steps = append(plan.Steps, steps...)
	plan.Notes = notes
	return plan, err
}

// loginSteps are the login plan's items runner add keeps: all but the
// machine start, which runner add plans itself. The nested plan reports its
// checks only; the kept items are reported here.
func (a *runnerAdd) loginSteps(ctx context.Context) ([]Step, error) {
	lp, err := a.login().Plan(withoutChecked(ctx))
	if err != nil {
		return nil, err
	}
	steps := []Step{}
	for _, s := range lp.Steps {
		if s.Action != actStart {
			steps = append(steps, s)
		}
	}
	Checked(ctx, steps...)
	return steps, nil
}

// The item steps of runner add that runner remove and the read-back share.
func wslStep() Step {
	return Step{ID: "wslconfig", Action: actSetWSLConfig, Target: host.WSLConfig, Next: "set"}
}

func autostartStep(action string, as host.Autostart) Step {
	return Step{ID: "autostart", Action: action, Target: entryName(as)}
}

func lingerStep(where string) Step {
	return Step{ID: "linger", Action: actLinger, Target: where, Next: "enabled"}
}

func slotStep(slot string) Step {
	return Step{ID: "runner:" + slot, Action: actAddRunner, Target: slot}
}

// entryState names an autostart entry's state.
func entryState(as host.Autostart) string {
	switch {
	case as.Current:
		return "current"
	case as.Present:
		return "outdated"
	}
	return "absent"
}

// hostSteps are the host items that keep the machine and its runners up
// while the host is on: the .wslconfig key on a WSL machine and the
// autostart entry, each no change when in place. They only read.
func (a *runnerAdd) hostSteps(ctx context.Context) ([]Step, error) {
	steps := []Step{}
	if a.host.WSL() {
		Checking(ctx, host.WSLConfig)
		if err := a.host.CheckWSL(ctx); err != nil {
			return nil, autostartError(ctx, err)
		}
		set, err := a.host.IdleTimeoutSet()
		if err != nil {
			return nil, autostartError(ctx, err)
		}
		s := wslStep()
		s.Detail, s.Current, s.NoChange = wslConfigValue, "not set", set
		if set {
			s.Current = "set"
		} else if !a.host.NeedsStart() {
			s.Detail += "; takes effect after the next wsl --shutdown; runner add does not shut down the VM"
		}
		steps = append(steps, s)
	}
	Checking(ctx, "autostart entry")
	as, err := a.host.Autostart(ctx)
	if err != nil {
		return nil, autostartError(ctx, err)
	}
	a.entry = as
	if as.Kind != "" {
		s := autostartStep(actRegisterAutostart, as)
		s.Detail, s.Current, s.Next, s.NoChange = entryTriggers(as.Kind), entryState(as), "current", as.Current
		if as.Present && !as.Current {
			s.Detail += "; replaces the registered one: " + as.Outdated
		}
		steps = append(steps, s)
	}
	return steps, nil
}

// applyHostSteps applies the plan's changing host steps.
func (a *runnerAdd) applyHostSteps(ctx context.Context, steps []Step) error {
	for _, s := range steps {
		var err error
		switch s.Action {
		case actSetWSLConfig:
			err = Track(ctx, s, func() error {
				changed, err := a.host.EnsureIdleTimeout()
				if err != nil {
					return autostartError(ctx, err)
				}
				if changed && !a.host.NeedsStart() {
					a.wslPending = true
				}
				return nil
			})
		case actRegisterAutostart:
			err = Track(ctx, s, func() error {
				as, err := a.host.EnsureAutostart(ctx)
				if err != nil {
					return autostartError(ctx, err)
				}
				if as.Kind != "" && !as.Current {
					return Errorf(CodeAutostartFailed, "%s was registered, but reads back different: %s", entryName(as), as.Outdated)
				}
				return nil
			})
		}
		if err != nil {
			return err
		}
	}
	return nil
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
	Checking(ctx, "GitHub login")
	user, err := gh.User(ctx)
	if err != nil {
		return nil, err
	}
	Checking(ctx, "permissions on "+repo)
	perms, err := gh.RepoPermissions(ctx, repo)
	if err != nil {
		return nil, err
	}
	if !perms.Admin && !perms.Push {
		return nil, Errorf(CodeRunnerRequestRefused, "%s has no write access to %s, so it cannot add Runners there; ask the owner of %s to add %s as a collaborator with write access, then run gh nibfin runner add again",
			user.Login, repo, repo, user.Login)
	}
	a.route = routeRequest
	if perms.Admin {
		a.route = routeAdmin
	}
	if !perms.Admin {
		// The job-started hook reads github.com only, and a private
		// repository with git over SSH; both stop before any write.
		if _, _, err := hookRepo(repo); err != nil {
			return nil, err
		}
		if perms.Private && (!pr.git || !pr.ssh) {
			return nil, Errorf(CodeUnsupportedHost, "%s is private, so the job-started hook reads it with git and ssh, which are not both installed on %s: install git and the OpenSSH client on the runner host (inside the podman machine VM when there is one), then run gh nibfin runner add again",
				repo, a.host.Describe())
		}
	}
	return &addEnv{inv: inv, probe: pr, repo: repo, gh: gh, user: user, admin: perms.Admin, private: perms.Private}, nil
}

// runnerLabels are the custom labels of a runner: nibfin-<user id>, which
// the stage Job of an Issue assigned to the user runs on, and for an admin
// also nibfin, which routing runs on. runner add registers them; upgrade
// sets them on the runners already registered.
func runnerLabels(admin bool, userID int64) []string {
	own := fmt.Sprintf("nibfin-%d", userID)
	if admin {
		return []string{nibfinLabel, own}
	}
	return []string{own}
}

// labels are the custom labels of a new runner (runnerLabels). GitHub's
// default labels, self-hosted among them, stay: --no-default-labels is never
// passed.
func (a *runnerAdd) labels(env *addEnv) string {
	return strings.Join(runnerLabels(env.admin, env.user.ID), ",")
}

// needFiles reports whether a slot needs the archive and a registration
// token: one not configured yet.
func needFiles(inv *runners.Inventory, slots []string) bool {
	for _, s := range slots {
		if r, ok := inv.Find(s); !ok || !r.Configured {
			return true
		}
	}
	return false
}

// sshKey finds the request route's SSH key once.
func (a *runnerAdd) sshKey(ctx context.Context, env *addEnv) (*localKey, error) {
	if env.key == nil {
		k, err := findSSHKey(ctx, env.gh, env.user.Login)
		if err != nil {
			return nil, err
		}
		env.key, a.key = k, k
	}
	return env.key, nil
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

// steps are the items of a running host: linger, the tools volume, the
// login items and one item per chosen slot. The host advice is returned as
// notes.
func (a *runnerAdd) steps(ctx context.Context, env *addEnv) ([]Step, []string, error) {
	where := a.host.Describe()
	steps := []Step{}
	linger := lingerStep(where)
	linger.NoChange, linger.Current = env.probe.linger, "disabled"
	if env.probe.linger {
		linger.Current = "enabled"
	}
	steps = append(steps, linger)
	Checked(ctx, linger)
	has, err := volumeExists(ctx, a.host, toolsVolume)
	if err != nil {
		return nil, nil, err
	}
	tools := volumeStep(toolsVolume)
	tools.Detail, tools.Current, tools.NoChange = where, presence(has), has
	steps = append(steps, tools)
	Checked(ctx, tools)
	logins, err := a.loginSteps(ctx)
	if err != nil {
		return nil, nil, err
	}
	steps = append(steps, logins...)
	reported := len(steps)
	slots, fresh := a.slots(env.inv)
	request := !env.admin && needFiles(env.inv, slots)
	if request {
		Checking(ctx, "SSH key")
		k, err := a.sshKey(ctx, env)
		if err != nil {
			return nil, nil, err
		}
		steps = append(steps, sshKeyStep(k))
		if env.private {
			path := runners.DeployKeyPath(env.inv, env.repo)
			pub, err := a.readDeployKey(ctx, path)
			if err != nil {
				return nil, nil, err
			}
			steps = append(steps, deployKeyStep(path, pub))
		}
		steps = append(steps, requestStep(env.repo, k.id, env.private))
	}
	detail := env.repo + ", labels " + a.labels(env) + ", .env NIBFIN_PROVIDER=" + a.prov.name
	var notes []string
	if !env.admin {
		Checking(ctx, "job-started hook")
		hook, err := a.planHook(ctx, env)
		if err != nil {
			return nil, nil, err
		}
		steps = append(steps, hook)
		detail += ", job-started hook"
		note, err := a.unhookedNote(ctx, env, slots)
		if err != nil {
			return nil, nil, err
		}
		if note != "" {
			notes = append(notes, note)
		}
	}
	for _, s := range slots {
		st := slotStep(s)
		st.Detail, st.Current, st.Next = detail, "absent", "configured and registered to "+env.repo
		if r, ok := env.inv.Find(s); ok {
			st.Current = slotCondition(r)
		}
		steps = append(steps, st)
	}
	if request {
		steps = append(steps, requestCloseStep(env.repo))
	}
	Checked(ctx, steps[reported:]...)
	return steps, append(a.advice(env.probe, len(env.inv.Runners)+fresh), notes...), nil
}

// slotCondition names an existing slot's local state.
func slotCondition(r runners.Runner) string {
	switch {
	case !r.Configured:
		return "incomplete: not configured"
	case !r.Unit:
		return "incomplete: no service unit"
	case !r.Enabled:
		return "incomplete: service not enabled"
	}
	return "configured, service enabled"
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
	return probeHost(ctx, a.host, CodeRunnerInstallFailed)
}

// script runs a runner-host script; a failure is runner_install_failed.
func (a *runnerAdd) script(ctx context.Context, what, script string, stdin io.Reader, secret string, args ...string) (string, error) {
	out, _, err := hostScript(ctx, a.host, CodeRunnerInstallFailed, what, script, stdin, secret, args...)
	return out, err
}

func (a *runnerAdd) Apply(ctx context.Context, plan Plan) error {
	if err := a.validate(); err != nil {
		return err
	}
	if err := a.detect(ctx); err != nil {
		return err
	}
	steps := plan.Changes()
	for _, s := range steps {
		if (s.Action == actSetWSLConfig && s.Target != host.WSLConfig) || (s.Action == actRegisterAutostart && s.Target == "") {
			return Errorf(CodeInvalidArgument, "unknown runner add step %q %q", s.Action, s.Target)
		}
	}
	start, stopped := findStep(steps, actStart)
	var env *addEnv
	var w work
	var id age.Identity
	// prepare loads what the items need and, on the request route, unlocks
	// the SSH key; it only reads (and asks for the passphrase).
	prepare := func() error {
		var err error
		if w, err = a.classify(steps); err != nil {
			return err
		}
		if !env.admin && needFiles(env.inv, targets(w.slots)) {
			k, err := a.sshKey(ctx, env)
			if err != nil {
				return err
			}
			if id, err = unlockSSHKey(k, a.p.Terminal); err != nil {
				return err
			}
		}
		return nil
	}
	if !stopped {
		var err error
		if env, err = a.load(ctx); err != nil {
			return err
		}
		if !strings.EqualFold(env.repo, plan.Repo) {
			return Errorf(CodeRunnerRepoConflict, "the plan registers to %q but the runners here use %s; plan again", plan.Repo, env.repo)
		}
		if err := prepare(); err != nil {
			return err
		}
	}
	// The host steps come first; none needs the VM.
	if err := a.applyHostSteps(ctx, steps); err != nil {
		return err
	}
	if stopped {
		// Started through the autostart entry, so the VM does not depend
		// on the process that ran gh.
		if err := Track(ctx, start, func() error {
			if err := a.host.StartViaAutostart(ctx); err != nil {
				return autostartError(ctx, err)
			}
			return nil
		}); err != nil {
			return err
		}
		var err error
		if env, err = a.load(ctx); err != nil {
			return err
		}
		if steps, _, err = a.steps(ctx, env); err != nil {
			return err
		}
		steps = Changes(steps)
		if err := prepare(); err != nil {
			return err
		}
	}

	// The order is fixed: linger, the tools volume, the login, the slots.
	for _, s := range w.pre {
		if s.Action == actLinger {
			if err := Track(ctx, s, func() error {
				if _, err := a.script(ctx, "enable linger", lingerScript, nil, ""); err != nil {
					return Errorf(CodeRunnerInstallFailed, "%v; enable it with: sudo loginctl enable-linger $USER", err)
				}
				return nil
			}); err != nil {
				return err
			}
			continue
		}
		if err := Track(ctx, s, func() error { return createVolume(ctx, a.host, toolsVolume) }); err != nil {
			return err
		}
	}
	if len(w.logins) > 0 {
		if err := a.login().Apply(ctx, Plan{Operation: "login", Steps: w.logins}); err != nil {
			return err
		}
	}
	// The hook goes before any slot, so no .env names a hook this run has
	// not written.
	if w.hook != nil {
		if env.admin || w.hook.Target != runners.HookScript(env.inv) {
			return Errorf(CodeInvalidArgument, "unknown runner add step %q %q", w.hook.Action, w.hook.Target)
		}
		if err := Track(ctx, *w.hook, func() error { return a.writeHook(ctx, env) }); err != nil {
			return err
		}
	}
	src := slotSource{
		download: func(ctx context.Context) (runnerDownload, error) { return a.pickDownload(ctx, env) },
		// Fetched after the login, so a long login cannot expire it.
		token: func(ctx context.Context) (string, error) { return env.gh.RegistrationToken(ctx, env.repo) },
	}
	if !env.admin {
		src.download = func(ctx context.Context) (runnerDownload, error) {
			arch, err := runnerArch(env.probe)
			if err != nil {
				return runnerDownload{}, err
			}
			return releaseDownload(ctx, arch)
		}
		src.token = func(ctx context.Context) (string, error) {
			// A private repository is read with a deploy key, created
			// (or reused) on the host before the request carries it.
			deployKey := ""
			if env.private {
				var err error
				if deployKey, err = a.deployKey(ctx, env); err != nil {
					return "", err
				}
			}
			return a.requestToken(ctx, env, id, deployKey)
		}
	}
	err := a.install(ctx, env, w.slots, src)
	return a.finishRequest(ctx, env, err)
}

// work is the plan's runner items in the order Apply applies them.
type work struct {
	pre, logins, slots []Step
	// hook is the job-started hook item, when it changes.
	hook *Step
}

// classify sorts the changing steps into the runner items; the host items
// and the request items are applied separately. Any other step is refused.
func (a *runnerAdd) classify(steps []Step) (work, error) {
	var w work
	slotRE := regexp.MustCompile(`^` + a.prov.name + `-[1-9][0-9]{0,5}$`)
	for _, s := range steps {
		switch {
		case s.Action == actLinger, s.Action == actCreateVolume && s.Target == toolsVolume:
			w.pre = append(w.pre, s)
		case s.Action == actCreateVolume && s.Target == a.prov.volume(), s.Action == actLogIn && s.Target == a.prov.name:
			w.logins = append(w.logins, s)
		case s.Action == actAddRunner && slotRE.MatchString(s.Target):
			w.slots = append(w.slots, s)
		case s.Action == actInstallHook:
			h := s
			w.hook = &h
		case s.Action == actSetWSLConfig, s.Action == actRegisterAutostart, s.Action == actStart,
			s.Action == actRequestRegistration, s.Action == actCloseRequest, s.Action == actCreateDeployKey:
		default:
			return w, Errorf(CodeInvalidArgument, "unknown runner add step %q %q", s.Action, s.Target)
		}
	}
	return w, nil
}

func targets(steps []Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Target
	}
	return out
}

// findStep returns the first step with action.
func findStep(steps []Step, action string) (Step, bool) {
	for _, s := range steps {
		if s.Action == action {
			return s, true
		}
	}
	return Step{}, false
}

// slotSource supplies what new slots need: the runner archive's download
// and the registration token, by route.
type slotSource struct {
	download func(context.Context) (runnerDownload, error)
	token    func(context.Context) (string, error)
}

// install installs each slot as one item: its files, its registration, its
// .env and its service. The archive and the registration token they share
// are fetched first, only when a slot is not configured yet; when that
// fails, every slot fails with it.
func (a *runnerAdd) install(ctx context.Context, env *addEnv, slots []Step, src slotSource) error {
	failAll := func(err error) error {
		for _, s := range slots {
			Started(ctx, s)
			Failed(ctx, s, err)
		}
		return err
	}
	var archive, token string
	if needFiles(env.inv, targets(slots)) {
		d, err := src.download(ctx)
		if err != nil {
			return failAll(err)
		}
		path, cleanup, err := downloadArchive(ctx, d)
		if err != nil {
			return failAll(err)
		}
		defer cleanup()
		archive = path
		if token, err = src.token(ctx); err != nil {
			return failAll(err)
		}
	}
	url := "https://" + env.repo
	labels := a.labels(env)
	for _, st := range slots {
		s := st.Target
		err := Track(ctx, st, func() error {
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
				a.registered = append(a.registered, name)
			}
			// After config.sh, whose env.sh keeps the line, and before the
			// service first starts and reads .env.
			if _, err := a.script(ctx, "set the provider in the .env of "+s, envScript, nil, "", dir, a.prov.name); err != nil {
				return err
			}
			if !env.admin {
				if _, err := a.script(ctx, "set the job-started hook in the .env of "+s, hookEnvScript, nil, "", dir, runners.HookScript(env.inv)); err != nil {
					return err
				}
			}
			unit, err := unitText(s, dir)
			if err != nil {
				return err
			}
			if !a.host.Local() {
				if err := a.unitDir(ctx, s, env.inv.UnitDir); err != nil {
					return err
				}
			}
			_, err = a.script(ctx, "install the service of "+s, unitScript, strings.NewReader(unit), "", env.inv.UnitDir, runners.UnitName(s))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// unitDir makes the unit directory dir usable by the VM user for the slot s
// on a machine host; a failure names the remedy inside the VM.
func (a *runnerAdd) unitDir(ctx context.Context, s, dir string) error {
	_, err := a.script(ctx, "make the unit directory of "+s+" writable", unitDirScript, nil, "", dir)
	if err != nil {
		return Errorf(CodeRunnerInstallFailed, "%w; inside the VM, run: sudo chown $(id -u):$(id -g) %s %s/default.target.wants", err, dir, dir)
	}
	return nil
}

// runnerArch is the GitHub runner architecture of the host.
func runnerArch(pr hostProbe) (string, error) {
	arch := map[string]string{"x86_64": "x64", "amd64": "x64", "aarch64": "arm64", "arm64": "arm64"}[pr.arch]
	if arch == "" {
		return "", Errorf(CodeUnsupportedHost, "the runner host's architecture %q has no GitHub runner", pr.arch)
	}
	return arch, nil
}

func (a *runnerAdd) pickDownload(ctx context.Context, env *addEnv) (runnerDownload, error) {
	arch, err := runnerArch(env.probe)
	if err != nil {
		return runnerDownload{}, err
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

// slotReadBack names a slot's state as read back, and what keeps it from
// being confirmed. On the request route GitHub's runner list needs admin
// and is not read. env is the .env check, nil when not checked.
func slotReadBack(st RunnerState, request bool, env, hook *bool, provider string) (state, missing string) {
	r := st.Runner
	var parts, miss []string
	if r.Configured {
		parts = append(parts, "configured")
	} else {
		miss = append(miss, "not configured")
	}
	if r.Active {
		parts = append(parts, "active")
	} else {
		miss = append(miss, "service not active")
	}
	if env != nil {
		if *env {
			parts = append(parts, ".env NIBFIN_PROVIDER="+provider)
		} else {
			miss = append(miss, ".env lacks NIBFIN_PROVIDER="+provider)
		}
	}
	if hook != nil {
		if *hook {
			parts = append(parts, ".env names the job-started hook")
		} else {
			miss = append(miss, ".env lacks the job-started hook")
		}
	}
	switch {
	case request:
		parts = append(parts, "GitHub's runner list needs admin and was not read")
	case st.Listed:
		listed := "listed on GitHub"
		if st.Status != "" {
			listed += " (" + st.Status + ")"
		}
		parts = append(parts, listed)
	default:
		miss = append(miss, "not registered on GitHub")
	}
	return strings.Join(parts, ", "), strings.Join(miss, ", ")
}

// envHasProvider reads back whether the slot's .env names the provider.
func (a *runnerAdd) envHasProvider(ctx context.Context, dir string) (bool, error) {
	out, err := a.script(ctx, "read the .env of "+dir, envCheckScript, nil, "", dir, a.prov.name)
	return strings.TrimSpace(out) == "yes", err
}

// ReadBack reads every item back: the host, its .wslconfig key and
// autostart entry, linger, the tools volume, the login and each slot. The
// items read so far are returned also with an error.
func (a *runnerAdd) ReadBack(ctx context.Context, plan Plan) (Result, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	if err := a.detect(ctx); err != nil {
		return nil, err
	}
	where := a.host.Describe()
	var checks Checks
	ws, hs, ls, tools := wslStep(), hostStep(where, false), lingerStep(where), volumeStep(toolsVolume)
	as0 := autostartStep(actRegisterAutostart, a.entry)
	if a.host.WSL() {
		checks.Expect(ws)
	}
	if a.entry.Kind != "" {
		checks.Expect(as0)
	}
	checks.Expect(hs, ls, tools, volumeStep(a.prov.volume()), loginStep(a.prov))

	// The slots to verify: those Apply acted on, else the plan's, else
	// (a plan made on a stopped machine) all of the provider's.
	planned := map[string]bool{}
	for _, s := range a.applied {
		planned[s] = true
	}
	if len(planned) == 0 {
		for _, s := range plan.Changes() {
			if s.Action == actAddRunner {
				planned[s.Target] = true
			}
		}
	}
	for _, s := range a.applied {
		checks.Expect(slotStep(s))
	}
	for _, s := range plan.Changes() {
		if len(a.applied) == 0 && s.Action == actAddRunner {
			checks.Expect(slotStep(s.Target))
		}
	}
	r := Result{"host": where}
	fail := func(err error) (Result, error) { return checks.Fill(r, nil, err), err }

	if a.host.NeedsStart() {
		checks.Set(hs, false, "stopped", where+" is not running")
		return fail(Errorf(CodePodmanFailed, "%s is not running", where))
	}
	checks.Set(hs, true, "running", "")
	inv, err := listRunners(ctx, a.host)
	if err != nil {
		return fail(err)
	}
	// The .env is checked on the slots this run acted on or planned;
	// complete runners from before keep theirs as they are.
	checkEnv := len(planned) > 0
	if len(planned) == 0 {
		for _, rn := range inv.Of(a.prov.name) {
			planned[rn.Slot] = true
			checks.Expect(slotStep(rn.Slot))
		}
	}
	repo, err := runners.Repo(inv.Runners)
	if err != nil {
		return fail(Wrap(CodeRunnerRepoConflict, err))
	}
	var list []ghRunner
	request := false
	if repo != "" {
		gh, err := newGitHub(repoHost(repo))
		if err != nil {
			return fail(err)
		}
		route := a.route
		if route == "" {
			perms, err := gh.RepoPermissions(ctx, repo)
			if err != nil {
				return fail(err)
			}
			if route = routeRequest; perms.Admin {
				route = routeAdmin
			}
		}
		request = route == routeRequest
		r["route"] = route
		if !request {
			if list, err = gh.ListRunners(ctx, repo); err != nil {
				return fail(err)
			}
		}
		if err := a.readBackRequest(ctx, gh, repo, plan, &checks, r); err != nil {
			return fail(err)
		}
		if request {
			if err := a.readBackHook(ctx, gh, inv, repo, &checks, r); err != nil {
				return fail(err)
			}
		}
	}

	states := joinRunners(inv.Runners, list)
	var missing []string
	for _, st := range states {
		rn := st.Runner
		if planned[rn.Slot] {
			delete(planned, rn.Slot)
			var env, hook *bool
			if checkEnv && rn.Configured {
				ok, err := a.envHasProvider(ctx, inv.SlotDir(rn.Slot))
				if err != nil {
					return fail(err)
				}
				env = &ok
				if request {
					hooked, err := hookInEnv(ctx, a.host, CodeRunnerInstallFailed, inv, rn.Slot)
					if err != nil {
						return fail(err)
					}
					hook = &hooked
				}
			}
			// Offline right after the start is reported, not failed.
			state, miss := slotReadBack(st, request, env, hook, a.prov.name)
			checks.Set(slotStep(rn.Slot), miss == "", state, miss)
			if miss != "" {
				missing = append(missing, rn.Slot)
			}
		}
	}
	var gone []string
	for s := range planned {
		gone = append(gone, s)
	}
	sort.Strings(gone)
	for _, s := range gone {
		missing = append(missing, s)
		checks.Set(slotStep(s), false, "absent", "not on "+where)
	}
	r["repo"], r["runners"] = repo, states

	l := a.login()
	in := false
	has, err := volumeExists(ctx, a.host, a.prov.volume())
	if err != nil {
		return fail(err)
	}
	checks.Set(volumeStep(a.prov.volume()), has, presence(has), "the volume does not exist")
	if has {
		if in, err = l.check(ctx, a.prov); err != nil {
			return fail(err)
		}
	}
	checks.Set(loginStep(a.prov), in, loggedIn(in), a.prov.name+" is not logged in")
	r["login"] = map[string]any{a.prov.name: map[string]bool{"loggedIn": in}}
	pr, err := a.probe(ctx)
	if err != nil {
		return fail(err)
	}
	linger := "disabled"
	if pr.linger {
		linger = "enabled"
	}
	checks.Set(ls, pr.linger, linger, "linger is not enabled")
	toolsHas, err := volumeExists(ctx, a.host, toolsVolume)
	if err != nil {
		return fail(err)
	}
	checks.Set(tools, toolsHas, presence(toolsHas), "the volume does not exist")
	notes := a.advice(pr, len(inv.Runners))
	r["advice"] = notes
	as, err := a.host.Autostart(ctx)
	if err != nil {
		return fail(autostartError(ctx, err))
	}
	r["autostart"] = as
	if as.Kind != "" {
		why := "not registered"
		if as.Present {
			why = "differs: " + as.Outdated
		}
		checks.Set(autostartStep(actRegisterAutostart, as), as.Current, entryState(as), why)
	}
	wslSet := true
	if a.host.WSL() {
		if wslSet, err = a.host.IdleTimeoutSet(); err != nil {
			return fail(autostartError(ctx, err))
		}
		w := map[string]string{"vmIdleTimeout": "", "effective": ""}
		state := "not set"
		if wslSet {
			w["vmIdleTimeout"], w["effective"] = "-1", "now"
			state = "set, effective now"
			if a.wslPending {
				w["effective"] = "after wsl --shutdown"
				state = "set, effective after wsl --shutdown"
			}
		}
		r["wslconfig"] = w
		checks.Set(ws, wslSet, state, "does not set "+wslConfigValue)
	}
	checks.Fill(r, notes, nil)
	if len(missing) > 0 {
		return r, Errorf(CodeRunnerMissing, "runners not configured, not running or not registered on GitHub: %s", strings.Join(missing, ", "))
	}
	if !in {
		return r, Errorf(CodeNotLoggedIn, "%s is not logged in after runner add", a.prov.name)
	}
	if as.Kind != "" && !as.Current {
		why := "is not registered"
		if as.Present {
			why = "differs: " + as.Outdated
		}
		return r, Errorf(CodeNotApplied, "the autostart %s %s after runner add", entryName(as), why)
	}
	if !wslSet {
		return r, Errorf(CodeNotApplied, "%s does not set %s after runner add", host.WSLConfig, wslConfigValue)
	}
	return r, nil
}

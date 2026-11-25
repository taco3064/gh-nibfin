package op

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/runners"
)

// RunnerRemoveParams are the inputs of runner remove. Count 0 removes all of
// the provider's runners on the host.
type RunnerRemoveParams struct {
	Provider    string `json:"provider"`
	Count       int    `json:"count"`
	DeleteLogin bool   `json:"deleteLogin"`
}

// RunnerRemove is the entry point of runner remove.
func RunnerRemove(p RunnerRemoveParams) Operation { return &runnerRemove{p: p} }

const (
	actRemoveRunner  = "remove runner"
	actRemoveRunners = "remove runners"
	actDeleteVolume  = "delete volume"
	actKeepVolume    = "keep volume"

	// nibfinLabel is the custom label every nibfin Job is routed by, besides
	// its own nibfin-<user>-<provider> label.
	nibfinLabel = "nibfin"
	// busyExit is stopScript's exit status when GitHub reports the runner
	// busy but no worker of it runs.
	busyExit = 75
)

// The runner-host scripts of runner remove. They are constants; only data
// travels as their arguments.
const (
	// stopScript stops the runner in the slot $1 (unit $3 in the unit
	// directory $2) once no Job runs on it. A Job runs as the slot's
	// Runner.Worker process; each one found is waited for until it exits,
	// then the slot is scanned again, and the service is stopped right after
	// a scan finds none. With $4 = 1 (GitHub reports the runner busy) a
	// first scan that finds no worker stops nothing and exits busyExit.
	stopScript = `r=$(cd -P -- "$1" 2>/dev/null && pwd -P) || r=$1
workers() {
  for p in /proc/[0-9]*; do
    e=$(readlink "$p/exe" 2>/dev/null) || continue
    case $e in
    "$r"/*/Runner.Worker|"$r"/*/"Runner.Worker (deleted)") printf '%s\n' "${p#/proc/}" ;;
    esac
  done
}
w=$(workers)
if [ -z "$w" ] && [ "$4" = 1 ]; then
  echo "GitHub reports the runner busy, but no Runner.Worker of it runs here" >&2
  exit 75
fi
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
	// purgeScript deletes the slot $2 in the runner directory $1 and its
	// unit in the unit directory $3. It refuses anything but a
	// <provider>-<n> slot in a .../nibfin/runners directory.
	purgeScript = `no() { echo "refusing to remove $1" >&2; exit 2; }
case $1 in /*/nibfin/runners) ;; *) no "$1" ;; esac
case $1 in */../*|*/./*) no "$1" ;; esac
case $3 in /*) ;; *) no "$3" ;; esac
case $2 in [a-z]*-[1-9]*) ;; *) no "$2" ;; esac
case ${2%-*} in *[!a-z]*) no "$2" ;; esac
case ${2##*-} in *[!0-9]*) no "$2" ;; esac
rm -rf -- "$1/$2" || exit 1
rm -f -- "$3/nibfin-runner-$2.service" || exit 1
systemctl --user daemon-reload
`
)

// Slot classes, in removal order.
const (
	classStale = "stale"
	classIdle  = "idle"
	classBusy  = "busy"
)

type runnerRemove struct {
	p    RunnerRemoveParams
	prov provider
	host runnerHost
	// removed are the slots Apply acted on, for ReadBack.
	removed []slotState
	// deleteVolume is true when the applied plan deletes the login volume.
	deleteVolume bool
}

// remoteRunner is a GitHub runner of a repository.
type remoteRunner struct {
	ghRunner
	repo   string
	client github
}

// slotState is a slot joined with its GitHub registration.
type slotState struct {
	runners.Runner
	// name is the name the slot is or would be registered under.
	name string
	// gh is its registration, nil when GitHub lists none.
	gh *remoteRunner
}

// busy reports whether GitHub reports the runner running a Job. An offline
// runner is not asked to show a worker.
func (s slotState) busy() bool { return s.gh != nil && s.gh.Busy && s.gh.Status != "offline" }

func (s slotState) class() string {
	switch {
	case !s.Complete() || s.gh == nil || !s.gh.hasCustomLabel(nibfinLabel):
		return classStale
	case s.busy():
		return classBusy
	}
	return classIdle
}

// removeEnv is what runner remove learns from the host and GitHub.
type removeEnv struct {
	inv *runners.Inventory
	// all are the host's slots, mine the provider's, in slot order.
	all, mine []slotState
	// lists are the GitHub runners of each repository asked.
	lists map[string][]ghRunner
	// clients are the GitHub clients by host.
	clients map[string]github
}

func (r *runnerRemove) validate() error {
	p, ok := lookupProvider(r.p.Provider)
	if !ok {
		return Errorf(CodeInvalidArgument, "unknown provider %q; use claude or codex", r.p.Provider)
	}
	if r.p.Count < 0 {
		return Errorf(CodeInvalidArgument, "count must be at least 1, got %d", r.p.Count)
	}
	r.prov = p
	return nil
}

func (r *runnerRemove) detect(ctx context.Context) error {
	if r.host != nil {
		return nil
	}
	h, err := detectHost(ctx)
	if err != nil {
		return err
	}
	r.host = h
	return nil
}

func (r *runnerRemove) Plan(ctx context.Context) (Plan, error) {
	plan := Plan{Operation: "runner remove", Steps: []Step{}, Confirm: true}
	if err := r.validate(); err != nil {
		return plan, err
	}
	if err := r.detect(ctx); err != nil {
		return plan, err
	}
	where := r.host.Describe()
	if r.host.NeedsStart() {
		// Nothing can be asked of a stopped machine; Apply derives the
		// slots after the start.
		n := "all"
		if r.p.Count > 0 {
			n = fmt.Sprintf("×%d", r.p.Count)
		}
		plan.Steps = append(plan.Steps,
			Step{Action: actStart, Target: where},
			Step{Action: actRemoveRunners, Target: r.prov.name + " " + n, Detail: "slots chosen after start"},
		)
		if r.p.DeleteLogin {
			plan.Steps = append(plan.Steps, Step{Action: actDeleteVolume, Target: r.prov.volume(), Detail: "if no " + r.prov.name + " runner remains"})
		}
		return plan, nil
	}
	env, err := r.load(ctx, nil)
	if err != nil {
		return plan, err
	}
	plan.Repo, plan.Steps, err = r.steps(ctx, env)
	if err != nil {
		return plan, err
	}
	plan.ReadOnly = true
	for _, s := range plan.Steps {
		if s.Action != actKeepVolume {
			plan.ReadOnly = false
		}
	}
	return plan, nil
}

// load discovers the slots and joins them with GitHub. Besides the
// repositories of the configured slots, extra repositories are asked too.
func (r *runnerRemove) load(ctx context.Context, extra []string) (*removeEnv, error) {
	inv, err := listRunners(ctx, r.host)
	if err != nil {
		return nil, err
	}
	pr, err := probeHost(ctx, r.host, CodeRunnerRemoveFailed)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var repos []string
	for _, repo := range extra {
		if repo != "" && !seen[strings.ToLower(repo)] {
			seen[strings.ToLower(repo)] = true
			repos = append(repos, repo)
		}
	}
	for _, s := range inv.Runners {
		if s.Repo != "" && !seen[strings.ToLower(s.Repo)] {
			seen[strings.ToLower(s.Repo)] = true
			repos = append(repos, s.Repo)
		}
	}
	sort.Strings(repos)
	env := &removeEnv{inv: inv, lists: map[string][]ghRunner{}, clients: map[string]github{}}
	for _, repo := range repos {
		h := repoHost(repo)
		gh, ok := env.clients[h]
		if !ok {
			if gh, err = newGitHub(h); err != nil {
				return nil, err
			}
			env.clients[h] = gh
		}
		if env.lists[repo], err = gh.ListRunners(ctx, repo); err != nil {
			return nil, err
		}
	}
	for _, s := range inv.Runners {
		st := env.match(s, pr, repos)
		env.all = append(env.all, st)
		if s.Provider == r.prov.name {
			env.mine = append(env.mine, st)
		}
	}
	return env, nil
}

// match finds the slot's registration by name: the name in .runner, or for
// a slot without one the name runner add gives it on this host. A slot that
// names its repository is looked up only there.
func (env *removeEnv) match(s runners.Runner, pr hostProbe, repos []string) slotState {
	st := slotState{Runner: s, name: s.Name}
	if st.name == "" {
		st.name, _ = pr.runnerName(s.Slot)
	}
	if st.name == "" {
		return st
	}
	if s.Repo != "" {
		repos = []string{s.Repo}
	}
	for _, repo := range repos {
		for _, g := range env.lists[repo] {
			if strings.EqualFold(g.Name, st.name) {
				st.gh = &remoteRunner{ghRunner: g, repo: repo, client: env.clients[repoHost(repo)]}
				return st
			}
		}
	}
	return st
}

// pick returns n slots: stale first, then idle, then busy, in slot order
// within each class.
func pick(slots []slotState, n int) []slotState {
	var out []slotState
	for _, c := range []string{classStale, classIdle, classBusy} {
		for _, s := range slots {
			if s.class() == c {
				out = append(out, s)
			}
		}
	}
	return out[:n]
}

func (r *runnerRemove) steps(ctx context.Context, env *removeEnv) (string, []Step, error) {
	where := r.host.Describe()
	k := len(env.mine)
	if r.p.Count > k {
		return "", nil, Errorf(CodeInvalidArgument, "only %d %s runners on %s", k, r.prov.name, where)
	}
	if k == 0 && !r.p.DeleteLogin {
		return "", nil, Errorf(CodeNoRunners, "no %s runners on %s", r.prov.name, where)
	}
	n := r.p.Count
	if n == 0 {
		n = k
	}
	steps := []Step{}
	repos := map[string]string{}
	for _, s := range pick(env.mine, n) {
		steps = append(steps, Step{Action: actRemoveRunner, Target: s.Slot, Detail: s.detail()})
		repo := s.Repo
		if s.gh != nil {
			repo = s.gh.repo
		}
		if repo != "" {
			repos[strings.ToLower(repo)] = repo
		}
	}
	if r.p.DeleteLogin {
		has, err := volumeExists(ctx, r.host, r.prov.volume())
		if err != nil {
			return "", nil, err
		}
		switch {
		case !has:
			steps = append(steps, Step{Action: actKeepVolume, Target: r.prov.volume(), Detail: "no such volume"})
		case k > n:
			steps = append(steps, Step{Action: actKeepVolume, Target: r.prov.volume(), Detail: fmt.Sprintf("%d %s runners remain", k-n, r.prov.name)})
		default:
			steps = append(steps, Step{Action: actDeleteVolume, Target: r.prov.volume(), Detail: where})
		}
	}
	repo := ""
	if len(repos) == 1 {
		for _, v := range repos {
			repo = v
		}
	}
	return repo, steps, nil
}

func (s slotState) detail() string {
	name := s.name
	if s.gh == nil {
		name += " (not registered)"
	}
	d := strings.TrimSpace(name) + ", " + s.class()
	if s.busy() {
		d += ": waits for its Job to end"
	}
	return d
}

func (r *runnerRemove) Apply(ctx context.Context, plan Plan) error {
	if err := r.validate(); err != nil {
		return err
	}
	if err := r.detect(ctx); err != nil {
		return err
	}
	steps := plan.Steps
	var env *removeEnv
	var err error
	if len(steps) > 0 && steps[0].Action == actStart {
		if err := r.host.Start(ctx); err != nil {
			return hostError(err)
		}
		if env, err = r.load(ctx, nil); err != nil {
			return err
		}
		if _, steps, err = r.steps(ctx, env); err != nil {
			return err
		}
	} else if env, err = r.load(ctx, nil); err != nil {
		return err
	}

	slotRE := regexp.MustCompile(`^` + regexp.QuoteMeta(r.prov.name) + `-[1-9][0-9]{0,5}$`)
	var chosen []slotState
	for _, s := range steps {
		switch {
		case s.Action == actRemoveRunner && slotRE.MatchString(s.Target):
			st, ok := find(env.mine, s.Target)
			if !ok {
				return Errorf(CodeInvalidArgument, "runner %s is no longer on %s; plan again", s.Target, r.host.Describe())
			}
			chosen = append(chosen, st)
		case s.Action == actDeleteVolume && s.Target == r.prov.volume():
			r.deleteVolume = true
		case s.Action == actKeepVolume && s.Target == r.prov.volume():
		default:
			return Errorf(CodeInvalidArgument, "unknown runner remove step %q %q", s.Action, s.Target)
		}
	}

	// Without its custom labels a runner takes no new nibfin Job.
	for _, s := range chosen {
		if s.gh != nil && s.gh.customLabels() {
			if err := s.gh.client.RemoveCustomLabels(ctx, s.gh.repo, s.gh.ID); err != nil {
				return err
			}
		}
	}
	if len(chosen) > 0 {
		// Read GitHub again: a Job may have started meanwhile.
		if env, err = r.load(ctx, nil); err != nil {
			return err
		}
	}
	var now []slotState
	for _, busy := range []bool{false, true} {
		for _, c := range chosen {
			s, ok := find(env.mine, c.Slot)
			if !ok {
				s = c
			}
			if s.busy() == busy {
				now = append(now, s)
			}
		}
	}
	var stuck []string
	for _, s := range now {
		if err := r.removeSlot(ctx, env.inv, s); err != nil {
			if Code(err) != CodeRunnerBusy {
				return err
			}
			stuck = append(stuck, s.Slot)
		}
	}
	if len(stuck) > 0 {
		return Errorf(CodeRunnerBusy, "GitHub reports %s busy, but no Job of it runs on %s; it takes no new nibfin Job now: run gh nibfin runner remove again to finish it", strings.Join(stuck, ", "), r.host.Describe())
	}
	if r.deleteVolume {
		return r.removeVolume(ctx)
	}
	return nil
}

func find(slots []slotState, slot string) (slotState, bool) {
	for _, s := range slots {
		if s.Slot == slot {
			return s, true
		}
	}
	return slotState{}, false
}

// removeSlot stops the slot's runner once no Job runs on it, deletes its
// registration, then its files. The registration goes before the files, so
// an interrupted run leaves a slot the next run finds.
func (r *runnerRemove) removeSlot(ctx context.Context, inv *runners.Inventory, s slotState) error {
	r.removed = append(r.removed, s)
	require := "0"
	if s.busy() {
		require = "1"
	}
	_, exit, err := hostScript(ctx, r.host, CodeRunnerRemoveFailed, "stop "+s.Slot, stopScript, nil, "",
		inv.SlotDir(s.Slot), inv.UnitDir, runners.UnitName(s.Slot), require)
	if exit == busyExit {
		return Errorf(CodeRunnerBusy, "%s is busy on GitHub, but no Job of it runs", s.Slot)
	}
	if err != nil {
		return err
	}
	if s.gh != nil {
		if err := s.gh.client.DeleteRunner(ctx, s.gh.repo, s.gh.ID); err != nil {
			return err
		}
	}
	_, _, err = hostScript(ctx, r.host, CodeRunnerRemoveFailed, "remove the files of "+s.Slot, purgeScript, nil, "",
		inv.Dir, s.Slot, inv.UnitDir)
	return err
}

// removeVolume deletes the login volume when no runner of the provider
// remains. A volume still in use is kept: podman refuses it.
func (r *runnerRemove) removeVolume(ctx context.Context) error {
	inv, err := listRunners(ctx, r.host)
	if err != nil {
		return err
	}
	if len(inv.Of(r.prov.name)) > 0 {
		return nil
	}
	has, err := volumeExists(ctx, r.host, r.prov.volume())
	if err != nil || !has {
		return err
	}
	code, stderr, err := quiet(ctx, r.host, "volume", "rm", r.prov.volume())
	if err != nil {
		return err
	}
	if code != 0 {
		return Errorf(CodePodmanFailed, "podman volume rm %s exited with status %d: %s", r.prov.volume(), code, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r *runnerRemove) ReadBack(ctx context.Context, plan Plan) (Result, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := r.detect(ctx); err != nil {
		return nil, err
	}
	if r.host.NeedsStart() {
		return nil, Errorf(CodePodmanFailed, "%s is not running", r.host.Describe())
	}
	var extra []string
	for _, s := range r.removed {
		if s.gh != nil {
			extra = append(extra, s.gh.repo)
		}
	}
	env, err := r.load(ctx, extra)
	if err != nil {
		return nil, err
	}
	states := []RunnerState{}
	for _, s := range env.all {
		st := RunnerState{Runner: s.Runner}
		if s.gh != nil {
			st.Listed, st.Status, st.Busy = true, s.gh.Status, s.gh.Busy
		}
		states = append(states, st)
	}
	removed := []string{}
	var left []string
	for _, s := range r.removed {
		removed = append(removed, s.Slot)
		_, local := env.inv.Find(s.Slot)
		listed := false
		if s.gh != nil {
			for _, g := range env.lists[s.gh.repo] {
				listed = listed || strings.EqualFold(g.Name, s.gh.Name)
			}
		}
		switch {
		case local && listed:
			left = append(left, s.Slot+" (still on the host and on GitHub)")
		case local:
			left = append(left, s.Slot+" (still on the host)")
		case listed:
			left = append(left, s.Slot+" (still registered on GitHub)")
		}
	}
	remaining := len(env.mine)
	has, err := volumeExists(ctx, r.host, r.prov.volume())
	if err != nil {
		return nil, err
	}
	login := map[string]string{"volume": "absent", "reason": "no such volume"}
	switch {
	case has && !r.p.DeleteLogin:
		login = map[string]string{"volume": "kept", "reason": "--delete-login was not given"}
	case has && remaining > 0:
		login = map[string]string{"volume": "kept", "reason": fmt.Sprintf("%d %s runners remain", remaining, r.prov.name)}
	case has:
		login = map[string]string{"volume": "kept", "reason": "no " + r.prov.name + " runner remains, but the volume was not deleted"}
	case r.deleteVolume:
		login = map[string]string{"volume": "deleted", "reason": "no " + r.prov.name + " runner remains"}
	}
	res := Result{
		"host":      r.host.Describe(),
		"provider":  r.prov.name,
		"removed":   removed,
		"remaining": remaining,
		"runners":   states,
		"login":     login,
	}
	if len(left) > 0 {
		return res, Errorf(CodeNotApplied, "runners not removed: %s", strings.Join(left, ", "))
	}
	if r.deleteVolume && has && remaining == 0 {
		return res, Errorf(CodeNotApplied, "%s still exists after runner remove", r.prov.volume())
	}
	return res, nil
}

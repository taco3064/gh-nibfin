package op

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/runners"
)

// The item actions of the host part of upgrade.
const (
	actSetEnv       = "set provider in .env"
	actUpdateUnit   = "update service"
	actSetLabels    = "set runner labels"
	upEnvPrefix     = "runner-env:"
	upUnitPrefix    = "runner-unit:"
	upLabelsPrefix  = "runner-labels:"
	waitsForJob     = "waits for its Job to end"
	noRunnerHost    = "no runner host on this computer"
	upgradeOperator = "gh nibfin upgrade"
)

// The runner-host scripts of the host part of upgrade. They are constants;
// only data travels as their arguments.
const (
	// slotSpecScript reads the slot $1 against the current spec: whether its
	// .env has the line NIBFIN_PROVIDER=$3, the unit file $2 (base64, - when
	// absent) and whether a Job runs on it.
	slotSpecScript = workersFn + `if grep -qxF "NIBFIN_PROVIDER=$3" "$1/.env" 2>/dev/null; then echo 'env yes'; else echo 'env no'; fi
if [ -f "$2" ]; then printf 'unit b64:%s\n' "$(base64 < "$2" | tr -d '\n')"; else echo 'unit -'; fi
if [ -n "$(workers)" ]; then echo 'job yes'; else echo 'job no'; fi
`
	// slotUpgradeScript brings the slot $1 (unit $3 in the unit directory
	// $2) to the current spec once no Job runs on it: it waits for each
	// Runner.Worker of the slot, stops the service right after a scan finds
	// none, sets NIBFIN_PROVIDER=$4 in its .env when $5 = 1 (envScript),
	// writes the unit from stdin when $6 = 1 (unitScript, which also
	// reloads, enables and starts it), and otherwise enables and starts it.
	// The service is stopped before anything is written, so an interrupted
	// run leaves it inactive, which the next plan shows.
	slotUpgradeScript = workersFn + `set -e
w=$(workers)
while [ -n "$w" ]; do
  for p in $w; do
    tail --pid="$p" -f /dev/null || exit 1
  done
  w=$(workers)
done
if [ -e "$2/$3" ]; then
  systemctl --user stop "$3"
fi
if [ "$5" = 1 ]; then
  (set -- "$1" "$4"
` + envScript + `)
fi
if [ "$6" = 1 ]; then
  (set -- "$2" "$3"
` + unitScript + `)
else
  systemctl --user enable --now "$3"
fi
`
)

// RunnersUpgrade is the host part of upgrade: it brings the runners on this
// host to what runner add sets up today. internal/upgrade composes it with
// the repository part.
func RunnersUpgrade() *HostUpgrade { return &HostUpgrade{} }

// HostUpgrade keeps what Plan read for Apply and ReadBack.
type HostUpgrade struct {
	host    runnerHost
	planned bool
	inv     *runners.Inventory
	repo    string
	gh      github
	user    ghUser
	admin   bool
	// slots are the configured slots in slot order, as Plan read them.
	slots []upSlot
	// steps are the plan items.
	steps []Step
}

// upSlot is one configured slot as Plan read it.
type upSlot struct {
	runners.Runner
	env  bool
	unit string // the unit file, "" when absent
	has  bool   // the unit file exists
	job  bool   // a Runner.Worker of the slot ran
	gh   *ghRunner
}

// slotSpec is slotSpecScript's output.
type slotSpec struct {
	env  bool
	unit string
	has  bool
	job  bool
}

func (h *HostUpgrade) spec(ctx context.Context, inv *runners.Inventory, r runners.Runner, code string) (slotSpec, error) {
	out, _, err := hostScript(ctx, h.host, code, "read the runner "+r.Slot, slotSpecScript, nil, "",
		inv.SlotDir(r.Slot), inv.UnitDir+"/"+runners.UnitName(r.Slot), r.Provider)
	if err != nil {
		return slotSpec{}, err
	}
	var sp slotSpec
	seen := 0
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		key, val, _ := strings.Cut(sc.Text(), " ")
		switch {
		case key == "env" && (val == "yes" || val == "no"):
			sp.env = val == "yes"
		case key == "job" && (val == "yes" || val == "no"):
			sp.job = val == "yes"
		case key == "unit" && val == "-":
		case key == "unit" && strings.HasPrefix(val, "b64:"):
			b, err := base64.StdEncoding.DecodeString(val[4:])
			if err != nil {
				return sp, Errorf(code, "read the runner %s: bad unit encoding", r.Slot)
			}
			sp.unit, sp.has = string(b), true
		default:
			return sp, Errorf(code, "read the runner %s: unexpected line %q", r.Slot, sc.Text())
		}
		seen++
	}
	if seen != 3 {
		return sp, Errorf(code, "read the runner %s: incomplete output", r.Slot)
	}
	return sp, nil
}

func upEnvStep(slot string) Step {
	return Step{ID: upEnvPrefix + slot, Action: actSetEnv, Target: slot}
}

func upUnitStep(slot string) Step {
	return Step{ID: upUnitPrefix + slot, Action: actUpdateUnit, Target: slot}
}

func upLabelsStep(slot string) Step {
	return Step{ID: upLabelsPrefix + slot, Action: actSetLabels, Target: slot}
}

// customOf returns the runner's custom and read-only labels.
func customOf(g *ghRunner) (custom, readOnly []string) {
	if g == nil {
		return nil, nil
	}
	for _, l := range g.Labels {
		if l.Type == "custom" {
			custom = append(custom, l.Name)
		} else {
			readOnly = append(readOnly, l.Name)
		}
	}
	return custom, readOnly
}

// sameSet reports whether a and b hold the same labels.
func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

// unitState names a slot's unit against the current text; "" when current.
func unitState(r runners.Runner, unit string, has bool, want string) string {
	switch {
	case !has:
		return "absent"
	case unit != want:
		return "differs"
	case !r.Enabled:
		return "not enabled"
	case !r.Active:
		return "not active"
	}
	return ""
}

// findGH returns GitHub's runner of name, which GitHub compares without case.
func findGH(list []ghRunner, name string) *ghRunner {
	if name == "" {
		return nil
	}
	for i := range list {
		if strings.EqualFold(list[i].Name, name) {
			return &list[i]
		}
	}
	return nil
}

// Plan reads the runners of this host and returns one item per difference
// against the current spec, "no change" when it matches, and notes on what
// is skipped. It only reads.
func (h *HostUpgrade) Plan(ctx context.Context) ([]Step, []string, error) {
	*h = HostUpgrade{planned: true}
	steps, notes := []Step{}, []string{}
	Checking(ctx, "host")
	hst, err := detectHost(ctx)
	switch Code(err) {
	case "":
	case CodePodmanMissing, CodeMachineMissing, CodeUnsupportedHost:
		return steps, append(notes, noRunnerHost), nil
	default:
		return steps, notes, err
	}
	h.host = hst
	where := hst.Describe()
	if hst.NeedsStart() {
		return steps, append(notes, where+" is stopped, so its Runners were not checked; start it and run "+upgradeOperator+" again"), nil
	}
	inv, err := listRunners(ctx, hst)
	if err != nil {
		return steps, notes, err
	}
	h.inv = inv
	repo, err := runners.Repo(inv.Runners)
	if err != nil {
		return steps, notes, Wrap(CodeRunnerRepoConflict, err)
	}
	h.repo = repo
	for _, r := range inv.Runners {
		if !r.Configured {
			notes = append(notes, fmt.Sprintf("%s is incomplete; run gh nibfin runner add %s", r.Slot, r.Provider))
		}
	}
	if repo == "" {
		return steps, append(notes, "no Runners on "+where), nil
	}
	if h.gh, err = newGitHub(repoHost(repo)); err != nil {
		return steps, notes, err
	}
	Checking(ctx, "GitHub login")
	if h.user, err = h.gh.User(ctx); err != nil {
		return steps, notes, err
	}
	Checking(ctx, "permissions on "+repo)
	perms, err := h.gh.RepoPermissions(ctx, repo)
	if err != nil {
		return steps, notes, err
	}
	h.admin = perms.Admin
	var list []ghRunner
	if h.admin {
		Checking(ctx, "GitHub Runners of "+repo)
		if list, err = h.gh.ListRunners(ctx, repo); err != nil {
			return steps, notes, err
		}
	} else {
		notes = append(notes, "labels left as they are: changing them needs admin of "+repo)
	}
	want := runnerLabels(true, h.user.ID)
	for _, r := range inv.Runners {
		if !r.Configured {
			continue
		}
		Checking(ctx, r.Slot+" service")
		sp, err := h.spec(ctx, inv, r, CodeRunnerInstallFailed)
		if err != nil {
			return steps, notes, err
		}
		text, err := unitText(r.Slot, inv.SlotDir(r.Slot))
		if err != nil {
			return steps, notes, err
		}
		s := upSlot{Runner: r, env: sp.env, unit: sp.unit, has: sp.has, job: sp.job}
		wait := ""
		if sp.job {
			wait = "; " + waitsForJob
		}
		env := upEnvStep(r.Slot)
		env.Detail = "NIBFIN_PROVIDER=" + r.Provider + ", other lines kept" + wait
		env.Current, env.Next, env.NoChange = "missing", "set", sp.env
		if sp.env {
			env.Current = "set"
		}
		unit := upUnitStep(r.Slot)
		unit.Detail = runners.UnitName(r.Slot) + wait
		unit.Current, unit.Next = unitState(r, sp.unit, sp.has, text), "current, enabled and active"
		if unit.Current == "" {
			unit.Current, unit.NoChange = "current", true
		}
		steps = append(steps, env, unit)
		Checked(ctx, env, unit)
		if h.admin {
			if s.gh = findGH(list, r.Name); s.gh == nil {
				notes = append(notes, fmt.Sprintf("%s (%s) is not registered on GitHub; run gh nibfin runner remove and gh nibfin runner add", r.Slot, r.Name))
			} else {
				custom, _ := customOf(s.gh)
				ls := upLabelsStep(r.Slot)
				ls.Detail = r.Name
				ls.Current, ls.Next, ls.NoChange = labelList(custom), labelList(want), sameSet(custom, want)
				steps = append(steps, ls)
				Checked(ctx, ls)
			}
		}
		h.slots = append(h.slots, s)
	}
	h.steps = steps
	return steps, notes, nil
}

func labelList(ls []string) string {
	if len(ls) == 0 {
		return "none"
	}
	return strings.Join(ls, ", ")
}

// slotOfStep returns the slot an item of the host part names, and its kind.
func slotOfStep(s Step) (slot, kind string) {
	for _, p := range []string{upEnvPrefix, upUnitPrefix, upLabelsPrefix} {
		if rest, ok := strings.CutPrefix(s.ID, p); ok {
			return rest, p
		}
	}
	return "", ""
}

// IsHostStep reports whether s is an item of the host part of upgrade.
func IsHostStep(s Step) bool {
	_, kind := slotOfStep(s)
	return kind != ""
}

// Apply applies the changing items one runner at a time: the runners where
// Plan saw no Job first, then the others, in slot order within each group.
// The .env and service items of a runner are one write that waits for its
// Job; its labels follow. The first failure stops the run.
func (h *HostUpgrade) Apply(ctx context.Context, steps []Step) error {
	if !h.planned {
		return Errorf(CodeInvalidArgument, "upgrade: Apply needs a Plan on the same operation")
	}
	type work struct{ env, unit, labels *Step }
	todo := map[string]*work{}
	for _, s := range steps {
		if s.NoChange {
			continue
		}
		slot, kind := slotOfStep(s)
		i := slices.IndexFunc(h.slots, func(x upSlot) bool { return x.Slot == slot })
		if kind == "" || i < 0 || s.Target != slot || (kind == upLabelsPrefix && h.slots[i].gh == nil) {
			return Errorf(CodeInvalidArgument, "unknown upgrade step %q %q", s.ID, s.Target)
		}
		w := todo[slot]
		if w == nil {
			w = &work{}
			todo[slot] = w
		}
		st := s
		switch kind {
		case upEnvPrefix:
			w.env = &st
		case upUnitPrefix:
			w.unit = &st
		default:
			w.labels = &st
		}
	}
	if len(todo) == 0 {
		return nil
	}
	inv, err := listRunners(ctx, h.host)
	if err != nil {
		return err
	}
	var order []upSlot
	for _, job := range []bool{false, true} {
		for _, s := range h.slots {
			if todo[s.Slot] != nil && s.job == job {
				order = append(order, s)
			}
		}
	}
	for _, s := range order {
		w := todo[s.Slot]
		r, ok := inv.Find(s.Slot)
		if !ok || !r.Configured {
			return Errorf(CodeInvalidArgument, "runner %s is no longer on %s; plan again", s.Slot, h.host.Describe())
		}
		if w.env != nil || w.unit != nil {
			var items []Step
			for _, x := range []*Step{w.env, w.unit} {
				if x != nil {
					items = append(items, *x)
				}
			}
			if err := TrackAll(ctx, items, func() error { return h.upgradeSlot(ctx, inv, s.Slot, s.Provider, w.env != nil, w.unit != nil) }); err != nil {
				return err
			}
		}
		if w.labels != nil {
			if err := Track(ctx, *w.labels, func() error {
				return h.gh.SetCustomLabels(ctx, h.repo, s.gh.ID, runnerLabels(true, h.user.ID))
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// upgradeSlot runs slotUpgradeScript on one slot.
func (h *HostUpgrade) upgradeSlot(ctx context.Context, inv *runners.Inventory, slot, provider string, env, unit bool) error {
	text := ""
	if unit {
		var err error
		if text, err = unitText(slot, inv.SlotDir(slot)); err != nil {
			return err
		}
		if !h.host.Local() {
			a := &runnerAdd{host: h.host}
			if err := a.unitDir(ctx, slot, inv.UnitDir); err != nil {
				return err
			}
		}
	}
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	_, _, err := hostScript(ctx, h.host, CodeRunnerInstallFailed, "upgrade the runner "+slot, slotUpgradeScript,
		strings.NewReader(text), "", inv.SlotDir(slot), inv.UnitDir, runners.UnitName(slot), provider, flag(env), flag(unit))
	return err
}

// RunnerUpgradeState is one runner as the host part of upgrade reads it
// back.
type RunnerUpgradeState struct {
	Slot     string `json:"slot"`
	Provider string `json:"provider"`
	Name     string `json:"name,omitempty"`
	// Env is true when the .env has the line NIBFIN_PROVIDER=<provider>.
	Env bool `json:"env"`
	// Unit is current, differs or absent; Enabled and Active are the
	// service's state.
	Unit    string `json:"unit"`
	Enabled bool   `json:"enabled"`
	Active  bool   `json:"active"`
	// The GitHub registration, read with admin only.
	Listed         bool     `json:"listed"`
	Status         string   `json:"status,omitempty"`
	Busy           bool     `json:"busy"`
	Labels         []string `json:"labels,omitempty"`
	ReadOnlyLabels []string `json:"readOnlyLabels,omitempty"`
}

// ReadBack reads every item of the plan back into checks, sets the result
// key runners, and fails with not_applied when an item is not confirmed.
func (h *HostUpgrade) ReadBack(ctx context.Context, checks *Checks, r Result) error {
	if !h.planned {
		return Errorf(CodeInvalidArgument, "upgrade: ReadBack needs a Plan on the same operation")
	}
	checks.Expect(h.steps...)
	states := []RunnerUpgradeState{}
	if h.host == nil || len(h.slots) == 0 {
		r["runners"] = map[string]any{"repo": h.repo, "slots": states}
		return nil
	}
	inv, err := listRunners(ctx, h.host)
	if err != nil {
		return err
	}
	var list []ghRunner
	if h.admin {
		if list, err = h.gh.ListRunners(ctx, h.repo); err != nil {
			return err
		}
	}
	var missing []string
	for _, s := range h.slots {
		rn, ok := inv.Find(s.Slot)
		if !ok || !rn.Configured {
			missing = append(missing, s.Slot)
			for _, st := range h.steps {
				if slot, _ := slotOfStep(st); slot == s.Slot {
					checks.Set(st, false, "absent", "not on "+h.host.Describe())
				}
			}
			continue
		}
		sp, err := h.spec(ctx, inv, rn, CodeRunnerInstallFailed)
		if err != nil {
			return err
		}
		text, err := unitText(rn.Slot, inv.SlotDir(rn.Slot))
		if err != nil {
			return err
		}
		st := RunnerUpgradeState{Slot: rn.Slot, Provider: rn.Provider, Name: rn.Name, Env: sp.env, Unit: "current", Enabled: rn.Enabled, Active: rn.Active}
		switch {
		case !sp.has:
			st.Unit = "absent"
		case sp.unit != text:
			st.Unit = "differs"
		}
		bad := false
		envState := "missing"
		if sp.env {
			envState = "set"
		}
		checks.Set(upEnvStep(rn.Slot), sp.env, envState, ".env lacks NIBFIN_PROVIDER="+rn.Provider)
		bad = bad || !sp.env
		us := unitState(rn, sp.unit, sp.has, text)
		checks.Set(upUnitStep(rn.Slot), us == "", orCurrent(us), "service "+us)
		bad = bad || us != ""
		if h.admin && s.gh != nil {
			g := findGH(list, rn.Name)
			custom, readOnly := customOf(g)
			if g != nil {
				st.Listed, st.Status, st.Busy = true, g.Status, g.Busy
			}
			st.Labels, st.ReadOnlyLabels = custom, readOnly
			ok := g != nil && sameSet(custom, runnerLabels(true, h.user.ID))
			state := labelList(custom)
			if g != nil && len(readOnly) > 0 {
				state += " (read-only: " + strings.Join(readOnly, ", ") + ")"
			}
			if g != nil && g.Status != "" {
				// Offline right after the restart is reported, not failed.
				state += "; " + g.Status
			}
			why := "custom labels are " + labelList(custom)
			if g == nil {
				state, why = "not listed", "not registered on GitHub"
			}
			checks.Set(upLabelsStep(rn.Slot), ok, state, why)
			bad = bad || !ok
		}
		if bad {
			missing = append(missing, rn.Slot)
		}
		states = append(states, st)
	}
	r["runners"] = map[string]any{"repo": h.repo, "slots": states}
	if len(missing) > 0 {
		return Errorf(CodeNotApplied, "runners not at the current spec after upgrade: %s", strings.Join(missing, ", "))
	}
	return nil
}

func orCurrent(s string) string {
	if s == "" {
		return "current"
	}
	return s
}

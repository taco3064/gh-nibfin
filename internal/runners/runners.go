// Package runners discovers the gh nibfin Runners on the host where they
// live: which exist, their provider and the repository they serve.
//
// Each Runner is the official GitHub Actions runner in its own slot
// directory, ${XDG_DATA_HOME:-$HOME/.local/share}/nibfin/runners/<provider>-<n>,
// run by the systemd user unit
// ${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/nibfin-runner-<provider>-<n>.service.
// Discovery reads only the runner's own settings file (.runner), never its
// credentials.
package runners

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Shell runs a fixed script where the Runners live; host.Host satisfies it.
type Shell interface {
	Shell(ctx context.Context, script string, args ...string) *exec.Cmd
}

// ListScript prints the layout's directories and one line per slot. Paths
// and .runner contents are base64-encoded so every line stays one line.
const ListScript = `d="${XDG_DATA_HOME:-$HOME/.local/share}/nibfin/runners"
u="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
b() { printf '%s' "$1" | base64 | tr -d '\n'; }
printf 'dirs %s %s\n' "$(b "$d")" "$(b "$u")"
[ -d "$d" ] || exit 0
for s in "$d"/*; do
  [ -d "$s" ] || continue
  n=${s##*/}
  case $n in ''|*[!a-z0-9-]*) continue ;; esac
  r=-
  if [ -f "$s/.runner" ]; then r=b64:$(base64 < "$s/.runner" | tr -d '\n') || exit 1; fi
  f=0
  [ -f "$u/nibfin-runner-$n.service" ] && f=1
  e=$(systemctl --user is-enabled "nibfin-runner-$n.service" 2>/dev/null | head -n 1 | tr -cd 'a-z-')
  a=$(systemctl --user is-active "nibfin-runner-$n.service" 2>/dev/null | head -n 1 | tr -cd 'a-z-')
  printf 'slot %s %s %s %s %s\n' "$n" "$r" "$f" "${e:-unknown}" "${a:-unknown}"
done
`

// Runner is one slot on the host.
type Runner struct {
	Slot     string `json:"slot"`
	Provider string `json:"provider"`
	// Number is the slot's number within its provider.
	Number int `json:"-"`
	// Name is the runner's name on GitHub, from .runner.
	Name string `json:"name,omitempty"`
	// Repo is the repository it serves as HOST/OWNER/REPO, from .runner.
	Repo string `json:"repo,omitempty"`
	// Configured is true when .runner exists.
	Configured bool `json:"configured"`
	// Unit is true when the unit file exists.
	Unit bool `json:"unit"`
	// Enabled is true when the unit is enabled.
	Enabled bool `json:"enabled"`
	// Active is true when the unit is running.
	Active bool `json:"active"`
	// Dir is the slot directory on the host.
	Dir string `json:"-"`
	// invalid explains a .runner that could not be used.
	invalid string
}

// Complete reports whether the slot is configured, has its unit and the unit
// is enabled. An incomplete slot is what an interrupted add left.
func (r Runner) Complete() bool { return r.Configured && r.Unit && r.Enabled }

// Inventory is the result of a discovery.
type Inventory struct {
	// Dir holds the slot directories; UnitDir the user units.
	Dir, UnitDir string
	Runners      []Runner
}

// UnitName is the systemd user unit of a slot.
func UnitName(slot string) string { return "nibfin-runner-" + slot + ".service" }

// SlotDir is the directory of a slot.
func (inv *Inventory) SlotDir(slot string) string { return path.Join(inv.Dir, slot) }

// Find returns the slot's runner, if it exists.
func (inv *Inventory) Find(slot string) (Runner, bool) {
	for _, r := range inv.Runners {
		if r.Slot == slot {
			return r, true
		}
	}
	return Runner{}, false
}

// Of returns the provider's runners in slot order.
func (inv *Inventory) Of(provider string) []Runner {
	var out []Runner
	for _, r := range inv.Runners {
		if r.Provider == provider {
			out = append(out, r)
		}
	}
	return out
}

// Providers reports which providers have at least one slot.
func (inv *Inventory) Providers() map[string]bool {
	m := map[string]bool{}
	for _, r := range inv.Runners {
		m[r.Provider] = true
	}
	return m
}

// List discovers the Runners. Only slots named after one of providers count.
func List(ctx context.Context, sh Shell, providers []string) (*Inventory, error) {
	var stdout, stderr bytes.Buffer
	c := sh.Shell(ctx, ListScript)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return nil, &Error{Err: err, Stderr: strings.TrimSpace(stderr.String())}
	}
	inv, err := Parse(stdout.String(), providers)
	if err != nil {
		return nil, &Error{Err: err}
	}
	return inv, nil
}

// Error is a failed discovery; Stderr is the script's error output.
type Error struct {
	Err    error
	Stderr string
}

func (e *Error) Error() string {
	if e.Stderr != "" {
		return "cannot list the runners: " + e.Stderr
	}
	return "cannot list the runners: " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// ExitCode is the script's exit status, or -1 when it did not exit.
func (e *Error) ExitCode() int {
	var x *exec.ExitError
	if errors.As(e.Err, &x) {
		return x.ExitCode()
	}
	return -1
}

// Parse reads ListScript's output strictly.
func Parse(out string, providers []string) (*Inventory, error) {
	slotRE, err := regexp.Compile(`^(` + strings.Join(quoteAll(providers), "|") + `)-([1-9][0-9]{0,5})$`)
	if err != nil {
		return nil, err
	}
	inv := &Inventory{}
	sawDirs := false
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), " ")
		switch {
		case f[0] == "dirs" && len(f) == 3 && !sawDirs:
			if inv.Dir, err = dirField(f[1]); err != nil {
				return nil, err
			}
			if inv.UnitDir, err = dirField(f[2]); err != nil {
				return nil, err
			}
			sawDirs = true
		case f[0] == "slot" && len(f) == 6 && sawDirs:
			m := slotRE.FindStringSubmatch(f[1])
			if m == nil {
				continue
			}
			n, _ := strconv.Atoi(m[2])
			r := Runner{Slot: f[1], Provider: m[1], Number: n, Dir: inv.SlotDir(f[1])}
			switch {
			case f[2] == "-":
			case strings.HasPrefix(f[2], "b64:"):
				raw, err := base64.StdEncoding.DecodeString(f[2][4:])
				if err != nil {
					return nil, fmt.Errorf("slot %s: bad .runner encoding", r.Slot)
				}
				r.Configured = true
				r.Name, r.Repo, err = settings(raw)
				if err != nil {
					r.invalid = err.Error()
				}
			default:
				return nil, fmt.Errorf("slot %s: bad .runner field", r.Slot)
			}
			switch f[3] {
			case "1":
				r.Unit = true
			case "0":
			default:
				return nil, fmt.Errorf("slot %s: bad unit field", r.Slot)
			}
			r.Enabled = f[4] == "enabled"
			r.Active = f[5] == "active" || f[5] == "activating" || f[5] == "reloading"
			inv.Runners = append(inv.Runners, r)
		default:
			return nil, fmt.Errorf("unexpected line %q", sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawDirs {
		return nil, errors.New("no layout line")
	}
	sort.Slice(inv.Runners, func(i, j int) bool {
		a, b := inv.Runners[i], inv.Runners[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Number < b.Number
	})
	return inv, nil
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = regexp.QuoteMeta(s)
	}
	return out
}

func dirField(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	d := string(b)
	if err != nil || !path.IsAbs(d) || strings.ContainsAny(d, "\x00\n\r") {
		return "", fmt.Errorf("bad layout directory %q", d)
	}
	return path.Clean(d), nil
}

// settings reads the runner's name and repository from .runner. The runner
// writes it as UTF-8 JSON, possibly with a BOM; Go matches the field names
// case-insensitively, so PascalCase and camelCase both work.
func settings(raw []byte) (name, repo string, err error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	var s struct {
		AgentName string
		GitHubURL string `json:"gitHubUrl"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", "", fmt.Errorf("unreadable .runner: %v", err)
	}
	repo, err = RepoOfURL(s.GitHubURL)
	return s.AgentName, repo, err
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// RepoOfURL turns a runner's GitHub URL into HOST/OWNER/REPO. Anything but a
// repository URL is rejected.
func RepoOfURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("runner URL %q is not a repository URL", s)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !nameRE.MatchString(parts[0]) || !nameRE.MatchString(parts[1]) || !nameRE.MatchString(strings.ReplaceAll(u.Host, ":", "-")) {
		return "", fmt.Errorf("runner URL %q is not a repository URL", s)
	}
	return strings.ToLower(u.Host) + "/" + parts[0] + "/" + parts[1], nil
}

// Repo returns the one repository the configured runners serve, "" when
// none is configured. Runners that disagree, or a .runner without a usable
// repository, are an error naming them.
func Repo(rs []Runner) (string, error) {
	repo := ""
	seen := map[string][]string{}
	var order []string
	for _, r := range rs {
		if !r.Configured {
			continue
		}
		if r.invalid != "" {
			return "", fmt.Errorf("runner %s: %s", r.Slot, r.invalid)
		}
		key := strings.ToLower(r.Repo)
		if _, ok := seen[key]; !ok {
			order = append(order, key)
			if repo == "" {
				repo = r.Repo
			}
		}
		seen[key] = append(seen[key], r.Slot)
	}
	if len(order) > 1 {
		var parts []string
		for _, k := range order {
			parts = append(parts, fmt.Sprintf("%s (%s)", k, strings.Join(seen[k], ", ")))
		}
		return "", fmt.Errorf("the runners on this host serve different repositories: %s", strings.Join(parts, "; "))
	}
	return repo, nil
}

// DeployKeyPath is the private key file of the read-only deploy key that a
// collaborator's host uses for the private execution repository repo
// (<host>/<owner>/<name>); its public key is the same path with ".pub". It
// lies beside the slot root, outside every slot, one per repository since a
// deploy key serves one repository only.
func DeployKeyPath(inv *Inventory, repo string) string {
	return path.Join(path.Dir(inv.Dir), "deploy-keys", strings.ToLower(repo), "id_ed25519")
}

// HookDir is the directory of the job-started hook of a collaborator's
// Runners, ${XDG_DATA_HOME:-$HOME/.local/share}/nibfin/hook: beside the slot
// root and outside every slot, as the runner asks of hook scripts. There is
// one per host, since a host's Runners serve one repository (Repo).
func HookDir(inv *Inventory) string {
	return path.Join(path.Dir(inv.Dir), "hook")
}

// HookScript is the hook's script, which a slot's .env names in
// ACTIONS_RUNNER_HOOK_JOB_STARTED.
func HookScript(inv *Inventory) string {
	return path.Join(HookDir(inv), "job-started.sh")
}

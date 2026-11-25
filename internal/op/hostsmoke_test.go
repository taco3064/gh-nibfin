//go:build hostsmoke

package op

// The host smoke test drives the host side of runner add and runner remove
// against a real Windows podman machine: the real host layer (podman machine
// ssh), slot layout, scripts and systemd units. Only what needs GitHub is
// replaced: the registration token, the runner download (a stand-in archive
// from testdata/hostsmoke) and the GitHub runner list. Every check reads the
// VM itself. See "Host smoke test" in the README for how to run it.

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/host"
)

const (
	// smokeEnv names the podman machine the test may use.
	smokeEnv      = "NIBFIN_HOST_SMOKE"
	smokeProvider = "claude"
	smokeRepo     = "github.com/nibfin-smoke/placeholder"
	smokeUser     = 4242
	// smokeToken is a fixed, non-secret registration token.
	smokeToken = "NIBFINSMOKETOKEN"
	// smokeBindings is podman machine's own user unit, which runner add
	// must leave as it is.
	smokeBindings = "podman-mnt-bindings.service"
)

// smokeReadScript prints what the test checks, read from the VM itself, as
// "<subject> <key> <value>" lines: the user, the owners of the unit
// directory and its default.target.wants, podman machine's own bindings
// unit, and for each slot given as an argument its directory, files and
// service state.
const smokeReadScript = `d="${XDG_DATA_HOME:-$HOME/.local/share}/nibfin/runners"
u="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
has() { if [ -e "$1" ]; then echo 1; else echo 0; fi; }
printf 'vm uid %s\n' "$(id -u)"
printf 'vm units-owner %s\n' "$(stat -c %u "$u" 2>/dev/null || echo -)"
printf 'vm wants-owner %s\n' "$(stat -c %u "$u/default.target.wants" 2>/dev/null || echo -)"
b=podman-mnt-bindings.service
if [ -f "$u/$b" ]; then h=$(sha256sum < "$u/$b" | cut -c1-64); else h=-; fi
printf 'vm bindings-hash %s\n' "$h"
printf 'vm bindings-link %s\n' "$(readlink "$u/default.target.wants/$b" 2>/dev/null || echo -)"
for n do
  s="$d/$n"
  printf '%s dir %s\n' "$n" "$(if [ -d "$s" ]; then echo 1; else echo 0; fi)"
  printf '%s runner %s\n' "$n" "$(has "$s/.runner")"
  printf '%s env %s\n' "$n" "$(has "$s/.env")"
  printf '%s unitfile %s\n' "$n" "$(has "$u/nibfin-runner-$n.service")"
  c=0
  [ -f "$s/.configured" ] && c=$(wc -l < "$s/.configured" | tr -cd '0-9')
  printf '%s configured %s\n' "$n" "$c"
  e=$(systemctl --user is-enabled "nibfin-runner-$n.service" 2>/dev/null | head -n 1 | tr -cd 'a-z-')
  a=$(systemctl --user is-active "nibfin-runner-$n.service" 2>/dev/null | head -n 1 | tr -cd 'a-z-')
  printf '%s enabled %s\n' "$n" "${e:-not-found}"
  printf '%s active %s\n' "$n" "${a:-unknown}"
done
`

// smokeInterruptScript puts a slot into the state an interrupted add leaves
// (configured, no unit): it disables and stops the unit $2 in the unit
// directory $1 and deletes its file.
const smokeInterruptScript = `set -e
systemctl --user disable --now "$2"
rm -f -- "$1/$2"
systemctl --user daemon-reload
`

// smokeGitHub fakes the GitHub side of the runner commands. Its runner list
// is one online, idle runner per configured slot that discovery on the real
// host reports, minus those DeleteRunner removed.
type smokeGitHub struct {
	h         runnerHost
	downloads []runnerDownload
	// downloadCalls and tokenCalls count Downloads and RegistrationToken.
	downloadCalls, tokenCalls int
	ids                       map[string]int64
	deleted                   map[int64]bool
	// unlabeled and deletes record RemoveCustomLabels and DeleteRunner.
	unlabeled, deletes []int64
}

func (g *smokeGitHub) User(context.Context) (int64, error) { return smokeUser, nil }

func (g *smokeGitHub) Downloads(context.Context, string) ([]runnerDownload, error) {
	g.downloadCalls++
	return g.downloads, nil
}

func (g *smokeGitHub) RegistrationToken(context.Context, string) (string, error) {
	g.tokenCalls++
	return smokeToken, nil
}

func (g *smokeGitHub) ListRunners(ctx context.Context, repo string) ([]ghRunner, error) {
	inv, err := listRunners(ctx, g.h)
	if err != nil {
		return nil, err
	}
	out := []ghRunner{}
	for _, r := range inv.Runners {
		if !r.Configured || r.Name == "" || !strings.EqualFold(r.Repo, repo) {
			continue
		}
		key := strings.ToLower(r.Name)
		id, ok := g.ids[key]
		if !ok {
			id = int64(len(g.ids) + 1)
			g.ids[key] = id
		}
		if g.deleted[id] {
			continue
		}
		out = append(out, ghRunner{ID: id, Name: r.Name, Status: "online", Labels: []ghLabel{
			{Name: "self-hosted", Type: "read-only"},
			{Name: nibfinLabel, Type: "custom"},
			{Name: "nibfin-4242-" + smokeProvider, Type: "custom"},
		}})
	}
	return out, nil
}

func (g *smokeGitHub) RemoveCustomLabels(_ context.Context, _ string, id int64) error {
	g.unlabeled = append(g.unlabeled, id)
	return nil
}

func (g *smokeGitHub) DeleteRunner(_ context.Context, _ string, id int64) error {
	g.deletes = append(g.deletes, id)
	g.deleted[id] = true
	return nil
}

// smokeArchive builds the stand-in runner archive from testdata/hostsmoke.
// Carriage returns are dropped, so a CRLF checkout still runs in the VM.
func smokeArchive(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"config.sh", "bin/runsvc.sh"} {
		b, err := os.ReadFile(filepath.Join("testdata", "hostsmoke", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = strings.ReplaceAll(string(b), "\r", "")
	}
	return tarGz(t, files)
}

// useSmokeGitHub installs the GitHub fakes and a TLS server serving the
// stand-in archive; the cleanup restores them.
func useSmokeGitHub(t *testing.T, h runnerHost) *smokeGitHub {
	t.Helper()
	body := smokeArchive(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	sum := shaHex(string(body))
	gh := &smokeGitHub{h: h, ids: map[string]int64{}, deleted: map[int64]bool{}}
	for _, arch := range []string{"x64", "arm64"} {
		gh.downloads = append(gh.downloads, runnerDownload{OS: "linux", Architecture: arch,
			DownloadURL: srv.URL + "/runner-" + arch + ".tar.gz", Filename: "runner-" + arch + ".tar.gz", SHA256: sum})
	}
	oldGH, oldHTTP, oldRepo := newGitHub, httpClient, currentRepo
	t.Cleanup(func() { newGitHub, httpClient, currentRepo = oldGH, oldHTTP, oldRepo })
	newGitHub = func(string) (github, error) { return gh, nil }
	httpClient = srv.Client()
	currentRepo = func() (string, error) { return smokeRepo, nil }
	return gh
}

// smokeVM is one read of the VM: values by subject and key.
type smokeVM map[string]map[string]string

func (v smokeVM) get(subject, key string) string { return v[subject][key] }

func readVM(t *testing.T, ctx context.Context, h runnerHost, slots ...string) smokeVM {
	t.Helper()
	var stdout, stderr bytes.Buffer
	c := h.Shell(ctx, smokeReadScript, slots...)
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		t.Fatalf("read the VM: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	v := smokeVM{}
	sc := bufio.NewScanner(&stdout)
	for sc.Scan() {
		f := strings.SplitN(strings.TrimRight(sc.Text(), "\r"), " ", 3)
		if len(f) != 3 {
			continue
		}
		if v[f[0]] == nil {
			v[f[0]] = map[string]string{}
		}
		v[f[0]][f[1]] = f[2]
	}
	return v
}

// expectVM fails for every key of want whose value the VM read differs in,
// naming the subject (a slot or "vm") and the item.
func expectVM(t *testing.T, step string, v smokeVM, want map[string]map[string]string) {
	t.Helper()
	for subject, keys := range want {
		for key, w := range keys {
			if got := v.get(subject, key); got != w {
				t.Errorf("%s: %s %s = %q, want %q", step, subject, key, got, w)
			}
		}
	}
}

// readySlot is the VM read of a slot runner add completed: its directory,
// .runner, .env and unit file exist, config.sh ran once, and the unit is
// enabled and active.
func readySlot() map[string]string {
	return map[string]string{"dir": "1", "runner": "1", "env": "1", "unitfile": "1", "configured": "1", "enabled": "enabled", "active": "active"}
}

func autostartPresent(t *testing.T, ctx context.Context, h runnerHost) host.Autostart {
	t.Helper()
	as, err := h.Autostart(ctx)
	if err != nil {
		t.Fatalf("read the autostart entry: %v", err)
	}
	return as
}

// withoutLogin drops the provider login items from a runner add plan: the
// login needs the provider's account, which the smoke test does not have.
func withoutLogin(p Plan) Plan {
	steps := []Step{}
	for _, s := range p.Steps {
		if (s.Action == actCreateVolume && s.Target == "nibfin-home-"+smokeProvider) || s.Action == actLogIn {
			continue
		}
		steps = append(steps, s)
	}
	p.Steps = steps
	return p
}

func addTargets(p Plan, action string) []string {
	var out []string
	for _, s := range p.Changes() {
		if s.Action == action {
			out = append(out, s.Target)
		}
	}
	return out
}

// smokeAdd runs runner add without its login items and returns its plan and
// read-back. The read-back may fail only because the provider is not logged
// in; the checks of the slots must pass.
func smokeAdd(t *testing.T, ctx context.Context, count int) Plan {
	t.Helper()
	o := RunnerAdd(RunnerAddParams{Provider: smokeProvider, Count: count})
	p, err := o.Plan(ctx)
	if err != nil {
		t.Fatalf("runner add --count %d: plan: %v", count, err)
	}
	p = withoutLogin(p)
	if err := o.Apply(ctx, p); err != nil {
		t.Fatalf("runner add --count %d: apply: %v [%s]", count, err, Code(err))
	}
	r, err := o.ReadBack(ctx, p)
	if err != nil && Code(err) != CodeNotLoggedIn {
		t.Fatalf("runner add --count %d: read back: %v [%s]", count, err, Code(err))
	}
	items, _ := r[ResultItems].([]Check)
	for _, slot := range addTargets(p, actAddRunner) {
		found := false
		for _, c := range items {
			if c.ID == "runner:"+slot {
				found = true
				if !c.Confirmed {
					t.Errorf("runner add --count %d: read back runner:%s = %q: %s", count, slot, c.State, c.Reason)
				}
			}
		}
		if !found {
			t.Errorf("runner add --count %d: read back has no runner:%s check", count, slot)
		}
	}
	return p
}

func smokeRemove(ctx context.Context) (Plan, error) {
	o := RunnerRemove(RunnerRemoveParams{Provider: smokeProvider})
	p, err := o.Plan(ctx)
	if err != nil {
		return p, err
	}
	if err := o.Apply(ctx, p); err != nil {
		return p, err
	}
	_, err = o.ReadBack(ctx, p)
	return p, err
}

func TestHostSmoke(t *testing.T) {
	machine := os.Getenv(smokeEnv)
	if runtime.GOOS != "windows" || machine == "" {
		t.Skipf("the host smoke test runs on Windows with %s set to the podman machine name", smokeEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// The guard: nothing is written unless the named machine is the
	// running default WSL machine, without Runners and without the
	// autostart entry.
	h, err := detectHost(ctx)
	if err != nil {
		t.Fatalf("guard: detect the host: %v", err)
	}
	hh, ok := h.(*host.Host)
	if !ok || hh.Machine != machine || !hh.WSL() || hh.NeedsStart() {
		t.Fatalf("guard: %s=%q does not name the running default WSL podman machine (found %s)", smokeEnv, machine, h.Describe())
	}
	inv, err := listRunners(ctx, h)
	if err != nil {
		t.Fatalf("guard: list the Runners: %v", err)
	}
	if len(inv.Runners) > 0 {
		var slots []string
		for _, r := range inv.Runners {
			slots = append(slots, r.Slot)
		}
		t.Fatalf("guard: %s already has Runner slots: %s", machine, strings.Join(slots, ", "))
	}
	if as := autostartPresent(t, ctx, h); as.Kind == "" || as.Present {
		t.Fatalf("guard: the autostart entry %q already exists (or none applies here: kind %q)", as.Name, as.Kind)
	}

	gh := useSmokeGitHub(t, h)
	// A failed run removes what it added, so a person's host keeps no
	// slots. Registered after the fakes, so it runs before they are
	// restored.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer ccancel()
		if _, err := smokeRemove(cctx); err != nil && Code(err) != CodeNoRunners {
			t.Logf("cleanup: runner remove %s: %v", smokeProvider, err)
		}
	})

	slots := []string{"claude-1", "claude-2", "claude-3"}
	before := readVM(t, ctx, h, slots...)
	uid := before.get("vm", "uid")
	t.Logf("before: unit directory owner %s, default.target.wants owner %s, VM user %s",
		before.get("vm", "units-owner"), before.get("vm", "wants-owner"), uid)

	t.Run("add", func(t *testing.T) {
		p := smokeAdd(t, ctx, 2)
		if got := addTargets(p, actAddRunner); strings.Join(got, ",") != "claude-1,claude-2" {
			t.Fatalf("add: planned slots %v, want claude-1, claude-2", got)
		}
		v := readVM(t, ctx, h, slots...)
		expectVM(t, "add", v, map[string]map[string]string{
			"vm": {
				"units-owner":   uid,
				"wants-owner":   uid,
				"bindings-hash": before.get("vm", "bindings-hash"),
				"bindings-link": before.get("vm", "bindings-link"),
			},
			"claude-1": readySlot(),
			"claude-2": readySlot(),
			"claude-3": {"dir": "0", "unitfile": "0"},
		})
		if as := autostartPresent(t, ctx, h); !as.Present || !as.Current {
			t.Errorf("add: autostart %s present %v, current %v (%s)", as.Name, as.Present, as.Current, as.Outdated)
		}
		if gh.downloadCalls != 1 || gh.tokenCalls != 1 {
			t.Errorf("add: %d download lists and %d registration tokens fetched, want 1 each", gh.downloadCalls, gh.tokenCalls)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("rerun after an interrupted add", func(t *testing.T) {
		inv, err := listRunners(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		c := h.Shell(ctx, smokeInterruptScript, inv.UnitDir, "nibfin-runner-claude-2.service")
		c.Stdout, c.Stderr = io.Discard, &stderr
		if err := c.Run(); err != nil {
			t.Fatalf("interrupt claude-2: %v: %s", err, strings.TrimSpace(stderr.String()))
		}
		expectVM(t, "interrupt", readVM(t, ctx, h, "claude-2"), map[string]map[string]string{
			"claude-2": {"runner": "1", "unitfile": "0"},
		})
		downloads, tokens := gh.downloadCalls, gh.tokenCalls
		p := smokeAdd(t, ctx, 1)
		if got := addTargets(p, actAddRunner); strings.Join(got, ",") != "claude-2" {
			t.Fatalf("rerun: planned slots %v, want claude-2", got)
		}
		if gh.downloadCalls != downloads || gh.tokenCalls != tokens {
			t.Errorf("rerun: fetched %d download lists and %d registration tokens, want none",
				gh.downloadCalls-downloads, gh.tokenCalls-tokens)
		}
		expectVM(t, "rerun", readVM(t, ctx, h, slots...), map[string]map[string]string{
			"claude-1": readySlot(),
			"claude-2": readySlot(),
			"claude-3": {"dir": "0", "unitfile": "0"},
		})
	})
	if t.Failed() {
		return
	}

	t.Run("remove", func(t *testing.T) {
		p, err := smokeRemove(ctx)
		if err != nil {
			t.Fatalf("runner remove %s: %v [%s]", smokeProvider, err, Code(err))
		}
		if got := addTargets(p, actRemoveRunner); strings.Join(got, ",") != "claude-1,claude-2" {
			t.Errorf("remove: planned slots %v, want claude-1, claude-2", got)
		}
		if len(addTargets(p, actRemoveAutostart)) != 1 {
			t.Errorf("remove: the plan does not remove the autostart entry: %+v", p.Steps)
		}
		v := readVM(t, ctx, h, slots...)
		for _, s := range slots[:2] {
			if v.get(s, "dir") != "0" || v.get(s, "unitfile") != "0" {
				t.Errorf("remove: %s dir %s, unitfile %s, want both 0", s, v.get(s, "dir"), v.get(s, "unitfile"))
			}
			if a := v.get(s, "active"); a == "active" || a == "activating" || a == "deactivating" {
				t.Errorf("remove: %s active = %q, want inactive or not found", s, a)
			}
		}
		if as := autostartPresent(t, ctx, h); as.Present {
			t.Errorf("remove: autostart %s is still present", as.Name)
		}
		if len(gh.unlabeled) != 2 || len(gh.deletes) != 2 {
			t.Errorf("remove: labels removed from %v, registrations deleted %v, want two runners each", gh.unlabeled, gh.deletes)
		}
	})
}

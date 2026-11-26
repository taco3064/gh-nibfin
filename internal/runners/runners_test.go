package runners

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

var providers = []string{"claude", "codex"}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func listing(slots ...string) string {
	return "dirs " + b64("/home/u/.local/share/nibfin/runners") + " " + b64("/home/u/.config/systemd/user") + "\n" + strings.Join(slots, "")
}

func slot(name, runner string, unit bool, enabled, active string) string {
	r := "-"
	if runner != "" {
		r = "b64:" + b64(runner)
	}
	u := "0"
	if unit {
		u = "1"
	}
	return "slot " + name + " " + r + " " + u + " " + enabled + " " + active + "\n"
}

const pascal = "\xef\xbb\xbf{\n  \"AgentId\": 7,\n  \"AgentName\": \"h-1-claude-1\",\n  \"PoolId\": 1,\n  \"GitHubUrl\": \"https://github.com/Owner/Repo\",\n  \"WorkFolder\": \"_work\"\n}"
const camel = `{"agentName":"h-1-codex-2","gitHubUrl":"https://github.com/owner/repo"}`

func TestParse(t *testing.T) {
	out := listing(
		slot("codex-2", camel, true, "enabled", "active"),
		slot("claude-1", pascal, true, "enabled", "inactive"),
		slot("claude-10", "", false, "unknown", "unknown"),
		slot("claude-3", camel, false, "unknown", "inactive"),
		slot("gemini-1", camel, true, "enabled", "active"),
		slot("claude-0", camel, true, "enabled", "active"),
	)
	inv, err := Parse(out, providers)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Dir != "/home/u/.local/share/nibfin/runners" || inv.UnitDir != "/home/u/.config/systemd/user" {
		t.Errorf("dirs = %q %q", inv.Dir, inv.UnitDir)
	}
	var got []string
	for _, r := range inv.Runners {
		got = append(got, r.Slot)
	}
	if !reflect.DeepEqual(got, []string{"claude-1", "claude-3", "claude-10", "codex-2"}) {
		t.Fatalf("slots = %v", got)
	}
	c1 := inv.Runners[0]
	want := Runner{Slot: "claude-1", Provider: "claude", Number: 1, Name: "h-1-claude-1", Repo: "github.com/Owner/Repo",
		Configured: true, Unit: true, Enabled: true, Dir: "/home/u/.local/share/nibfin/runners/claude-1"}
	if !reflect.DeepEqual(c1, want) || !c1.Complete() {
		t.Errorf("claude-1 = %+v", c1)
	}
	if r := inv.Runners[1]; !r.Configured || r.Unit || r.Complete() || r.Repo != "github.com/owner/repo" {
		t.Errorf("claude-3 = %+v", r)
	}
	if r := inv.Runners[2]; r.Configured || r.Complete() || r.Number != 10 {
		t.Errorf("claude-10 = %+v", r)
	}
	if r := inv.Runners[3]; r.Provider != "codex" || !r.Active || r.Name != "h-1-codex-2" {
		t.Errorf("codex-2 = %+v", r)
	}
	if repo, err := Repo(inv.Runners); err != nil || repo != "github.com/Owner/Repo" {
		t.Errorf("Repo = %q, %v", repo, err)
	}
	if p := inv.Providers(); !p["claude"] || !p["codex"] || len(p) != 2 {
		t.Errorf("providers = %v", p)
	}
}

func TestParseRejects(t *testing.T) {
	for _, out := range []string{
		"",
		"slot claude-1 - 0 x y\n",
		listing("bogus line\n"),
		listing(slot("claude-1", "", false, "a", "b") + "slot claude-2 xyz 0 a b\n"),
		listing("slot claude-1 - 2 a b\n"),
		"dirs " + b64("relative") + " " + b64("/u") + "\n",
	} {
		if _, err := Parse(out, providers); err == nil {
			t.Errorf("accepted %q", out)
		}
	}
}

func TestRepoConflictAndInvalid(t *testing.T) {
	inv, err := Parse(listing(
		slot("claude-1", camel, true, "enabled", "active"),
		slot("codex-1", `{"AgentName":"x","GitHubUrl":"https://ghe.example/o/other"}`, true, "enabled", "active"),
	), providers)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Repo(inv.Runners)
	if err == nil || !strings.Contains(err.Error(), "github.com/owner/repo (claude-1)") || !strings.Contains(err.Error(), "ghe.example/o/other (codex-1)") {
		t.Errorf("conflict = %v", err)
	}
	for _, bad := range []string{
		`{"GitHubUrl":"https://github.com/org-only"}`,
		`{"GitHubUrl":"http://github.com/o/r"}`,
		`{"GitHubUrl":"https://x@github.com/o/r"}`,
		`{"GitHubUrl":"https://github.com/o/r/extra"}`,
		`not json`,
	} {
		inv, err := Parse(listing(slot("claude-1", bad, true, "enabled", "active")), providers)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Repo(inv.Runners); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if repo, err := Repo(nil); repo != "" || err != nil {
		t.Errorf("empty = %q, %v", repo, err)
	}
}

type localShell struct{ env []string }

func (l localShell) Shell(ctx context.Context, script string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, "sh", append([]string{"-c", script, "nibfin"}, args...)...)
	c.Env = append(os.Environ(), l.env...)
	return c
}

// TestListScript runs the real script against a slot tree.
func TestListScript(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the script runs on the Linux runner host")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("no base64")
	}
	root := t.TempDir()
	data, conf := filepath.Join(root, "data"), filepath.Join(root, "conf")
	sh := localShell{[]string{"XDG_DATA_HOME=" + data, "XDG_CONFIG_HOME=" + conf}}
	inv, err := List(context.Background(), sh, providers)
	if err != nil || len(inv.Runners) != 0 || inv.Dir != filepath.ToSlash(filepath.Join(data, "nibfin/runners")) {
		t.Fatalf("empty: %+v, %v", inv, err)
	}
	dir := filepath.Join(data, "nibfin", "runners")
	for _, s := range []string{"claude-1", "codex-1", "notes", "Bad Name"} {
		if err := os.MkdirAll(filepath.Join(dir, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "claude-1", ".runner"), []byte(pascal+strings.Repeat(" ", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	units := filepath.Join(conf, "systemd", "user")
	if err := os.MkdirAll(units, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(units, "nibfin-runner-claude-1.service"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	inv, err = List(context.Background(), sh, providers)
	if err != nil || len(inv.Runners) != 2 {
		t.Fatalf("list: %+v, %v", inv, err)
	}
	c, x := inv.Runners[0], inv.Runners[1]
	if c.Slot != "claude-1" || !c.Configured || !c.Unit || c.Name != "h-1-claude-1" || c.Repo != "github.com/Owner/Repo" {
		t.Errorf("claude-1 = %+v", c)
	}
	if x.Slot != "codex-1" || x.Configured || x.Unit {
		t.Errorf("codex-1 = %+v", x)
	}
}

func TestListError(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	_, err := List(context.Background(), failShell{}, providers)
	var e *Error
	if err == nil || !strings.Contains(err.Error(), "boom") || !errorsAs(err, &e) || e.ExitCode() != 4 {
		t.Errorf("err = %v", err)
	}
}

type failShell struct{}

func (failShell) Shell(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", "echo boom >&2; exit 4")
}

func errorsAs(err error, e **Error) bool {
	x, ok := err.(*Error)
	*e = x
	return ok
}

func TestDeployKeyPath(t *testing.T) {
	inv := &Inventory{Dir: "/home/u/.local/share/nibfin/runners"}
	if p := DeployKeyPath(inv, "github.com/Owner/Repo"); p != "/home/u/.local/share/nibfin/deploy-keys/github.com/owner/repo/id_ed25519" {
		t.Errorf("path = %q", p)
	}
}

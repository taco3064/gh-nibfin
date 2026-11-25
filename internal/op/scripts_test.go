package op

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// localHost runs the scripts with the local sh, as host.Host does on Linux.
type localHost struct {
	fakeHost
	env []string
}

func (l *localHost) Shell(ctx context.Context, script string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, "sh", append([]string{"-c", script, "nibfin"}, args...)...)
	c.Env = append(os.Environ(), l.env...)
	return c
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// TestScriptsUnderSh runs the real runner-host scripts against a directory
// tree, with stand-ins for config.sh and systemctl.
func TestScriptsUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	for _, tool := range []string{"sh", "tar", "gzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho \"$*\" >> \"$SYSTEMCTL_LOG\"\n"), 0o755)
	h := &localHost{env: []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "SYSTEMCTL_LOG=" + filepath.Join(root, "systemctl.log")}}
	a := &runnerAdd{host: h}
	ctx := context.Background()

	if _, err := a.probe(ctx); err != nil {
		t.Fatalf("probe: %v", err)
	}

	slot := filepath.Join(root, "data", "nibfin", "runners", "claude-1")
	arc := filepath.Join(root, "runner.tar.gz")
	os.WriteFile(arc, tarGz(t, map[string]string{
		"bin/runsvc.sh": "#!/bin/sh\n",
		"config.sh":     "#!/bin/sh\nprintf '%s' \"$ACTIONS_RUNNER_INPUT_TOKEN\" > token.out\nprintf '%s\\n' \"$@\" > args.out\nread x && echo stdin-leaked > leak.out\nexit 0\n",
	}), 0o644)
	extract := func(mode string) error {
		f, _ := os.Open(arc)
		defer f.Close()
		_, err := a.script(ctx, "extract", extractScript, f, "", slot, mode)
		return err
	}
	if err := extract("new"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(slot, "runsvc.sh")); err != nil {
		t.Errorf("runsvc.sh not copied: %v", err)
	}
	if err := extract("new"); Code(err) != CodeRunnerInstallFailed {
		t.Errorf("a new slot that exists: %v", err)
	}
	if err := extract("existing"); err != nil {
		t.Errorf("re-extract: %v", err)
	}

	_, err := a.script(ctx, "configure", configureScript, strings.NewReader("tok-123\nmore\n"), "tok-123", slot, "--name", "it's me", "--replace")
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(slot, "token.out")); string(b) != "tok-123" {
		t.Errorf("token = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(slot, "args.out")); string(b) != "--name\nit's me\n--replace\n" {
		t.Errorf("args = %q", b)
	}
	if _, err := os.Stat(filepath.Join(slot, "leak.out")); err == nil {
		t.Error("config.sh could read the rest of stdin")
	}

	units := filepath.Join(root, "units")
	if _, err := a.script(ctx, "unit", unitScript, strings.NewReader("[Unit]\n"), "", units, "nibfin-runner-claude-1.service"); err != nil {
		t.Fatalf("unit: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(units, "nibfin-runner-claude-1.service")); string(b) != "[Unit]\n" {
		t.Errorf("unit = %q", b)
	}
	if _, err := os.Stat(filepath.Join(units, "nibfin-runner-claude-1.service.tmp")); err == nil {
		t.Error("tmp unit left")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "systemctl.log")); string(b) != "--user daemon-reload\n--user enable --now nibfin-runner-claude-1.service\n" {
		t.Errorf("systemctl = %q", b)
	}
}

// TestUnitDirScriptUnderSh runs unitDirScript with a stand-in sudo: it
// asks sudo for a non-recursive chown of exactly the directories the user
// cannot write, and of nothing else.
func TestUnitDirScriptUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write every directory")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	log := filepath.Join(root, "sudo.log")
	os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\necho \"$*\" >> \"$SUDO_LOG\"\nexit ${SUDO_EXIT:-0}\n"), 0o755)
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "SUDO_LOG=" + log}
	a := &runnerAdd{host: &localHost{env: env}}
	ctx := context.Background()
	idOut, err := exec.Command("sh", "-c", `echo "$(id -u):$(id -g)"`).Output()
	if err != nil {
		t.Fatal(err)
	}
	owner := strings.TrimSpace(string(idOut))
	run := func(dir string) error {
		_, err := a.script(ctx, "unitdir", unitDirScript, nil, "", dir)
		return err
	}
	sudo := func() string {
		b, _ := os.ReadFile(log)
		os.Remove(log)
		return string(b)
	}

	// Missing: nothing to do.
	units := filepath.Join(root, "systemd", "user")
	if err := run(units); err != nil || sudo() != "" {
		t.Errorf("missing: %v", err)
	}
	// Writable: no sudo.
	wants := filepath.Join(units, "default.target.wants")
	os.MkdirAll(wants, 0o755)
	if err := run(units); err != nil || sudo() != "" {
		t.Errorf("writable: %v", err)
	}
	// Only the read-only one is changed, never recursively.
	os.Chmod(wants, 0o555)
	t.Cleanup(func() { os.Chmod(wants, 0o755); os.Chmod(units, 0o755) })
	if err := run(units); err != nil {
		t.Fatalf("wants read-only: %v", err)
	}
	if got := sudo(); got != "-n chown "+owner+" "+wants+"\n" {
		t.Errorf("sudo = %q", got)
	}
	os.Chmod(units, 0o555)
	if err := run(units); err != nil {
		t.Fatalf("both read-only: %v", err)
	}
	if got := sudo(); got != "-n chown "+owner+" "+units+"\n-n chown "+owner+" "+wants+"\n" {
		t.Errorf("sudo = %q", got)
	}
	// A symlinked directory is not followed.
	os.Chmod(units, 0o755)
	os.Chmod(wants, 0o755)
	target := filepath.Join(root, "elsewhere")
	os.Mkdir(target, 0o555)
	t.Cleanup(func() { os.Chmod(target, 0o755) })
	link := filepath.Join(root, "linked")
	os.Symlink(target, link)
	if err := run(link); err != nil || sudo() != "" {
		t.Errorf("symlink: %v", err)
	}
	// A refused sudo fails the script.
	os.Chmod(units, 0o555)
	a.host = &localHost{env: append(env, "SUDO_EXIT=1")}
	if err := run(units); Code(err) != CodeRunnerInstallFailed {
		t.Errorf("refused: %v", err)
	}
}

// TestEnvScriptsUnderSh runs envScript and envCheckScript with the local sh.
func TestEnvScriptsUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	a := &runnerAdd{host: &localHost{}, prov: provider{name: "claude"}}
	ctx := context.Background()
	slot := t.TempDir()
	env := filepath.Join(slot, ".env")
	check := func() bool {
		ok, err := a.envHasProvider(ctx, slot)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		return ok
	}
	set := func(p string) {
		if _, err := a.script(ctx, "env", envScript, nil, "", slot, p); err != nil {
			t.Fatalf("env %s: %v", p, err)
		}
	}
	// No .env yet (config.sh not run): created with the one line.
	if check() {
		t.Error("check passed without .env")
	}
	set("claude")
	if b, _ := os.ReadFile(env); string(b) != "NIBFIN_PROVIDER=claude\n" {
		t.Errorf(".env = %q", b)
	}
	// env.sh's lines are kept, an old provider line is replaced, and a
	// missing final newline is not lost.
	os.WriteFile(env, []byte("LANG=C.UTF-8\nNIBFIN_PROVIDER=codex\nJAVA_HOME=/j"), 0o644)
	if check() {
		t.Error("check passed for codex")
	}
	set("claude")
	set("claude")
	if b, _ := os.ReadFile(env); string(b) != "LANG=C.UTF-8\nJAVA_HOME=/j\nNIBFIN_PROVIDER=claude\n" {
		t.Errorf(".env = %q", b)
	}
	if !check() {
		t.Error("check failed")
	}
	if _, err := os.Stat(env + ".tmp"); err == nil {
		t.Error("tmp .env left")
	}
	// Only the provider line: grep finds nothing else, which is no error.
	os.WriteFile(env, []byte("NIBFIN_PROVIDER=codex\n"), 0o644)
	set("claude")
	if b, _ := os.ReadFile(env); string(b) != "NIBFIN_PROVIDER=claude\n" {
		t.Errorf(".env = %q", b)
	}
}

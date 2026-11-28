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

// TestDeployKeyScriptsUnderSh runs the real deploy key scripts: the pair is
// written with the private key from stdin, mode 0600 in a 0700 directory,
// read back, and a lone private key is not a pair.
func TestDeployKeyScriptsUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	for _, tool := range []string{"sh", "stat"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	root := t.TempDir()
	a := &runnerAdd{host: &localHost{}}
	ctx := context.Background()
	key := filepath.Join(root, "nibfin", "deploy-keys", "github.com", "o", "r", "id_ed25519")
	read := func() string {
		out, err := a.script(ctx, "read", deployKeyReadScript, nil, "", key)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return out
	}
	if out := read(); out != "" {
		t.Errorf("absent: %q", out)
	}
	const priv = "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n"
	const pub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGtrampolinekeykeykeykeykeykeykeykeyk nibfin-deploy-key"
	// A stale public key from before is replaced.
	os.MkdirAll(filepath.Dir(key), 0o755)
	os.WriteFile(key+".pub", []byte("old\n"), 0o644)
	if _, err := a.script(ctx, "write", deployKeyWriteScript, strings.NewReader(priv), priv, key, pub); err != nil {
		t.Fatalf("write: %v", err)
	}
	if b, _ := os.ReadFile(key); string(b) != priv {
		t.Errorf("private key %q", b)
	}
	for p, want := range map[string]os.FileMode{key: 0o600, filepath.Dir(key): 0o700 | os.ModeDir} {
		if fi, err := os.Stat(p); err != nil || fi.Mode() != want {
			t.Errorf("%s: mode %v, want %v (%v)", p, fi.Mode(), want, err)
		}
	}
	if out := read(); out != pub+"\n" {
		t.Errorf("read %q", out)
	}
	out, err := a.script(ctx, "check", deployKeyCheckScript, nil, "", key)
	if err != nil || out != "mode 600\n"+pub+"\n" {
		t.Errorf("check %q, %v", out, err)
	}
	if m, _ := filepath.Glob(key + "*.tmp"); len(m) != 0 {
		t.Errorf("temporary files left: %v", m)
	}
	// A private key without its public key (an interrupted write) is no pair.
	os.Remove(key + ".pub")
	if out := read(); out != "" {
		t.Errorf("lone private key: %q", out)
	}
	if out, err := a.script(ctx, "check", deployKeyCheckScript, nil, "", key); err != nil || out != "" {
		t.Errorf("check lone: %q, %v", out, err)
	}
}

// TestHookScriptsUnderSh runs the hook's host scripts: the write replaces
// the directory with 0700/0600 modes, the read returns the files, the .env
// lines are set and checked, and the removal refuses other paths.
func TestHookScriptsUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	for _, tool := range []string{"sh", "tar", "base64", "find", "stat"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	root := t.TempDir()
	h := &localHost{}
	a := &runnerAdd{host: h}
	ctx := context.Background()
	dir := filepath.Join(root, "data", "nibfin", "hook")

	write := func(files map[string][]byte) {
		t.Helper()
		b, err := hookTar(files)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.script(ctx, "write", hookWriteScript, bytes.NewReader(b), "", dir); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	files := wantHook(true)
	write(files)
	st, err := readHook(ctx, h, CodeRunnerInstallFailed, dir)
	if err != nil {
		t.Fatal(err)
	}
	if why := st.diff(files); why != "" {
		t.Errorf("read back: %s", why)
	}
	for _, p := range []string{"config", "official/nibfin-job.yml"} {
		if fi, _ := os.Stat(filepath.Join(dir, p)); fi == nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v", p, fi)
		}
	}
	// A second write replaces the directory whole: a stray file goes, and
	// nothing is left beside it.
	os.WriteFile(filepath.Join(dir, "stray"), []byte("x"), 0o600)
	os.MkdirAll(dir+".new", 0o700)
	files["check.mjs"] = []byte("new\n")
	write(files)
	if st, _ = readHook(ctx, h, CodeRunnerInstallFailed, dir); st.diff(files) != "" {
		t.Errorf("rewrite: %s", st.diff(files))
	}
	if left, _ := filepath.Glob(dir + ".*"); len(left) != 0 {
		t.Errorf("left beside the hook: %v", left)
	}
	if st, _ := readHook(ctx, h, CodeRunnerInstallFailed, filepath.Join(root, "none")); st.present {
		t.Error("an absent directory reads as present")
	}

	// The .env lines.
	slot := filepath.Join(root, "data", "nibfin", "runners", "claude-1")
	os.MkdirAll(slot, 0o700)
	os.WriteFile(filepath.Join(slot, ".env"), []byte("LANG=C\nNIBFIN_PROVIDER=claude\nNIBFIN_RUNNER_DIR=/old\n"), 0o600)
	script := filepath.Join(dir, "job-started.sh")
	check := func() string {
		out, err := a.script(ctx, "check", hookEnvCheckScript, nil, "", slot, script)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	if got := check(); got != "no" {
		t.Errorf("check before = %s", got)
	}
	for i := 0; i < 2; i++ {
		if _, err := a.script(ctx, "env", hookEnvScript, nil, "", slot, script); err != nil {
			t.Fatal(err)
		}
	}
	env, _ := os.ReadFile(filepath.Join(slot, ".env"))
	if want := "LANG=C\nNIBFIN_PROVIDER=claude\nACTIONS_RUNNER_HOOK_JOB_STARTED=" + script + "\nNIBFIN_RUNNER_DIR=" + slot + "\n"; string(env) != want {
		t.Errorf(".env = %q, want %q", env, want)
	}
	if got := check(); got != "yes" {
		t.Errorf("check after = %s", got)
	}

	// Presence and removal; other paths are refused.
	present := func() bool {
		ok, err := hookPresent(ctx, h, CodeRunnerRemoveFailed, dir)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !present() {
		t.Error("the hook reads as absent")
	}
	for _, bad := range []string{filepath.Join(root, "data"), root + "/data/nibfin/../nibfin/hook", "relative/nibfin/hook"} {
		if _, err := a.script(ctx, "remove", hookRemoveScript, nil, "", bad); err == nil {
			t.Errorf("removed %s", bad)
		}
		if _, err := a.script(ctx, "write", hookWriteScript, strings.NewReader(""), "", bad); err == nil {
			t.Errorf("wrote %s", bad)
		}
	}
	if _, err := a.script(ctx, "remove", hookRemoveScript, nil, "", dir); err != nil {
		t.Fatal(err)
	}
	if present() {
		t.Error("the hook is still present")
	}
}

// TestLibicuScriptsUnderSh runs the probe's libicu line and libicuScript
// with stand-ins for ldconfig, sudo and dnf: ldconfig is asked with -NXv
// over the LD_LIBRARY_PATH entries, as config.sh does; libicu present
// changes nothing; a missing one is installed with sudo -n dnf and checked
// again; a missing dnf, a refused sudo or a library still missing fail.
func TestLibicuScriptsUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	icu := filepath.Join(root, "icu")
	ldLog, sudoLog := filepath.Join(root, "ldconfig.log"), filepath.Join(root, "sudo.log")
	// ldconfig lists libicu once the marker file exists.
	os.WriteFile(filepath.Join(bin, "ldconfig"), []byte("#!/bin/sh\necho \"$*\" >> \"$LD_LOG\"\necho '/usr/lib64:'\n"+
		"if [ -e \"$ICU\" ]; then echo '\tlibicuuc.so.78 -> libicuuc.so.78.3'; fi\n"), 0o755)
	// sudo logs its arguments and, unless refused, runs the command.
	os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\necho \"$*\" >> \"$SUDO_LOG\"\n[ \"${SUDO_EXIT:-0}\" = 0 ] || { echo 'sudo: a password is required' >&2; exit 1; }\nshift\nexec \"$@\"\n"), 0o755)
	// dnf installs libicu unless DNF_NOOP is set.
	os.WriteFile(filepath.Join(bin, "dnf"), []byte("#!/bin/sh\necho 'Installed: libicu'\n[ -n \"$DNF_NOOP\" ] || : > \"$ICU\"\n"), 0o755)
	// nodnf has every tool the script needs but dnf.
	nodnf := filepath.Join(root, "nodnf")
	os.MkdirAll(nodnf, 0o755)
	os.Symlink(filepath.Join(bin, "ldconfig"), filepath.Join(nodnf, "ldconfig"))
	for _, tool := range []string{"tr", "grep"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skip("no " + tool)
		}
		os.Symlink(p, filepath.Join(nodnf, tool))
	}
	base := []string{"ICU=" + icu, "LD_LOG=" + ldLog, "SUDO_LOG=" + sudoLog, "LD_LIBRARY_PATH=/opt/a:/opt/b"}
	host := func(path string, env ...string) *runnerAdd {
		return &runnerAdd{host: &localHost{env: append(append([]string{"PATH=" + path}, base...), env...)}}
	}
	ctx := context.Background()
	read := func(p string) string {
		b, _ := os.ReadFile(p)
		os.Remove(p)
		return string(b)
	}
	sysPath := bin + ":" + os.Getenv("PATH")

	// The probe reports libicu missing, then present.
	pr, err := host(sysPath).probe(ctx)
	if err != nil || pr.icu {
		t.Fatalf("probe without libicu: %v %v", pr.icu, err)
	}
	// The icu and the ssl check each run ldconfig.
	if got := read(ldLog); got != "-NXv /opt/a /opt/b\n-NXv /opt/a /opt/b\n" {
		t.Errorf("ldconfig args = %q", got)
	}
	os.WriteFile(icu, nil, 0o644)
	if pr, err = host(sysPath).probe(ctx); err != nil || !pr.icu {
		t.Fatalf("probe with libicu: %v %v", pr.icu, err)
	}

	run := func(a *runnerAdd) error {
		_, err := a.script(ctx, "install libicu", libicuScript, nil, "")
		return err
	}
	// Present: no sudo, no dnf.
	if err := run(host(sysPath)); err != nil || read(sudoLog) != "" {
		t.Errorf("present: %v", err)
	}
	// Missing: installed with sudo -n dnf, then found.
	os.Remove(icu)
	if err := run(host(sysPath)); err != nil {
		t.Fatalf("missing: %v", err)
	}
	if got := read(sudoLog); got != "-n dnf install -y libicu\n" {
		t.Errorf("sudo = %q", got)
	}
	if _, err := os.Stat(icu); err != nil {
		t.Errorf("libicu not installed: %v", err)
	}
	// A refused sudo keeps its output.
	os.Remove(icu)
	if err := run(host(sysPath, "SUDO_EXIT=1")); Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "a password is required") {
		t.Errorf("refused: %v", err)
	}
	// Still missing after dnf.
	read(sudoLog)
	if err := run(host(sysPath, "DNF_NOOP=1")); Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "still not found") {
		t.Errorf("still missing: %v", err)
	}
	// No dnf: nothing is asked of sudo.
	read(sudoLog)
	if err := run(host(nodnf)); Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "dnf is not available") || read(sudoLog) != "" {
		t.Errorf("no dnf: %v", err)
	}
}

// TestLibsslScriptsUnderSh runs the probe's libssl line and libsslScript
// under sh with a fake ldconfig, dnf and sudo. ldconfig always lists
// libssl.so.4 (OpenSSL 4) and libssl.so.30, which .NET 8 does not load, and
// lists a loadable libssl once the marker file exists. libssl present
// changes nothing; missing installs openssl3-libs, or openssl-libs where the
// distribution has no openssl3-libs, with sudo -n dnf; a refused sudo, a
// library still missing and a VM without dnf fail with their cause.
func TestLibsslScriptsUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	ssl := filepath.Join(root, "ssl")
	sudoLog := filepath.Join(root, "sudo.log")
	os.WriteFile(filepath.Join(bin, "ldconfig"), []byte("#!/bin/sh\necho '/usr/lib64:'\n"+
		"echo '\tlibssl.so.4 -> libssl.so.4.0.3'\necho '\tlibssl.so.30 -> libssl.so.30.0.0'\n"+
		"if [ -e \"$SSL\" ]; then printf '\\t%s -> %s.9\\n' \"${SSL_NAME:-libssl.so.3}\" \"${SSL_NAME:-libssl.so.3}\"; fi\n"), 0o755)
	// sudo logs its arguments and, unless refused, runs the command.
	os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\necho \"$*\" >> \"$SUDO_LOG\"\n[ \"${SUDO_EXIT:-0}\" = 0 ] || { echo 'sudo: a password is required' >&2; exit 1; }\nshift\nexec \"$@\"\n"), 0o755)
	// dnf knows openssl3-libs unless NO_OPENSSL3 is set, and installs the
	// library unless DNF_NOOP is set.
	os.WriteFile(filepath.Join(bin, "dnf"), []byte(`#!/bin/sh
case "$*" in
"-q repoquery openssl3-libs") [ -n "$NO_OPENSSL3" ] || echo 'openssl3-libs-1:3.5.8-1.fc45.x86_64'; exit 0 ;;
"install -y "*) echo "Installed: $3"; [ -n "$DNF_NOOP" ] || : > "$SSL" ;;
*) echo "unexpected dnf $*" >&2; exit 2 ;;
esac
`), 0o755)
	// nodnf has every tool the script needs but dnf.
	nodnf := filepath.Join(root, "nodnf")
	os.MkdirAll(nodnf, 0o755)
	os.Symlink(filepath.Join(bin, "ldconfig"), filepath.Join(nodnf, "ldconfig"))
	for _, tool := range []string{"tr", "grep"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skip("no " + tool)
		}
		os.Symlink(p, filepath.Join(nodnf, tool))
	}
	base := []string{"SSL=" + ssl, "SUDO_LOG=" + sudoLog}
	host := func(path string, env ...string) *runnerAdd {
		return &runnerAdd{host: &localHost{env: append(append([]string{"PATH=" + path}, base...), env...)}}
	}
	ctx := context.Background()
	read := func(p string) string {
		b, _ := os.ReadFile(p)
		os.Remove(p)
		return string(b)
	}
	sysPath := bin + ":" + os.Getenv("PATH")

	// The probe reports libssl missing with only libssl.so.4 and .so.30,
	// then present with each soname .NET 8 loads.
	pr, err := host(sysPath).probe(ctx)
	if err != nil || pr.ssl {
		t.Fatalf("probe with libssl.so.4 only: %v %v", pr.ssl, err)
	}
	os.WriteFile(ssl, nil, 0o644)
	for _, name := range []string{"libssl.so.3", "libssl.so.1.1", "libssl.so.1.0.2", "libssl.so.1.0.0", "libssl.so.10", "libssl.so.11", "libssl.so.111"} {
		if pr, err = host(sysPath, "SSL_NAME="+name).probe(ctx); err != nil || !pr.ssl {
			t.Errorf("probe with %s: %v %v", name, pr.ssl, err)
		}
	}
	for _, name := range []string{"libssl.so.4", "libssl.so.3x", "xlibssl.so.3", "libssl.so.12"} {
		if pr, err = host(sysPath, "SSL_NAME="+name).probe(ctx); err != nil || pr.ssl {
			t.Errorf("probe with %s: %v %v", name, pr.ssl, err)
		}
	}

	run := func(a *runnerAdd) (string, error) {
		return a.script(ctx, "install libssl", libsslScript, nil, "")
	}
	// Present: no sudo, no dnf.
	if _, err := run(host(sysPath)); err != nil || read(sudoLog) != "" {
		t.Errorf("present: %v", err)
	}
	// Missing: openssl3-libs is installed with sudo -n dnf, then found;
	// dnf's output is kept.
	os.Remove(ssl)
	out, err := run(host(sysPath))
	if err != nil || !strings.Contains(out, "Installed: openssl3-libs") {
		t.Fatalf("missing: %q %v", out, err)
	}
	if got := read(sudoLog); got != "-n dnf install -y openssl3-libs\n" {
		t.Errorf("sudo = %q", got)
	}
	if _, err := os.Stat(ssl); err != nil {
		t.Errorf("libssl not installed: %v", err)
	}
	// Missing where the distribution has no openssl3-libs: openssl-libs.
	os.Remove(ssl)
	if _, err := run(host(sysPath, "NO_OPENSSL3=1")); err != nil {
		t.Fatalf("missing, no openssl3-libs: %v", err)
	}
	if got := read(sudoLog); got != "-n dnf install -y openssl-libs\n" {
		t.Errorf("sudo = %q", got)
	}
	// A refused sudo keeps its output.
	os.Remove(ssl)
	if _, err := run(host(sysPath, "SUDO_EXIT=1")); Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "a password is required") {
		t.Errorf("refused: %v", err)
	}
	// Still missing after dnf: the error names the package and keeps dnf's
	// output.
	read(sudoLog)
	if _, err := run(host(sysPath, "DNF_NOOP=1")); Code(err) != CodeRunnerInstallFailed ||
		!strings.Contains(err.Error(), "libssl.so.3 is still not found after dnf install openssl3-libs") ||
		!strings.Contains(err.Error(), "Installed: openssl3-libs") {
		t.Errorf("still missing: %v", err)
	}
	// No dnf: nothing is asked of sudo.
	read(sudoLog)
	if _, err := run(host(nodnf)); Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "libssl (libssl.so.3) is not found and dnf is not available") || read(sudoLog) != "" {
		t.Errorf("no dnf: %v", err)
	}
}

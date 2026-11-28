package op

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	return buildTarGz(files)
}

// buildTarGz is tarGz without a test, for package-level fixtures.
func buildTarGz(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			panic(err)
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

// TestIcuCheckUnderSh runs the probe's libicu line with a stand-in for
// ldconfig: ldconfig is asked with -NXv over the LD_LIBRARY_PATH entries, as
// config.sh does, and libicu is reported missing, then present.
func TestIcuCheckUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	icu := filepath.Join(root, "icu")
	ldLog := filepath.Join(root, "ldconfig.log")
	// ldconfig lists libicu once the marker file exists.
	os.WriteFile(filepath.Join(bin, "ldconfig"), []byte("#!/bin/sh\necho \"$*\" >> \"$LD_LOG\"\necho '/usr/lib64:'\n"+
		"if [ -e \"$ICU\" ]; then echo '\tlibicuuc.so.78 -> libicuuc.so.78.3'; fi\n"), 0o755)
	a := &runnerAdd{host: &localHost{env: []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "ICU=" + icu, "LD_LOG=" + ldLog, "LD_LIBRARY_PATH=/opt/a:/opt/b"}}}
	ctx := context.Background()
	pr, err := a.probe(ctx)
	if err != nil || pr.icu {
		t.Fatalf("probe without libicu: %v %v", pr.icu, err)
	}
	// The icu and the ssl check each run ldconfig.
	if got, _ := os.ReadFile(ldLog); string(got) != "-NXv /opt/a /opt/b\n-NXv /opt/a /opt/b\n" {
		t.Errorf("ldconfig args = %q", got)
	}
	os.WriteFile(icu, nil, 0o644)
	if pr, err = a.probe(ctx); err != nil || !pr.icu {
		t.Fatalf("probe with libicu: %v %v", pr.icu, err)
	}
}

// TestSslCheckUnderSh runs the probe's libssl line with a stand-in for
// ldconfig that always lists libssl.so.4 (OpenSSL 4) and libssl.so.30, which
// .NET 8 does not load, and lists the soname SSL_NAME once the marker file
// exists: only the sonames .NET 8 loads count, each as a whole name.
func TestSslCheckUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	ssl := filepath.Join(root, "ssl")
	os.WriteFile(filepath.Join(bin, "ldconfig"), []byte("#!/bin/sh\necho '/usr/lib64:'\n"+
		"echo '\tlibssl.so.4 -> libssl.so.4.0.3'\necho '\tlibssl.so.30 -> libssl.so.30.0.0'\n"+
		"if [ -e \"$SSL\" ]; then printf '\\t%s -> %s.9\\n' \"${SSL_NAME:-libssl.so.3}\" \"${SSL_NAME:-libssl.so.3}\"; fi\n"), 0o755)
	host := func(env ...string) *runnerAdd {
		return &runnerAdd{host: &localHost{env: append([]string{"PATH=" + bin + ":" + os.Getenv("PATH"), "SSL=" + ssl}, env...)}}
	}
	ctx := context.Background()
	pr, err := host().probe(ctx)
	if err != nil || pr.ssl {
		t.Fatalf("probe with libssl.so.4 only: %v %v", pr.ssl, err)
	}
	os.WriteFile(ssl, nil, 0o644)
	for _, name := range []string{"libssl.so.3", "libssl.so.1.1", "libssl.so.1.0.2", "libssl.so.1.0.0", "libssl.so.10", "libssl.so.11", "libssl.so.111"} {
		if pr, err = host("SSL_NAME=" + name).probe(ctx); err != nil || !pr.ssl {
			t.Errorf("probe with %s: %v %v", name, pr.ssl, err)
		}
	}
	for _, name := range []string{"libssl.so.4", "libssl.so.3x", "xlibssl.so.3", "libssl.so.12"} {
		if pr, err = host("SSL_NAME=" + name).probe(ctx); err != nil || pr.ssl {
			t.Errorf("probe with %s: %v %v", name, pr.ssl, err)
		}
	}
}

// TestDepsScriptUnderSh runs depsScript and depsCheckScript under sh with
// stand-ins for ldconfig, sudo and dnf, and a stand-in installer on stdin
// in place of the runner's bin/installdependencies.sh. ldconfig lists libicu
// and libssl.so.3 once their marker files exist and always lists
// libssl.so.4. The installer installs what INST_ICU and INST_SSL name and
// exits with INST_EXIT. Both present changes nothing; missing runs the
// installer with sudo -n bash; libssl.so.3 still missing after it installs
// openssl3-libs (or openssl-libs) with sudo -n dnf; a failed installer, a
// refused sudo, libicu still missing, a failed fallback and a VM without
// dnf fail with their cause; the temporary file never stays.
func TestDepsScriptUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	tmp := filepath.Join(root, "tmp")
	os.MkdirAll(bin, 0o755)
	os.MkdirAll(tmp, 0o755)
	icu, ssl := filepath.Join(root, "icu"), filepath.Join(root, "ssl")
	sudoLog := filepath.Join(root, "sudo.log")
	os.WriteFile(filepath.Join(bin, "ldconfig"), []byte("#!/bin/sh\necho '/usr/lib64:'\necho '\tlibssl.so.4 -> libssl.so.4.0.3'\n"+
		"if [ -e \"$ICU\" ]; then echo '\tlibicuuc.so.78 -> libicuuc.so.78.3'; fi\n"+
		"if [ -e \"$SSL\" ]; then echo '\tlibssl.so.3 -> libssl.so.3.5.8'; fi\n"), 0o755)
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
	installer := `echo "installer as $0"
[ -z "$INST_ICU" ] || : > "$ICU"
[ -z "$INST_SSL" ] || : > "$SSL"
exit "${INST_EXIT:-0}"
`
	// nodnf has every tool the script needs but dnf.
	nodnf := filepath.Join(root, "nodnf")
	os.MkdirAll(nodnf, 0o755)
	for _, tool := range []string{"ldconfig", "sudo"} {
		os.Symlink(filepath.Join(bin, tool), filepath.Join(nodnf, tool))
	}
	for _, tool := range []string{"tr", "grep", "bash", "mktemp", "cat", "rm"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skip("no " + tool)
		}
		os.Symlink(p, filepath.Join(nodnf, tool))
	}
	base := []string{"ICU=" + icu, "SSL=" + ssl, "SUDO_LOG=" + sudoLog, "TMPDIR=" + tmp}
	sysPath := bin + ":" + os.Getenv("PATH")
	ctx := context.Background()
	read := func(p string) string {
		b, _ := os.ReadFile(p)
		os.Remove(p)
		return string(b)
	}
	// set leaves exactly the named libraries present.
	set := func(hasICU, hasSSL bool) {
		os.Remove(icu)
		os.Remove(ssl)
		if hasICU {
			os.WriteFile(icu, nil, 0o644)
		}
		if hasSSL {
			os.WriteFile(ssl, nil, 0o644)
		}
		read(sudoLog)
	}
	run := func(path string, env ...string) (string, error) {
		t.Helper()
		a := &runnerAdd{host: &localHost{env: append(append([]string{"PATH=" + path}, base...), env...)}}
		out, err := a.script(ctx, "install runner dependencies", depsScript, strings.NewReader(installer), "")
		if left, _ := os.ReadDir(tmp); len(left) != 0 {
			t.Errorf("temporary file left behind: %v", left)
		}
		return out, err
	}
	sudoed := func() []string {
		var out []string
		for _, l := range strings.Split(strings.TrimSpace(read(sudoLog)), "\n") {
			if strings.HasPrefix(l, "-n bash "+tmp+"/") {
				l = "-n bash TMP"
			}
			if l != "" {
				out = append(out, l)
			}
		}
		return out
	}

	// Present: no sudo, no installer.
	set(true, true)
	if out, err := run(sysPath); err != nil || out != "" || len(sudoed()) != 0 {
		t.Errorf("present: %q %v", out, err)
	}
	// Missing: the installer runs with sudo -n bash and its output is kept;
	// no fallback is needed.
	set(false, true)
	out, err := run(sysPath, "INST_ICU=1")
	if err != nil || !strings.Contains(out, "installer as "+tmp+"/") {
		t.Fatalf("installer: %q %v", out, err)
	}
	if got := sudoed(); !reflect.DeepEqual(got, []string{"-n bash TMP"}) {
		t.Errorf("sudo = %q", got)
	}
	// The installer leaves libssl.so.3 missing (OpenSSL 4): openssl3-libs is
	// installed with sudo -n dnf, then found.
	set(false, false)
	out, err = run(sysPath, "INST_ICU=1")
	if err != nil || !strings.Contains(out, "Installed: openssl3-libs") {
		t.Fatalf("fallback: %q %v", out, err)
	}
	if got := sudoed(); !reflect.DeepEqual(got, []string{"-n bash TMP", "-n dnf install -y openssl3-libs"}) {
		t.Errorf("sudo = %q", got)
	}
	if _, err := os.Stat(ssl); err != nil {
		t.Errorf("libssl not installed: %v", err)
	}
	// Where the distribution has no openssl3-libs: openssl-libs.
	set(true, false)
	if _, err := run(sysPath, "NO_OPENSSL3=1"); err != nil {
		t.Fatalf("fallback, no openssl3-libs: %v", err)
	}
	if got := sudoed(); !reflect.DeepEqual(got, []string{"-n bash TMP", "-n dnf install -y openssl-libs"}) {
		t.Errorf("sudo = %q", got)
	}
	// The installer fails: its status and output are kept, nothing else runs.
	set(false, true)
	if _, err := run(sysPath, "INST_EXIT=3"); Code(err) != CodeRunnerInstallFailed ||
		!strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "bin/installdependencies.sh failed inside the VM") ||
		!strings.Contains(err.Error(), "installer as") {
		t.Errorf("installer failed: %v", err)
	}
	if got := sudoed(); !reflect.DeepEqual(got, []string{"-n bash TMP"}) {
		t.Errorf("sudo = %q", got)
	}
	// A refused sudo keeps its output.
	set(false, true)
	if _, err := run(sysPath, "SUDO_EXIT=1"); Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "a password is required") {
		t.Errorf("refused: %v", err)
	}
	// libicu still missing after the installer.
	set(false, true)
	if _, err := run(sysPath); Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "libicu is still not found after bin/installdependencies.sh") {
		t.Errorf("libicu still missing: %v", err)
	}
	// The fallback does not help: the error names the package and keeps
	// dnf's output.
	set(true, false)
	if _, err := run(sysPath, "DNF_NOOP=1"); Code(err) != CodeRunnerInstallFailed ||
		!strings.Contains(err.Error(), "libssl.so.3 is still not found after dnf install openssl3-libs") ||
		!strings.Contains(err.Error(), "Installed: openssl3-libs") {
		t.Errorf("fallback failed: %v", err)
	}
	// No dnf for the fallback: nothing more is asked of sudo.
	set(true, false)
	if _, err := run(nodnf); Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "dnf is not available") {
		t.Errorf("no dnf: %v", err)
	}
	if got := sudoed(); !reflect.DeepEqual(got, []string{"-n bash TMP"}) {
		t.Errorf("sudo = %q", got)
	}

	// The Linux host's check names each missing library and installs
	// nothing.
	check := func() error {
		a := &runnerAdd{host: &localHost{env: append([]string{"PATH=" + sysPath}, base...)}}
		_, err := a.script(ctx, "check", depsCheckScript, nil, "")
		return err
	}
	for _, c := range []struct {
		icu, ssl bool
		missing  string
	}{{false, true, "missing: libicu"}, {true, false, "missing: libssl.so.3"}, {false, false, "missing: libicu, libssl.so.3"}} {
		set(c.icu, c.ssl)
		if err := check(); Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), c.missing) {
			t.Errorf("check %+v: %v", c, err)
		}
	}
	set(true, true)
	if err := check(); err != nil || len(sudoed()) != 0 {
		t.Errorf("check present: %v", err)
	}
}

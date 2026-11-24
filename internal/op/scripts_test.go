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

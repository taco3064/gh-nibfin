package host

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestSetIdleTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		changed        bool
	}{
		{"empty", "", "[wsl2]\r\nvmIdleTimeout=-1\r\n", true},
		{"no section, LF", "[general]\ninstanceIdleTimeout=5\n", "[general]\ninstanceIdleTimeout=5\n[wsl2]\nvmIdleTimeout=-1\n", true},
		{"no section, no final newline", "[general]\r\nx=1", "[general]\r\nx=1\r\n[wsl2]\r\nvmIdleTimeout=-1\r\n", true},
		{"section without key", "# mine\r\n[WSL2]\r\nmemory=8GB\r\n\r\n[experimental]\r\nsparseVhd=true\r\n",
			"# mine\r\n[WSL2]\r\nvmIdleTimeout=-1\r\nmemory=8GB\r\n\r\n[experimental]\r\nsparseVhd=true\r\n", true},
		{"header is the last line", "[wsl2]", "[wsl2]\r\nvmIdleTimeout=-1\r\n", true},
		{"other value", "[wsl2]\nmemory=8GB\n  VMIdleTimeout = 60000\n[x]\nvmIdleTimeout=5\n",
			"[wsl2]\nmemory=8GB\nvmIdleTimeout=-1\n[x]\nvmIdleTimeout=5\n", true},
		{"commented key is not the key", "[wsl2]\n# vmIdleTimeout=-1\n", "[wsl2]\nvmIdleTimeout=-1\n# vmIdleTimeout=-1\n", true},
		{"set", "[wsl2]\r\nmemory=8GB\r\nvmIdleTimeout = -1\r\n", "[wsl2]\r\nmemory=8GB\r\nvmIdleTimeout = -1\r\n", false},
		{"only the first wsl2 section", "[wsl2]\nvmIdleTimeout=-1\n[wsl2]\nvmIdleTimeout=7\n", "[wsl2]\nvmIdleTimeout=-1\n[wsl2]\nvmIdleTimeout=7\n", false},
	} {
		got, changed := setIdleTimeout(tc.in)
		if got != tc.want || changed != tc.changed {
			t.Errorf("%s: got %q (%v), want %q (%v)", tc.name, got, changed, tc.want, tc.changed)
		}
	}
}

func TestEnsureIdleTimeout(t *testing.T) {
	fakeTools(t, "windows", func(toolCall) reply { return reply{} })
	home, _ := homeDir()
	path := filepath.Join(home, ".wslconfig")
	h := winHost()

	if set, err := h.IdleTimeoutSet(); set || err != nil {
		t.Fatalf("missing file: %v, %v", set, err)
	}
	if changed, err := h.EnsureIdleTimeout(); !changed || err != nil {
		t.Fatalf("create: %v, %v", changed, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "[wsl2]\r\nvmIdleTimeout=-1\r\n" {
		t.Errorf("created %q", b)
	}

	// A BOM and the other lines survive.
	os.WriteFile(path, []byte("\xEF\xBB\xBF[wsl2]\r\nmemory=8GB\r\n"), 0o644)
	if changed, err := h.EnsureIdleTimeout(); !changed || err != nil {
		t.Fatal(changed, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "\xEF\xBB\xBF[wsl2]\r\nvmIdleTimeout=-1\r\nmemory=8GB\r\n" {
		t.Errorf("edited %q", b)
	}
	if set, err := h.IdleTimeoutSet(); !set || err != nil {
		t.Errorf("read back: %v, %v", set, err)
	}

	// Already set: no write.
	old := time0(t, path)
	if changed, err := h.EnsureIdleTimeout(); changed || err != nil {
		t.Errorf("rerun: %v, %v", changed, err)
	}
	if time0(t, path) != old {
		t.Error("rerun rewrote the file")
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 1 {
		t.Errorf("stray files: %v", entries)
	}

	// UTF-16 is refused, not rewritten.
	u := utf16.Encode([]rune("[wsl2]\r\nmemory=8GB\r\n"))
	b := []byte{0xFF, 0xFE}
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	os.WriteFile(path, b, 0o644)
	if _, err := h.EnsureIdleTimeout(); !errors.Is(err, ErrAutostart) || !strings.Contains(err.Error(), "edit it by hand: [wsl2] vmIdleTimeout=-1") {
		t.Errorf("UTF-16: %v", err)
	}
	if _, err := h.IdleTimeoutSet(); !errors.Is(err, ErrAutostart) {
		t.Errorf("UTF-16 read: %v", err)
	}
	if got, _ := os.ReadFile(path); !reflect.DeepEqual(got, b) {
		t.Error("UTF-16 file rewritten")
	}
}

func time0(t *testing.T, path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime().String() + fi.Name()
}

func utf16LE(s string) string {
	b := []byte{0xFF, 0xFE}
	for _, c := range utf16.Encode([]rune(s)) {
		b = append(b, byte(c), byte(c>>8))
	}
	return string(b)
}

func TestCheckWSL(t *testing.T) {
	for _, tc := range []struct {
		name, out string
		exit      int
		want      string
	}{
		{"utf-16", utf16LE("WSL version: 2.3.26.0\r\nKernel version: 5.15.167.4-1\r\n"), 0, ""},
		{"localized utf-16 without BOM", utf16LE("WSL 版本: 2.0.14.0\r\n")[2:], 0, ""},
		{"utf-8", "WSL version: 2.6.1.0\n", 0, ""},
		{"1.x", utf16LE("WSL version: 1.2.5.0\r\n"), 0, "WSL 1.2.5.0 does not support"},
		{"inbox wsl", utf16LE("Invalid command line option: --version\r\n"), 1, "Invalid command line option"},
		{"silent failure", "", 1, "exit status 1"},
	} {
		tl := fakeTools(t, "windows", func(toolCall) reply { return reply{stdout: tc.out, exit: tc.exit} })
		err := winHost().CheckWSL(context.Background())
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
		} else if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "wsl --update") {
			t.Errorf("%s: err = %v", tc.name, err)
		}
		if !reflect.DeepEqual(tl.names(), []string{"wsl.exe --version"}) {
			t.Errorf("%s: calls %q", tc.name, tl.names())
		}
	}
}

func TestAutostartToolsDropTokens(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret")
	var envs [][]string
	old := command
	t.Cleanup(func() { command = old })
	command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestToolHelper$")
		c.Env = append(os.Environ(), "GO_WANT_TOOL_HELPER=1")
		return c
	}
	_ = envs
	r := filtered(command(context.Background(), "wsl.exe"))
	for _, kv := range r.Env {
		if strings.HasPrefix(kv, "GH_TOKEN=") {
			t.Errorf("child env has %s", kv)
		}
	}
}

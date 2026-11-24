package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// TestHelperProcess is the fake podman: it prints HELPER_STDOUT and
// HELPER_STDERR and exits with HELPER_EXIT.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Fprint(os.Stdout, os.Getenv("HELPER_STDOUT"))
	fmt.Fprint(os.Stderr, os.Getenv("HELPER_STDERR"))
	if os.Getenv("HELPER_EXIT") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

type call struct {
	name string
	args []string
}

// fake installs the hooks: podman is found unless missing, and every command
// runs the helper with the given output and exit.
func fake(t *testing.T, os_ string, missing bool, stdout, stderr string, fail bool) *[]call {
	t.Helper()
	oldLook, oldCmd, oldOS := lookPath, command, goos
	t.Cleanup(func() { lookPath, command, goos = oldLook, oldCmd, oldOS })
	goos = os_
	lookPath = func(string) (string, error) {
		if missing {
			return "", exec.ErrNotFound
		}
		return "/fake/podman", nil
	}
	calls := &[]call{}
	command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		*calls = append(*calls, call{name, args})
		c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperProcess$")
		exit := "0"
		if fail {
			exit = "1"
		}
		c.Env = []string{"GO_WANT_HELPER_PROCESS=1", "HELPER_STDOUT=" + stdout, "HELPER_STDERR=" + stderr, "HELPER_EXIT=" + exit}
		return c
	}
	return calls
}

func TestDetectLinux(t *testing.T) {
	calls := fake(t, "linux", false, "", "", false)
	h, err := Detect(context.Background())
	if err != nil || h.Machine != "" || !h.Running || h.NeedsStart() || h.Describe() != "local podman" {
		t.Fatalf("Detect = %+v, %v", h, err)
	}
	if len(*calls) != 0 {
		t.Errorf("linux ran %v", *calls)
	}
}

func TestDetectPodmanMissing(t *testing.T) {
	for _, g := range []string{"linux", "windows", "darwin"} {
		fake(t, g, true, "", "", false)
		_, err := Detect(context.Background())
		if !errors.Is(err, ErrPodmanMissing) || !strings.Contains(err.Error(), InstallHint) {
			t.Errorf("%s: %v", g, err)
		}
	}
}

func TestDetectUnsupported(t *testing.T) {
	fake(t, "plan9", false, "", "", false)
	if _, err := Detect(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v", err)
	}
}

func TestDetectMachine(t *testing.T) {
	for _, tc := range []struct {
		goos, list string
		name       string
		running    bool
		err        error
	}{
		{"windows", `[{"Name":"other","Default":false,"Running":false},{"Name":"podman-machine-default","Default":true,"Running":true}]`, "podman-machine-default", true, nil},
		{"darwin", `[{"Name":"solo","Default":false,"Running":false}]`, "solo", false, nil},
		{"darwin", `[]`, "", false, ErrMachineMissing},
		{"windows", `[{"Name":"a"},{"Name":"b"}]`, "", false, ErrMachineMissing},
		{"windows", `not json`, "", false, ErrPodman},
	} {
		calls := fake(t, tc.goos, false, tc.list, "", false)
		h, err := Detect(context.Background())
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Errorf("%s: err = %v, want %v", tc.list, err, tc.err)
			}
			if tc.list == `[]` && !strings.Contains(err.Error(), "podman machine init") {
				t.Errorf("no init hint: %v", err)
			}
			continue
		}
		if err != nil || h.Machine != tc.name || h.Running != tc.running || h.NeedsStart() == tc.running {
			t.Errorf("%s: %+v, %v", tc.list, h, err)
		}
		want := []call{{"/fake/podman", []string{"machine", "list", "--format", "json"}}}
		if !reflect.DeepEqual(*calls, want) {
			t.Errorf("calls = %v", *calls)
		}
	}
}

func TestDetectListFails(t *testing.T) {
	fake(t, "windows", false, "", "cannot connect", true)
	_, err := Detect(context.Background())
	if !errors.Is(err, ErrPodman) || !strings.Contains(err.Error(), "cannot connect") {
		t.Errorf("err = %v", err)
	}
}

func TestStart(t *testing.T) {
	calls := fake(t, "darwin", false, "", "", false)
	h := &Host{podman: "/fake/podman", Machine: "m1"}
	if err := h.Start(context.Background()); err != nil || !h.Running {
		t.Fatalf("Start = %v, running %v", err, h.Running)
	}
	if err := h.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []call{{"/fake/podman", []string{"machine", "start", "m1"}}}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %v", *calls)
	}

	fake(t, "darwin", false, "", "another VM is already running", true)
	h = &Host{podman: "/fake/podman", Machine: "m1"}
	err := h.Start(context.Background())
	if !errors.Is(err, ErrMachineStart) || !strings.Contains(err.Error(), "already running") || h.Running {
		t.Errorf("err = %v", err)
	}
}

func TestPodmanArgs(t *testing.T) {
	calls := fake(t, "linux", false, "", "", false)
	local := &Host{podman: "/fake/podman", Running: true}
	local.Podman(context.Background(), "volume", "exists", "v")
	vm := &Host{podman: "/fake/podman", Machine: "my machine", Running: true}
	vm.Podman(context.Background(), "volume", "exists", "v")
	want := []call{
		{"/fake/podman", []string{"volume", "exists", "v"}},
		{"/fake/podman", []string{"--connection", "my machine", "volume", "exists", "v"}},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %v", *calls)
	}
	if vm.Describe() != "podman machine my machine" {
		t.Errorf("Describe = %q", vm.Describe())
	}
}

func TestChildEnvDropsTokens(t *testing.T) {
	got := childEnv([]string{"PATH=/bin", "GH_TOKEN=a", "GITHUB_TOKEN=b", "gh_enterprise_token=c", "GITHUB_ENTERPRISE_TOKEN=d", "GH_HOST=x", "CONTAINER_HOST=y"})
	want := []string{"PATH=/bin", "GH_HOST=x", "CONTAINER_HOST=y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("childEnv = %v", got)
	}
	t.Setenv("GH_TOKEN", "secret")
	h := &Host{podman: "podman"}
	oldCmd := command
	t.Cleanup(func() { command = oldCmd })
	command = exec.CommandContext
	for _, kv := range h.Podman(context.Background(), "version").Env {
		if strings.HasPrefix(kv, "GH_TOKEN=") {
			t.Errorf("child env has %s", kv)
		}
	}
}

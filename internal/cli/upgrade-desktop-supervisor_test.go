package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestUpgradeSupervisorPreservesActualChildOutputAndExit(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("system Python absent")
	}
	for _, code := range []int{0, 1, 2, 7} {
		child := "import sys; print('PR https://github.com/o/r/pull/9'); sys.exit(" +
			strconv.Itoa(code) + ")"
		cmd := exec.Command("/usr/bin/python3", "-I", "-B", "-c", upgradeExitScript,
			"/usr/bin/python3", "-I", "-B", "-c", child)
		var output bytes.Buffer
		cmd.Stderr = &output
		control, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if output.String() != "PR https://github.com/o/r/pull/9\n" {
			t.Fatal("child output lost", output.String())
		}
		err = decodeUpgradeExit(control)
		var exit *CommandError
		if code == 0 && err != nil || code != 0 &&
			(!errors.As(err, &exit) || exit.Exit != code) {
			t.Fatal(code, err)
		}
	}
}

func TestUpgradeProbeReportsUnsupportedAndUnavailableSeparately(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("system Python absent")
	}
	for _, diagnostic := range []string{"UNKNOWN_COMMAND_USE_HELP", "INSPECTION_FAILED"} {
		child := "import sys; print('" + diagnostic + "', file=sys.stderr); sys.exit(1)"
		cmd := exec.Command("/usr/bin/python3", "-I", "-B", "-c", upgradeProbeScript,
			"/usr/bin/python3", "-I", "-B", "-c", child)
		data, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Gap string `json:"gap"`
		}
		if json.Unmarshal(data, &result) != nil {
			t.Fatal(string(data))
		}
		want := "GUEST_UPGRADE_OBSERVATION_UNAVAILABLE"
		if diagnostic == "UNKNOWN_COMMAND_USE_HELP" {
			want = "INSTALLED_CLI_UPGRADE_TRANSPORT_UNSUPPORTED"
		}
		if result.Gap != want {
			t.Fatal(result.Gap, want)
		}
	}
}

func TestUpgradeWindowsRoutingNeverUsesDesktopPublisher(t *testing.T) {
	p := &upgradeDesktopFake{running: true, adoption: "required"}
	app, _ := upgradeDesktopApp(t, p)
	// A nil API would fail if Windows fell through to the local PR publisher.
	err := app.routeUpgrade([]string{"--repo=o/r", "--apply"}, "windows", io.Discard)
	if err == nil || err.Error() != "NO_ADOPTED_WSL_INSTALLATION_USE_INIT" {
		t.Fatal(err)
	}
}

func TestUpgradeCancelledTransportDoesNotRunFixture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &upgradeDesktopFake{running: true}
	transport := upgradeDesktopTransport{ctx: ctx, process: p}
	_, err := transport.DesktopQuery("wsl.exe", []string{"--list"})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestUpgradeNativeExecutionDistinguishesOutcomes(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("system Python absent")
	}
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "missing-wsl")
	err := runResolvedUpgradeGuest(ctx, missing, nil, io.Discard)
	if err == nil || err.Error() != "GUEST_LAUNCH_FAILED" {
		t.Fatal(err)
	}
	err = runResolvedUpgradeGuest(ctx, "/usr/bin/python3",
		[]string{"-I", "-B", "-c", "raise SystemExit(1)"}, io.Discard)
	if err == nil || err.Error() != "GUEST_TRANSPORT_LOST_READ_BACK_REQUIRED" {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	err = runResolvedUpgradeGuest(cancelled, "/usr/bin/python3", nil, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var output bytes.Buffer
	args := []string{"-I", "-B", "-c", upgradeExitScript,
		"/usr/bin/python3", "-I", "-B", "-c", "print('pending'); raise SystemExit(2)"}
	err = runResolvedUpgradeGuest(ctx, "/usr/bin/python3", args, &output)
	var command *CommandError
	if !errors.As(err, &command) || command.Exit != 2 || output.String() != "pending\n" {
		t.Fatal(err, output.String())
	}
}

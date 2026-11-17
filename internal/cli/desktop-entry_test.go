package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

const desktopTestIdentity = "0123456789abcdef0123456789abcdef"

type desktopFake struct {
	Calls    []string
	Running  bool
	CLI      bool
	Arch     string
	Identity string
	Changed  bool
	Reads    int
	Entered  []string
}

func (p *desktopFake) Run(tool string, args []string, _ []byte) ([]byte, error) {
	p.Calls = append(p.Calls, tool+" "+strings.Join(args, " "))
	if tool != "multipass" {
		return nil, errors.New("unexpected tool")
	}
	if slices.Equal(args, []string{"list", "--format", "json"}) {
		state := "Stopped"
		if p.Running {
			state = "Running"
		}
		return []byte(`{"list":[{"name":"guest","state":"` + state +
			`","release":"Ubuntu 24.04 LTS"}]}`), nil
	}
	if slices.Equal(args, []string{"start", "guest"}) {
		p.Running = true
		return nil, nil
	}
	if len(args) < 4 || !slices.Equal(args[:3], []string{"exec", "guest", "--"}) {
		return nil, errors.New("unsafe argv")
	}
	command := args[3:]
	switch strings.Join(command, " ") {
	case "cat /etc/machine-id":
		p.Reads++
		if p.Changed && p.Reads > 1 {
			return []byte(strings.Repeat("a", 32)), nil
		}
		return []byte(p.Identity), nil
	case "gh extension list":
		if !p.CLI {
			return nil, errors.New("not installed")
		}
		return []byte("gh nibfin"), nil
	case "cat /etc/os-release":
		return []byte("ID=ubuntu\nVERSION_ID=24.04"), nil
	case "uname -m":
		return []byte("aarch64"), nil
	case "cat /proc/1/comm":
		return []byte("systemd"), nil
	}
	if slices.Equal(command, []string{"gh", "nibfin", "preflight", "--repo", "o/r"}) {
		o := linuxTargetObservation{Distribution: "ubuntu", Version: "24.04",
			KernelArch: "aarch64", RuntimeArch: p.Arch, Architecture: p.Arch,
			ExecutableMachine: 183, Readiness: "unverified"}
		for _, name := range []string{"ubuntu_24_04", "native_architecture",
			"systemd", "cgroup_v2", "apparmor", "host_namespaces"} {
			o.Checks = append(o.Checks, capability{name, "observed", "HOST_OBSERVED_ONLY"})
		}
		data, _ := json.Marshal(map[string]any{"linux_target": o, "host_os": "linux",
			"linux_identity": p.Identity, "protected_execution": "unverified",
			"shared_host_adopted": true})
		return data, &CommandError{Code: "GUEST_EXIT_2", Exit: 2}
	}
	return nil, errors.New("unexpected guest command")
}

func (p *desktopFake) EnterGuest(_ context.Context, tool string,
	args []string, _ io.Writer) error {
	p.Entered = append([]string{tool}, args...)
	return nil
}

func TestDesktopPreviewNeverOpensStoppedGuest(t *testing.T) {
	p := &desktopFake{Identity: desktopTestIdentity, Arch: "arm64", CLI: true}
	var out bytes.Buffer
	err := (App{Process: p}).runDesktop([]string{"preflight", "--repo", "o/r"},
		"darwin", &out)
	var exit *CommandError
	if !errors.As(err, &exit) || exit.Exit != 2 {
		t.Fatal(err)
	}
	if len(p.Calls) != 1 || len(p.Entered) != 0 || p.Running {
		t.Fatal("preview opened or inspected stopped guest", p.Calls)
	}
	if !strings.Contains(out.String(), "STOPPED_GUEST_NOT_INSPECTED") ||
		!strings.Contains(out.String(), `"protected_execution": "unverified"`) {
		t.Fatal(out.String())
	}
}

func TestDesktopConsentedHandoffBindsGuestIdentityAndArchitecture(t *testing.T) {
	p := &desktopFake{Identity: desktopTestIdentity, Arch: "arm64", CLI: true}
	var out bytes.Buffer
	err := (App{Process: p}).runDesktop([]string{"init", "--repo", "o/r", "--apply",
		"--target", "multipass:guest", "--consent-open-target"}, "darwin", &out)
	if err != nil {
		t.Fatal(err, out.String())
	}
	expected := []string{"multipass", "exec", "guest", "--", "gh", "nibfin",
		"init", "--repo", "o/r", "--apply", "--linux-target-identity",
		desktopTestIdentity, "--linux-target-arch", "arm64"}
	if !slices.Equal(p.Entered, expected) {
		t.Fatal(p.Entered)
	}
	if p.Reads < 2 {
		t.Fatal("identity not rechecked")
	}
	if !strings.Contains(out.String(), "handoff") {
		t.Fatal("plan missing")
	}
}

func TestDesktopReplacedOrWrongArchitectureGuestCannotDelegate(t *testing.T) {
	for _, scenario := range []string{"changed", "wrong_arch", "missing_cli"} {
		p := &desktopFake{Identity: desktopTestIdentity, Arch: "arm64", CLI: true,
			Running: true}
		switch scenario {
		case "changed":
			p.Changed = true
		case "wrong_arch":
			p.Arch = "amd64"
		case "missing_cli":
			p.CLI = false
		}
		var out bytes.Buffer
		err := (App{Process: p}).runDesktop([]string{"init", "--repo", "o/r", "--apply",
			"--target", "multipass:guest", "--consent-open-target"}, "darwin", &out)
		if err == nil || len(p.Entered) != 0 {
			t.Fatalf("%s: %v", scenario, err)
		}
		if scenario == "missing_cli" &&
			!strings.Contains(out.String(), "INSTALL_LINUX_CLI_AND_RUN_PREFLIGHT") {
			t.Fatal(out.String())
		}
	}
}

func TestDesktopRejectsPathOptionsAndImplicitConsent(t *testing.T) {
	for _, option := range []string{"--directory", "--runner-directory",
		"--handoff-directory", "--quick"} {
		if _, err := parseDesktopOptions([]string{"init", "--repo", "o/r", option,
			"/desktop/path"}); err == nil {
			t.Fatal(option)
		}
	}
	o, err := parseDesktopOptions([]string{"init", "--repo=o/r", "--apply",
		"--consent-open-target=false"})
	if err != nil || o.Consent || !o.Apply {
		t.Fatal(o, err)
	}
	if _, err := parseDesktopOptions([]string{"preflight", "--repo", "o/r",
		"--consent-open-target"}); err == nil {
		t.Fatal("preflight accepted consent")
	}
}

func TestDesktopGuestArgumentsAndEnvironmentStaySeparated(t *testing.T) {
	target := desktopTarget{Provider: "wsl", Name: "Ubuntu Dev"}
	tool, args := guestCommand(target, "gh", "nibfin", "init", "--repo", "o/r")
	expected := []string{"--distribution", "Ubuntu Dev", "--cd", "~", "--exec",
		"gh", "nibfin", "init", "--repo", "o/r"}
	if tool != "wsl.exe" || !slices.Equal(args, expected) {
		t.Fatal(tool, args)
	}
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "WSLENV", "CODEX_HOME",
		"GH_CONFIG_DIR", "HOME", "USERPROFILE"} {
		t.Setenv(key, "desktop-secret")
	}
	for _, entry := range desktopEnvironment() {
		if strings.Contains(entry, "desktop-secret") {
			t.Fatal("desktop state forwarded")
		}
	}
}

func TestDesktopSanitizesGuestFactsAndRejectsMissingChecks(t *testing.T) {
	observation := linuxTargetObservation{Distribution: "ubuntu", Version: "24.04",
		KernelArch: "aarch64", RuntimeArch: "arm64", Architecture: "arm64",
		Readiness: "unverified", Checks: []capability{
			{"systemd", "observed", "SECRET_FROM_PROVIDER"},
			{"untrusted-name", "observed", "DO_NOT_EXPORT"},
			{"dedicated_engine", "unverified", "CORE_PREPARATION_REQUIRED"},
		}}
	sanitized := sanitizedDesktopLinux(observation)
	if sanitized == nil || requireLinuxObservation(observation) == nil {
		t.Fatal("incomplete observation accepted")
	}
	data, _ := json.Marshal(sanitized)
	if strings.Contains(string(data), "SECRET") ||
		strings.Contains(string(data), "untrusted-name") {
		t.Fatal(string(data))
	}
	engineGap := false
	for _, check := range sanitized.Checks {
		engineGap = engineGap || check.Name == "dedicated_engine" &&
			check.Status == "unverified"
	}
	if !engineGap {
		t.Fatal("engine preparation gap lost from preview")
	}
	observation.KernelArch = "aarch64\nmalicious diagnostic"
	if sanitizedDesktopLinux(observation) != nil {
		t.Fatal("unsafe facts accepted")
	}
}

func TestDesktopEntrypointDoesNotUseDesktopRepositoryAPI(t *testing.T) {
	if !desktopHost() {
		t.Skip("native desktop dispatcher exercised by desktop CI")
	}
	// No API is supplied. Discovery failures must still show recovery, without
	// reaching desktop repository planning or mutations.
	var out bytes.Buffer
	err := (App{Process: &inspectionProcess{missing: true}}).Run(
		[]string{"init", "--repo", "o/r"}, &out)
	if err != nil || !strings.Contains(out.String(), "guidance") ||
		strings.Contains(out.String(), "secret") {
		t.Fatal(err, out.String())
	}
}

func TestDesktopRecheckRejectsArchitectureChangedSincePlan(t *testing.T) {
	p := &desktopFake{Identity: desktopTestIdentity, Arch: "arm64", CLI: true, Running: true}
	old := desktopTarget{Provider: "multipass", Name: "guest",
		Identity: desktopTestIdentity,
		Running:  true, Status: "eligible",
		Linux: &linuxTargetObservation{Architecture: "amd64"}}
	if _, err := recheckDesktop(p, old, "darwin", "o/r"); err == nil {
		t.Fatal("changed architecture silently replaced the original handoff plan")
	}
}

func TestDesktopResumePreservesRequestedOptionsAndEnablesApply(t *testing.T) {
	options, err := parseDesktopOptions([]string{"init", "--repo", "o/r",
		"--execution-repo", "o/shared", "--apply=false"})
	if err != nil {
		t.Fatal(err)
	}
	commands := desktopResume(desktopTarget{Provider: "wsl", Name: "Ubuntu Dev"}, options)
	for _, command := range commands {
		if strings.Contains(command, "--apply=false") {
			t.Fatal("preview-only flag retained in recovery command", command)
		}
	}
	if !strings.Contains(commands[2], "'--execution-repo' 'o/shared'") ||
		!strings.Contains(commands[2], "'--apply'") ||
		!strings.Contains(commands[3], "o/shared") {
		t.Fatal(commands)
	}
}

type cancelledDesktop struct {
	desktopFake
	Received context.Context
}

func (p *cancelledDesktop) EnterGuest(ctx context.Context, _ string,
	_ []string, _ io.Writer) error {
	p.Received = ctx
	return ctx.Err()
}

func TestDesktopCancellationRequiresGuestReadBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	process := &cancelledDesktop{desktopFake: desktopFake{
		Identity: desktopTestIdentity, Arch: "arm64", CLI: true, Running: true}}
	var out bytes.Buffer
	err := (App{Process: process, Context: ctx}).runDesktop([]string{
		"init", "--repo", "o/r", "--apply", "--consent-open-target"}, "darwin", &out)
	if process.Received != ctx || err == nil ||
		!strings.Contains(err.Error(), "READ_BACK_REQUIRED") {
		t.Fatal("cancelled delegation must not report completion", err)
	}
}

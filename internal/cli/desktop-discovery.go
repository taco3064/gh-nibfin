package cli

import (
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"time"
)

const desktopQueryTimeout = 30 * time.Second

func desktopHost() bool {
	return runtime.GOOS == "windows" ||
		runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

func discoverDesktop(p Process, host, repo string) ([]desktopTarget, error) {
	var targets []desktopTarget
	var err error
	if host == "windows" {
		targets, err = wslInventory(p)
	} else {
		data, e := desktopQuery(p, "multipass", "list", "--format", "json")
		if e != nil {
			return nil, errors.New("MULTIPASS_INVENTORY_UNAVAILABLE")
		}
		targets, err = parseMultipass(data)
	}
	if err != nil {
		return nil, err
	}
	for i := range targets {
		if targets[i].Running && targets[i].Status != "incompatible" {
			inspectDesktopTarget(p, &targets[i], repo, host)
		}
	}
	return targets, nil
}

func inspectDesktopTarget(p Process, t *desktopTarget, repo, host string) {
	t.Gap = "GUEST_OBSERVATION_UNAVAILABLE"
	id, err := guestQuery(p, *t, "cat", "/etc/machine-id")
	identity := strings.TrimSpace(string(id))
	if err != nil || !machineIdentity.MatchString(identity) {
		coarseDesktopTarget(p, t)
		t.Gap = "GUEST_IDENTITY_UNAVAILABLE"
		return
	}
	t.Identity = identity
	// Probe presence separately: exit 2 is normal for preflight, not a missing CLI.
	_, err = guestQuery(p, *t, "gh", "extension", "list")
	if err != nil {
		coarseDesktopTarget(p, t)
		return
	}
	data, err := guestQuery(p, *t, "gh", "nibfin", "preflight", "--repo", repo)
	var exit *CommandError
	if err != nil && (!errors.As(err, &exit) || exit.Exit != 2) {
		coarseDesktopTarget(p, t)
		return
	}
	if len(data) > desktopOutputLimit {
		return
	}
	var report struct {
		HostOS    string                  `json:"host_os"`
		Identity  string                  `json:"linux_identity"`
		Adopted   bool                    `json:"shared_host_adopted"`
		Linux     *linuxTargetObservation `json:"linux_target"`
		Checks    []capability            `json:"checks"`
		Readiness string                  `json:"protected_execution"`
	}
	if !boundedDesktopJSON(data) || json.Unmarshal(data, &report) != nil ||
		report.Linux == nil ||
		report.HostOS != "linux" || report.Identity != identity ||
		report.Readiness != "unverified" {
		return
	}
	t.GuestChecks = sanitizedDesktopChecks(report.Checks, []string{
		"gh", "git", "node", "codex", "codex_auth", "repository_admin",
		"shared_runner_pool",
	}, false)
	o := report.Linux
	t.Linux = sanitizedDesktopLinux(*o)
	if t.Linux == nil {
		return
	}
	// Only finite, bounded facts are exported. Never relay arbitrary provider text.
	if o.Distribution != "ubuntu" || o.Version != "24.04" {
		t.Status, t.Gap = "incompatible", "MANAGED_TARGET_REQUIRES_UBUNTU_24_04"
		return
	}
	arch := o.Architecture
	if arch != "amd64" && arch != "arm64" {
		return
	}
	if host == "darwin" && arch != "arm64" {
		t.Status, t.Gap = "incompatible", "APPLE_SILICON_REQUIRES_NATIVE_ARM64"
		return
	}
	// Eligibility remains the same maintained rule used by the Linux entry.
	if requireLinuxObservation(*o) != nil {
		t.Gap = "GUEST_CAPABILITIES_UNVERIFIED_SEE_LINUX_CHECKS"
		return
	}
	t.Status, t.Gap = "eligible", ""
	t.Adopted = report.Adopted
	for _, check := range report.Checks {
		if check.Name == "shared_runner_pool" && check.Status == "observed" &&
			check.Code == "SHARED_RUNNER_SLOTS_ONLINE" {
			t.Adopted = true
		}
	}
}

func coarseDesktopTarget(p Process, t *desktopTarget) {
	t.Gap = "INSTALL_LINUX_CLI_AND_RUN_PREFLIGHT"
	data, err := guestQuery(p, *t, "cat", "/etc/os-release")
	if err != nil || len(data) > 16384 {
		return
	}
	distro, version, err := linuxRelease(data)
	if err != nil {
		return
	}
	if distro != "ubuntu" || version != "24.04" {
		t.Status, t.Gap = "incompatible", "MANAGED_TARGET_REQUIRES_UBUNTU_24_04"
		return
	}
	kernel, err := guestQuery(p, *t, "uname", "-m")
	if err != nil {
		return
	}
	arch := strings.TrimSpace(string(kernel))
	if arch != "x86_64" && arch != "aarch64" {
		return
	}
	if t.Provider == "multipass" && arch == "x86_64" {
		t.Status, t.Gap = "incompatible", "APPLE_SILICON_REQUIRES_NATIVE_ARM64"
	}
	service, err := guestQuery(p, *t, "cat", "/proc/1/comm")
	if err != nil {
		return
	}
	status := "unverified"
	if strings.TrimSpace(string(service)) == "systemd" {
		status = "observed"
	}
	t.Linux = &linuxTargetObservation{Distribution: distro, Version: version,
		KernelArch: arch, Readiness: "unverified", Checks: []capability{
			{"systemd", status, "COARSE_OBSERVATION_ONLY"},
		}}
}

func wslInventory(p Process) ([]desktopTarget, error) {
	quiet, e := desktopQuery(p, "wsl.exe", "--list", "--quiet")
	if e != nil {
		return nil, errors.New("WSL_INVENTORY_UNAVAILABLE")
	}
	verbose, e := desktopQuery(p, "wsl.exe", "--list", "--verbose")
	if e != nil {
		return nil, errors.New("WSL_INVENTORY_UNAVAILABLE")
	}
	active, e := desktopQuery(p, "wsl.exe", "--list", "--running", "--quiet")
	if e != nil {
		return nil, errors.New("WSL_RUNNING_INVENTORY_UNAVAILABLE")
	}
	return parseWSL(quiet, verbose, active)
}

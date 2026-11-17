package cli

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func boundedLinuxFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(data) > 16384 {
		return nil, errors.New("LINUX_OBSERVATION_UNAVAILABLE")
	}
	return data, nil
}

func nativeArchitecture() (string, uint16, error) {
	var uname syscall.Utsname
	if syscall.Uname(&uname) != nil {
		return "", 0, errors.New("LINUX_UNAME_UNAVAILABLE")
	}
	var name []byte
	for _, char := range uname.Machine {
		if char == 0 {
			break
		}
		name = append(name, byte(char))
	}
	file, err := os.Open("/proc/self/exe")
	if err != nil {
		return "", 0, errors.New("LINUX_EXECUTABLE_UNAVAILABLE")
	}
	defer file.Close()
	header := make([]byte, 64)
	if _, err := io.ReadFull(file, header); err != nil {
		return "", 0, errors.New("LINUX_EXECUTABLE_UNAVAILABLE")
	}
	machine, err := sharedELFMachine(header)
	return string(name), machine, err
}

func nativeSharedTarget(c sharedPlatform) (sharedTarget, error) {
	kernel, machine, err := nativeArchitecture()
	if err != nil {
		return sharedTarget{}, err
	}
	return c.selectTarget(kernel, runtime.GOARCH, machine)
}

func observeLinuxTarget(c sharedPlatform, read func(string) ([]byte, error),
	kernel, runtimeArch string, machine uint16) linuxTargetObservation {
	o := linuxTargetObservation{KernelArch: kernel, RuntimeArch: runtimeArch,
		ExecutableMachine: machine, Readiness: "unverified"}
	check := func(name string, valid bool, code string) {
		status := "unverified"
		if valid {
			status, code = "observed", "HOST_CAPABILITY_OBSERVED_ONLY"
		}
		o.Checks = append(o.Checks, capability{name, status, code})
	}
	data, err := read("/etc/os-release")
	distro, version, parseErr := linuxRelease(data)
	o.Distribution, o.Version = distro, version
	check("ubuntu_24_04", err == nil && parseErr == nil && distro == "ubuntu" &&
		version == "24.04", "MANAGED_TARGET_REQUIRES_UBUNTU_24_04")
	target, err := c.selectTarget(kernel, runtimeArch, machine)
	check("native_architecture", err == nil, "MANAGED_TARGET_ARCHITECTURE_MISMATCH")
	if err == nil {
		o.Architecture = target.Arch
	}
	data, err = read("/proc/1/comm")
	check("systemd", err == nil && strings.TrimSpace(string(data)) == "systemd",
		"MANAGED_TARGET_REQUIRES_SYSTEMD")
	data, err = read("/sys/fs/cgroup/cgroup.controllers")
	controllers := " " + strings.Join(strings.Fields(string(data)), " ") + " "
	check("cgroup_v2", err == nil && strings.Contains(controllers, " cpu ") &&
		strings.Contains(controllers, " memory ") && strings.Contains(controllers, " pids "),
		"MANAGED_TARGET_REQUIRES_CGROUP_CONTROLLERS")
	data, err = read("/sys/module/apparmor/parameters/enabled")
	check("apparmor", err == nil && strings.TrimSpace(string(data)) == "Y",
		"MANAGED_TARGET_REQUIRES_APPARMOR")
	data, err = read("/proc/sys/user/max_user_namespaces")
	namespaces, parseErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	valid := err == nil && parseErr == nil && namespaces > 0
	for _, table := range []string{"uid_map", "gid_map"} {
		data, err = read("/proc/self/" + table)
		valid = valid && err == nil &&
			strings.Join(strings.Fields(string(data)), " ") == "0 0 4294967295"
	}
	for _, ns := range []string{"pid", "mnt", "user"} {
		self, selfErr := read("/proc/self/ns/" + ns)
		// An ordinary SSH user can inspect its own namespaces. Reading root PID 1's
		// namespace links may require ptrace permission and is not an install prerequisite.
		valid = valid && selfErr == nil && strings.HasPrefix(string(self), ns+":[") &&
			strings.HasSuffix(string(self), "]")
	}
	check("host_namespaces", valid, "MANAGED_TARGET_REQUIRES_HOST_BOUNDARIES")
	return o
}

func probeLinuxTarget(c sharedPlatform, process Process) linuxTargetObservation {
	kernel, machine, _ := nativeArchitecture()
	read := func(path string) ([]byte, error) {
		if strings.Contains(path, "/ns/") {
			value, err := os.Readlink(path)
			return []byte(value), err
		}
		return boundedLinuxFile(path)
	}
	o := observeLinuxTarget(c, read, kernel, runtime.GOARCH, machine)
	o.Checks = append(o.Checks, observeDedicatedDocker(process, c, o.Architecture)...)
	o.Checks = append(o.Checks, observeDockerServices(process)...)
	return o
}

func requireManagedLinux(c sharedPlatform, process Process) error {
	return requireLinuxObservation(probeLinuxTarget(c, process))
}

func managedTargetPreview(api API, process Process) any {
	c, err := verifiedSharedPlatform(api)
	observation := probeLinuxTarget(c, process)
	if err != nil {
		observation.Checks = append(observation.Checks, capability{
			"producer_platform", "unverified", "VERIFIED_CORE_PLATFORM_UNAVAILABLE"})
	}
	return observation
}

func nativeLinuxTarget() bool { return true }

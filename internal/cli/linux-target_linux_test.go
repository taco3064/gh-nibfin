package cli

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func eligibleLinuxFiles() map[string]string {
	return map[string]string{
		"/etc/os-release":                         "ID=ubuntu\nVERSION_ID=\"24.04\"\n",
		"/proc/1/comm":                            "systemd\n",
		"/sys/fs/cgroup/cgroup.controllers":       "cpu memory pids\n",
		"/sys/module/apparmor/parameters/enabled": "Y\n",
		"/proc/sys/user/max_user_namespaces":      "1000\n",
		"/proc/self/uid_map":                      "0 0 4294967295\n",
		"/proc/self/gid_map":                      "0 0 4294967295\n",
		"/proc/self/ns/pid":                       "pid:[1]",
		"/proc/self/ns/mnt":                       "mnt:[1]",
		"/proc/self/ns/user":                      "user:[1]",
	}
}

func TestLinuxTargetObservations(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		c := platformFixture()
		for _, broken := range []string{"", "/etc/os-release", "/proc/1/comm",
			"/sys/fs/cgroup/cgroup.controllers", "/sys/module/apparmor/parameters/enabled",
			"/proc/sys/user/max_user_namespaces", "/proc/self/uid_map", "/proc/self/ns/mnt"} {
			files := eligibleLinuxFiles()
			if broken != "" {
				delete(files, broken)
			}
			read := func(path string) ([]byte, error) {
				value, ok := files[path]
				if !ok {
					return nil, errors.New("unavailable observation")
				}
				return []byte(value), nil
			}
			o := observeLinuxTarget(c, read, arch, arch, c.Targets[arch].ELFMachine)
			valid := true
			for _, check := range o.Checks {
				valid = valid && check.Status == "observed"
			}
			if valid != (broken == "") || o.Readiness != "unverified" {
				t.Fatalf("%s missing %s: %+v", arch, broken, o)
			}
		}
	}
	for _, data := range []string{"ID=debian\nVERSION_ID=12", "ID=ubuntu\nVERSION_ID=22.04",
		"ID=ubuntu\nID=debian\nVERSION_ID=24.04", "ID=$(touch bad)\nVERSION_ID=24.04"} {
		files := eligibleLinuxFiles()
		files["/etc/os-release"] = data
		read := func(path string) ([]byte, error) { return []byte(files[path]), nil }
		o := observeLinuxTarget(platformFixture(), read, "x86_64", "amd64", 62)
		if o.Checks[0].Status == "observed" {
			t.Fatal("unsupported distribution accepted")
		}
	}
}

func syntheticELF(machine uint16) []byte {
	bytes := make([]byte, 128)
	copy(bytes, "\x7fELF\x02\x01\x01")
	binary.LittleEndian.PutUint16(bytes[16:], 2)
	binary.LittleEndian.PutUint16(bytes[18:], machine)
	binary.LittleEndian.PutUint32(bytes[20:], 1)
	return bytes
}

func TestSharedExecutableRejectsWrongTargetAndTampering(t *testing.T) {
	c := platformFixture()
	path := filepath.Join(t.TempDir(), "node")
	for arch, record := range c.Targets {
		target := sharedTarget{record, arch, c.RunnerVersion, c.NodeVersion}
		data := syntheticELF(record.ELFMachine)
		if err := os.WriteFile(path, data, 0700); err != nil {
			t.Fatal(err)
		}
		digest := sha256Hash(data)
		if err := verifySharedExecutableFile(path, digest, target); err != nil {
			t.Fatal(err)
		}
		other := target
		other.ELFMachine++
		if verifySharedExecutableFile(path, digest, other) == nil {
			t.Fatal("digest-valid wrong-target binary accepted")
		}
		if verifySharedExecutableFile(path, strings.Repeat("f", 64), target) == nil {
			t.Fatal("tampered executable accepted")
		}
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
		if verifySharedExecutableFile(path, digest, target) == nil {
			t.Fatal("non-executable file accepted")
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDockerOverridesAreNeverProbed(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///home/user/.docker/desktop/docker.sock")
	checks := observeDedicatedDocker(nil, platformFixture(), "amd64")
	if checks[0].Status != "unverified" ||
		checks[0].Code != "DOCKER_OVERRIDE_REQUIRES_ADMINISTRATOR_INSPECTION" {
		t.Fatal(checks)
	}
}

func TestFreshEngineGapDoesNotWaiveOtherRequirements(t *testing.T) {
	files := eligibleLinuxFiles()
	read := func(path string) ([]byte, error) { return []byte(files[path]), nil }
	o := observeLinuxTarget(platformFixture(), read, "aarch64", "arm64", 183)
	o.Checks = append(o.Checks,
		capability{"dedicated_engine", "unverified", "CORE_PREPARATION_REQUIRED"},
		capability{"nibfin-engine.service", "unverified", "SERVICE_STATE_UNAVAILABLE"})
	if err := requireLinuxObservation(o); err != nil {
		t.Fatalf("fresh engine must reach Core preparation: %v", err)
	}
	if o.Readiness != "unverified" {
		t.Fatal("probe claimed protected readiness")
	}
	o.Checks = append(o.Checks,
		capability{"docker_endpoint", "unverified", "MANAGED_DOCKER_TARGET_MISMATCH"})
	if requireLinuxObservation(o) == nil {
		t.Fatal("Docker mismatch waived")
	}
	o.Checks = o.Checks[:len(o.Checks)-1]
	o.Checks[2].Status = "unverified"
	if requireLinuxObservation(o) == nil {
		t.Fatal("missing host boundary waived")
	}
}

func TestDockerTargetSelectionRejectsDesktopAndArchitectureConflict(t *testing.T) {
	c := platformFixture()
	info := linuxDockerInfo{"linux", "Ubuntu 24.04", "aarch64"}
	if !compatibleLinuxDocker(c, "arm64", info) {
		t.Fatal("native engine not observed")
	}
	if compatibleLinuxDocker(c, "amd64", info) {
		t.Fatal("wrong-target daemon accepted")
	}
	info.OperatingSystem = "Docker Desktop"
	if compatibleLinuxDocker(c, "arm64", info) {
		t.Fatal("Desktop daemon accepted")
	}
	info.OperatingSystem, info.OSType = "Ubuntu", "windows"
	if compatibleLinuxDocker(c, "arm64", info) {
		t.Fatal("non-Linux daemon accepted")
	}
}

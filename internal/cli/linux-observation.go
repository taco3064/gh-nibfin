package cli

import (
	"errors"
	"strings"
)

type linuxTargetObservation struct {
	Distribution      string       `json:"distribution"`
	Version           string       `json:"version"`
	KernelArch        string       `json:"kernel_arch"`
	RuntimeArch       string       `json:"runtime_arch"`
	ExecutableMachine uint16       `json:"executable_elf_machine"`
	Architecture      string       `json:"architecture,omitempty"`
	Checks            []capability `json:"checks"`
	Readiness         string       `json:"protected_execution"`
}

// os-release is data, never shell input. Only the two finite identity fields
// are consumed; escapes, expansion and duplicate identities are rejected.
func linuxRelease(data []byte) (string, string, error) {
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key != "ID" && key != "VERSION_ID" {
			continue
		}
		if _, exists := values[key]; exists {
			return "", "", errors.New("LINUX_OS_RELEASE_INVALID")
		}
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' ||
			value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		const characters = "abcdefghijklmnopqrstuvwxyz0123456789.-"
		if len(value) > 32 || strings.Trim(value, characters) != "" {
			return "", "", errors.New("LINUX_OS_RELEASE_INVALID")
		}
		values[key] = value
	}
	if values["ID"] == "" || values["VERSION_ID"] == "" {
		return "", "", errors.New("LINUX_OS_RELEASE_UNAVAILABLE")
	}
	return values["ID"], values["VERSION_ID"], nil
}

var linuxPrerequisiteChecks = []string{
	"ubuntu_24_04", "native_architecture", "systemd", "cgroup_v2",
	"apparmor", "host_namespaces",
}

func requireLinuxObservation(o linuxTargetObservation) error {
	seen := map[string]bool{}
	for _, check := range o.Checks {
		if seen[check.Name] {
			return errors.New("LINUX_OBSERVATION_AMBIGUOUS")
		}
		seen[check.Name] = true
		// Core owns preparation of the dedicated engine. Its absence is a gap,
		// not a reason to bypass the consented preparation operation.
		if check.Name == "dedicated_engine" || strings.HasSuffix(check.Name, ".service") {
			continue
		}
		if check.Status != "observed" {
			return errors.New(check.Code)
		}
	}
	for _, name := range linuxPrerequisiteChecks {
		if !seen[name] {
			return errors.New("LINUX_OBSERVATION_INCOMPLETE")
		}
	}
	return nil
}

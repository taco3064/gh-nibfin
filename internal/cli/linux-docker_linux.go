package cli

import (
	"encoding/json"
	"os"
	"strings"
)

type linuxDockerInfo struct{ OSType, OperatingSystem, Architecture string }

func compatibleLinuxDocker(c sharedPlatform, arch string, info linuxDockerInfo) bool {
	target, ok := c.Targets[arch]
	matches := false
	for _, alias := range target.Observations {
		matches = matches || alias == info.Architecture
	}
	return ok && matches && info.OSType == "linux" &&
		!strings.Contains(strings.ToLower(info.OperatingSystem), "docker desktop")
}

func observeDedicatedDocker(process Process, c sharedPlatform, arch string) []capability {
	endpoint := capability{"docker_endpoint", "observed", "FIXED_LOCAL_ENDPOINT_ONLY"}
	engine := capability{"dedicated_engine", "unverified", "CORE_PREPARATION_REQUIRED"}
	for _, key := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS",
		"DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_CONFIG"} {
		if os.Getenv(key) != "" {
			endpoint.Status = "unverified"
			endpoint.Code = "DOCKER_OVERRIDE_REQUIRES_ADMINISTRATOR_INSPECTION"
			return []capability{endpoint, engine}
		}
	}
	if process == nil {
		return []capability{endpoint, engine}
	}
	// Do not read user Docker contexts or connect to an environment-selected
	// endpoint. Core independently verifies ownership, daemon identity and isolation.
	for _, socket := range []string{"/run/docker.sock", "/run/nibfin-engine/docker.sock"} {
		if _, err := os.Lstat(socket); err != nil {
			continue
		}
		data, err := process.Run("/usr/bin/env", []string{"-i", "PATH=/usr/bin:/bin",
			"LANG=C", "/usr/bin/docker", "--config", "/etc/nibfin/engine/client",
			"--host", "unix://" + socket, "info", "--format", "{{json .}}"}, nil)
		var info linuxDockerInfo
		if err != nil || len(data) > 65536 || json.Unmarshal(data, &info) != nil {
			continue
		}
		if !compatibleLinuxDocker(c, arch, info) {
			endpoint.Status, endpoint.Code = "unverified", "MANAGED_DOCKER_TARGET_MISMATCH"
		} else {
			engine.Status, engine.Code = "observed", "LOCAL_ENGINE_REPORTED_ONLY"
		}
	}
	return []capability{endpoint, engine}
}

func observeDockerServices(process Process) []capability {
	var checks []capability
	for _, service := range []string{"docker.service", "nibfin-engine.service"} {
		check := capability{service, "unverified", "SERVICE_STATE_UNAVAILABLE"}
		if process != nil {
			data, err := process.Run("/usr/bin/env", []string{"-i", "PATH=/usr/bin:/bin",
				"LANG=C", "/usr/bin/systemctl", "show", service,
				"--property=LoadState,ActiveState", "--no-pager"}, nil)
			if err == nil && len(data) <= 4096 {
				for _, state := range []string{"active", "inactive", "failed", "activating"} {
					if strings.Contains("\n"+string(data), "\nActiveState="+state+"\n") {
						check.Status, check.Code = "observed", "SERVICE_STATE_"+strings.ToUpper(state)
					}
				}
			}
		}
		checks = append(checks, check)
	}
	return checks
}

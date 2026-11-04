package cli

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

func inspectDocker(process Process) []capability {
	result := []capability{{"docker_backend", "unverified", "DOCKER_INSPECTION_UNAVAILABLE"}}
	// Ambient overrides could redirect a read-only probe to an unrelated daemon.
	for _, key := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS",
		"DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_CONFIG"} {
		if os.Getenv(key) != "" {
			result[0].Code = "DOCKER_OVERRIDE_REQUIRES_ADMINISTRATOR_INSPECTION"
			return result
		}
	}
	data, err := process.Run("docker", []string{"context", "show"}, nil)
	context := strings.TrimSpace(string(data))
	if err != nil || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`).
		MatchString(context) {
		return result
	}
	data, err = process.Run("docker", []string{"context", "inspect", context}, nil)
	var contexts []struct {
		Endpoints map[string]struct{ Host string }
	}
	if err != nil || json.Unmarshal(data, &contexts) != nil || len(contexts) != 1 {
		return result
	}
	host := contexts[0].Endpoints["docker"].Host
	local := strings.HasPrefix(host, "unix:///") ||
		strings.HasPrefix(host, "npipe:////./pipe/")
	if !local || strings.ContainsAny(host, "\r\n\x00") {
		result[0].Code = "REMOTE_OR_UNKNOWN_DOCKER_ENDPOINT_NOT_PROBED"
		return result
	}
	data, err = process.Run("docker", []string{"--host", host,
		"info", "--format", "{{json .}}"}, nil)
	var info struct {
		OSType          string
		OperatingSystem string
		SecurityOptions []string
	}
	if err != nil || json.Unmarshal(data, &info) != nil {
		result[0].Code = "LOCAL_DOCKER_ENGINE_UNAVAILABLE"
		return result
	}
	if info.OSType != "linux" {
		result[0].Code = "LINUX_CONTAINER_ENGINE_NOT_OBSERVED"
		return result
	}
	result[0] = capability{"docker_backend", "observed", "LINUX_CONTAINER_ENGINE_REPORTED"}
	if strings.Contains(info.OperatingSystem, "Docker Desktop") {
		result[0].Code = "DOCKER_DESKTOP_LINUX_ENGINE_REPORTED"
	}
	for _, security := range []string{"rootless", "apparmor"} {
		check := capability{"docker_" + security, "unverified", "ENGINE_FLAG_NOT_REPORTED"}
		for _, option := range info.SecurityOptions {
			if option == "name="+security || strings.HasPrefix(option, "name="+security+",") {
				check.Status, check.Code = "observed", "ENGINE_FLAG_REPORTED_ONLY"
			}
		}
		result = append(result, check)
	}
	return result
}

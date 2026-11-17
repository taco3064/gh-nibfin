package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
)

//go:embed managed-relay-local.py
var managedRelayLocal string

type relayLocalSnapshot struct {
	Bindings json.RawMessage `json:"bindings"`
	Config   json.RawMessage `json:"config"`
}

type relayLocalRequest struct {
	Operation string              `json:"operation"`
	Expected  *relayLocalSnapshot `json:"expected,omitempty"`
	Config    *relayConfiguration `json:"config,omitempty"`
}

// Like deployment, local administration uses only the fixed system Python,
// isolated from user imports. Neither the script nor its paths come from GitHub.
func runRelayLocal(process Process, request relayLocalRequest) ([]byte, error) {
	python, err := filepath.EvalSymlinks("/usr/bin/python3")
	if err != nil || !regexp.MustCompile(`^/usr/bin/python3\.\d+$`).MatchString(python) {
		return nil, errors.New("MANAGED_RELAY_SYSTEM_PYTHON_REQUIRED")
	}
	if err := requireRootOwnedHostPath(python); err != nil {
		return nil, err
	}
	data, err := json.Marshal(request)
	if err != nil || len(data) > 262144 {
		return nil, errors.New("MANAGED_RELAY_LOCAL_INPUT_INVALID")
	}
	if err := requireRootOwnedHostPath("/usr/bin/env"); err != nil {
		return nil, err
	}
	out, err := runSharedAdministrator(process, "/usr/bin/env", []string{
		"-i", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", python,
		"-I", "-c", managedRelayLocal,
	}, data)
	if err != nil || len(out) > 262144 {
		return nil, errors.New("MANAGED_RELAY_LOCAL_READBACK_REQUIRED")
	}
	return out, nil
}

func runInstalledRelay(process Process, operation string) ([]byte, error) {
	switch operation {
	case "prepare", "inspect", "pause", "resume":
	default:
		return nil, errors.New("MANAGED_RELAY_OPERATION_INVALID")
	}
	node, _, err := trustedSharedCore()
	if err != nil {
		return nil, err
	}
	const core = "/opt/nibfin/core"
	const entry = core + "/bin/nibfin-notification-relay.mjs"
	if err := verifyPrivilegedCoreTree(core); err != nil {
		return nil, err
	}
	for _, path := range []string{entry, "/usr/bin/env"} {
		if err := requireRootOwnedHostPath(path); err != nil {
			return nil, err
		}
	}
	out, err := runSharedAdministrator(process, "/usr/bin/env", []string{
		"-i", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", node, entry, operation,
	}, nil)
	if err != nil || len(out) > 4*1024*1024 {
		return nil, errors.New("MANAGED_RELAY_CORE_READBACK_REQUIRED")
	}
	return out, nil
}

func runRelaySystemd(process Process, args ...string) error {
	for _, path := range []string{"/usr/bin/env", "/usr/bin/systemctl"} {
		if err := requireRootOwnedHostPath(path); err != nil {
			return err
		}
	}
	values := []string{"-i", "PATH=/usr/bin:/bin", "LANG=C.UTF-8",
		"/usr/bin/systemctl"}
	_, err := runSharedAdministrator(process, "/usr/bin/env", append(values, args...), nil)
	if err != nil {
		return errors.New("MANAGED_RELAY_SERVICE_READBACK_REQUIRED")
	}
	return nil
}

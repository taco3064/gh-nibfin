package cli

import (
	"encoding/json"
	"errors"
	"regexp"
)

var nativeCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

const maintenanceRoot = "/usr/local/libexec/nibfin/core-rollout"
const maintenanceBridge = maintenanceRoot + "/bridge.py"
const maintenanceCapability = "core-native-maintenance-v1"

type coreOperationDiagnostic struct {
	Status       string                  `json:"status,omitempty"`
	Installation string                  `json:"installation,omitempty"`
	Code         string                  `json:"code"`
	GitHubAPI    *nativeGitHubDiagnostic `json:"githubApi,omitempty"`
}

type coreOperationPending struct {
	CommandError
	Diagnostic coreOperationDiagnostic
}

func (e *coreOperationPending) Unwrap() error { return &e.CommandError }

func pendingCoreOperation(data []byte, fallback string) error {
	diagnostic := coreOperationDiagnostic{Code: fallback}
	var raw struct {
		Status       string          `json:"status"`
		Installation string          `json:"installation"`
		Code         string          `json:"code"`
		GitHubAPI    json.RawMessage `json:"githubApi"`
	}
	if json.Unmarshal(data, &raw) == nil {
		switch raw.Status {
		case "installed", "pending", "unsupported", "denied", "retired":
			diagnostic.Status = raw.Status
		}
		if raw.Installation == "complete" || raw.Installation == "pending" {
			diagnostic.Installation = raw.Installation
		}
		if nativeCode.MatchString(raw.Code) {
			diagnostic.Code = raw.Code
		}
		diagnostic.GitHubAPI = nativeGitHubCause(raw.GitHubAPI)
	}
	exit := 2
	if diagnostic.Status == "unsupported" || diagnostic.Status == "denied" {
		exit = 1
	}
	return &coreOperationPending{CommandError{Code: diagnostic.Code, Exit: exit}, diagnostic}
}

func maintenanceFailureOutput(name string, args []string, data []byte) ([]byte, bool) {
	direct := name == maintenanceBridge && len(args) == 0
	sudo := name == "/usr/bin/sudo" &&
		(len(args) == 2 && args[0] == "--" && args[1] == maintenanceBridge ||
			len(args) == 3 && args[0] == "-n" && args[1] == "--" &&
				args[2] == maintenanceBridge)
	if !direct && !sudo {
		return nil, false
	}
	var envelope struct {
		Status string `json:"status"`
		Code   string `json:"code"`
	}
	if json.Unmarshal(data, &envelope) != nil ||
		(envelope.Status != "unsupported" && envelope.Status != "denied") ||
		!nativeCode.MatchString(envelope.Code) {
		return nil, false
	}
	cause := coreOperationCause(pendingCoreOperation(data, envelope.Code))
	safe, err := json.Marshal(cause)
	return safe, err == nil
}

func decodeCoreSourceMaintenance(data []byte, callErr error) error {
	if len(data) == 0 && callErr != nil {
		return callErr
	}
	var result struct {
		Status       string `json:"status"`
		Installation string `json:"installation"`
	}
	if json.Unmarshal(data, &result) != nil || callErr != nil ||
		result.Status != "installed" || result.Installation != "complete" {
		return pendingCoreOperation(data, "CORE_SOURCE_MAINTENANCE_PENDING")
	}
	return nil
}

func coreOperationCause(err error) *coreOperationDiagnostic {
	var pending *coreOperationPending
	if errors.As(err, &pending) {
		return &pending.Diagnostic
	}
	return nil
}

func encodeCoreOperationFailure(encoder installResultEncoder, err error) {
	if cause := coreOperationCause(err); cause != nil {
		_ = encoder.Encode(map[string]any{"status": cause.Status,
			"code": cause.Code, "cause": cause})
	}
}

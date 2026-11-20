package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var nativeCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

const maintenanceRoot = "/usr/local/libexec/nibfin/core-rollout"
const maintenanceBridge = maintenanceRoot + "/bridge.py"
const maintenanceCapability = "core-native-maintenance-v1"
const coreActivationQueued = "UPGRADE_CORE_ACTIVATION_QUEUED"

type coreOperationDiagnostic struct {
	Status       string                  `json:"status,omitempty"`
	Installation string                  `json:"installation,omitempty"`
	Code         string                  `json:"code"`
	GitHubAPI    *nativeGitHubDiagnostic `json:"githubApi,omitempty"`
	CoreRunID    int64                   `json:"coreRunId,omitempty"`
	CoreRunURL   string                  `json:"coreRunURL,omitempty"`
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

func decodeMaintenanceForExecution(data []byte, callErr error,
	execution SharedExecution) error {
	err := decodeCoreSourceMaintenance(data, callErr)
	cause := coreOperationCause(err)
	if cause == nil || cause.Code != coreActivationQueued {
		return err
	}
	var run struct {
		Schema int    `json:"schemaVersion"`
		ID     int64  `json:"coreRunId"`
		URL    string `json:"coreRunURL"`
	}
	if cause.Status != "pending" || cause.Installation != "pending" ||
		json.Unmarshal(data, &run) != nil || run.Schema != 1 || run.ID <= 0 ||
		execution.RepositoryID <= 0 || !repoPattern.MatchString(execution.Repository) ||
		run.URL != nativeActionsRunURL(execution.Repository, run.ID) {
		return pendingCoreOperation([]byte(`{"status":"pending","installation":"pending"}`),
			"CORE_MAINTENANCE_NATIVE_RUN_UNVERIFIED")
	}
	cause.CoreRunID, cause.CoreRunURL = run.ID, run.URL
	return err
}

func nativeActionsRunURL(repository string, run int64) string {
	return fmt.Sprintf("https://github.com/%s/actions/runs/%d", repository, run)
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

package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMaintenanceReceiptScopesMissingToInitialBootstrapProbe(t *testing.T) {
	missing := []byte(`{"status":"missing"}`)
	if !missingMaintenanceBootstrap(missing, nil) {
		t.Fatal("normal empty-archive probe cannot request verified package preparation")
	}
	if _, err := decodeMaintenanceReceipt(missing, nil); err == nil {
		t.Fatal("bridge or completed bootstrap accepted missing as installed")
	}
	for _, data := range [][]byte{nil, []byte(`null`), []byte(`{"status":"pending"}`),
		[]byte(`{"status":true}`), []byte(`{"status":"missing"} garbage`)} {
		if missingMaintenanceBootstrap(data, nil) {
			t.Fatal("malformed or unknown bootstrap result authorized package work", string(data))
		}
	}
	if missingMaintenanceBootstrap(missing, errors.New("CORE_PENDING_RESULT")) {
		t.Fatal("failed observation authorized package preparation")
	}
}

func TestQueuedCoreMaintenanceOnlyShowsBoundNativeRun(t *testing.T) {
	const base = `{"schemaVersion":1,"status":"pending","installation":"pending",` +
		`"code":"UPGRADE_CORE_ACTIVATION_QUEUED","coreRunId":7,` +
		`"coreRunURL":"https://github.com/example/execution/actions/runs/7"}`
	execution := SharedExecution{"example/execution", 42}
	for _, change := range []struct{ old, new string }{
		{"github.com", "elsewhere.example"},
		{"example/execution", "different/execution"},
		{"/runs/7", "/runs/8"},
		{"/runs/7", "/runs/7?private=anything"},
		{`"coreRunId":7,`, ""},
		{`"schemaVersion":1`, `"schemaVersion":2`},
		{`"installation":"pending"`, `"installation":"complete"`},
	} {
		data := strings.Replace(base, change.old, change.new, 1)
		err := decodeMaintenanceForExecution([]byte(data), nil, execution)
		cause := coreOperationCause(err)
		if cause == nil || cause.Code != "CORE_MAINTENANCE_NATIVE_RUN_UNVERIFIED" ||
			cause.CoreRunID != 0 || cause.CoreRunURL != "" {
			t.Fatal("unverified run exposed as a trusted result", cause, err)
		}
	}
}

func TestMaintenanceEmptyReceiptPreservesCallerFailure(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded,
		errors.New("PROCESS_RESULT_UNAVAILABLE")} {
		if _, err := decodeMaintenanceReceipt(nil, cause); err != cause ||
			!errors.Is(err, cause) || coreOperationCause(err) != nil {
			t.Fatal("empty observation lost caller failure or acquired API cause", err)
		}
	}
	if _, err := decodeMaintenanceReceipt(nil, nil); err == nil {
		t.Fatal("empty successful process response became installed")
	}
	for _, data := range [][]byte{[]byte(`{"status":"installed"}`),
		[]byte(`{"status":"missing"}`)} {
		if _, err := decodeMaintenanceReceipt(data, context.Canceled); err == nil {
			t.Fatal("nonempty response overrode caller failure", string(data))
		}
	}
}

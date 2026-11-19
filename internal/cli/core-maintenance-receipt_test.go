package cli

import (
	"context"
	"errors"
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

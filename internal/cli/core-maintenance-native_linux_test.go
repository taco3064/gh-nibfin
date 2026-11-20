package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const queuedMaintenanceFixture = `{"schemaVersion":1,"status":"pending",` +
	`"installation":"pending","code":"UPGRADE_CORE_ACTIVATION_QUEUED",` +
	`"coreRunId":73,"coreRunURL":"https://github.com/example/execution/actions/runs/73"}`

func TestCoreSourceMaintenanceDefersUntilActualInstallation(t *testing.T) {
	pins := Pins()
	execution := SharedExecution{"example/execution", 42}
	target := nativeSourceTarget{1, 2}
	receipt := maintenanceReceipt{OperationTarget: &target, OperationSources: &pins}
	current := SourcePin{}
	reads, calls := 0, 0
	read := func() (SourcePin, error) { reads++; return current, nil }
	bridge := func(request any) ([]byte, error) {
		calls++
		if !reflect.DeepEqual(request, map[string]any{
			"kind": "administrator", "coreOnly": true, "target": target,
		}) {
			t.Fatal("maintenance changed the admitted native activation", request)
		}
		if calls == 1 {
			return []byte(queuedMaintenanceFixture), errors.New("fixture pending exit")
		}
		current = pins.Core
		return []byte(`{"status":"installed","installation":"complete"}`), nil
	}
	resume := func() error {
		return completeCoreSourceMaintenance(nil, pins, execution, receipt, read, bridge)
	}
	err := resume()
	cause := coreOperationCause(err)
	var command *CommandError
	if !errors.As(err, &command) || command.Exit != 2 || cause == nil ||
		cause.CoreRunID != 73 || cause.Code != coreActivationQueued ||
		cause.CoreRunURL != "https://github.com/example/execution/actions/runs/73" ||
		calls != 1 || reads != 1 {
		t.Fatal("queued native run disappeared or claimed installation", cause, err)
	}
	if err := resume(); err != nil || calls != 2 || reads != 3 {
		t.Fatal("retry did not reuse the activation and verify the installed source", err)
	}
	receipt = maintenanceReceipt{}
	if err := resume(); err != nil || calls != 2 || reads != 4 {
		t.Fatal("confirmed installed source needed old metadata or another dispatch", err)
	}
}

func TestCoreSourceMaintenanceRequiresInstalledReadback(t *testing.T) {
	pins := Pins()
	target := nativeSourceTarget{1, 2}
	receipt := maintenanceReceipt{OperationTarget: &target, OperationSources: &pins}
	read := func() (SourcePin, error) { return SourcePin{}, nil }
	bridge := func(any) ([]byte, error) {
		return []byte(`{"status":"installed","installation":"complete"}`), nil
	}
	err := completeCoreSourceMaintenance(nil, pins, SharedExecution{}, receipt, read,
		bridge)
	if err == nil || err.Error() != "CORE_SOURCE_MAINTENANCE_REQUIRES_READBACK" {
		t.Fatal("completion became success without local installation", err)
	}
}

func TestRebuildWaitsForQueuedNativeMaintenanceBeforeRetiringPool(t *testing.T) {
	driver := newRebuildDriver(t)
	ops := driver.operations()
	installed := false
	requests := 0
	ops.migrate = func(sharedHost) error {
		if !installed {
			return &nativeSourceRequired{Producers: Pins()}
		}
		return nil
	}
	ops.source = func(Producers) error {
		requests++
		data := strings.Replace(queuedMaintenanceFixture, "example/execution",
			driver.old.Repository, 1)
		return decodeMaintenanceForExecution([]byte(data), nil, driver.old.SharedExecution)
	}
	var out bytes.Buffer
	apply := func() error {
		return (App{API: driver}).applyRebuild(
			driver.api.data["repos/example/project"].(Repository),
			driver.api.data["user"].(User), "claude", []byte("fixture-secret"),
			json.NewEncoder(&out), ops)
	}
	err := apply()
	var command *CommandError
	if !errors.As(err, &command) || command.Exit != 2 || requests != 1 ||
		len(driver.calls) != 0 || len(driver.deletes) != 0 ||
		!strings.Contains(out.String(), "https://github.com/example/project/actions/runs/73") {
		t.Fatal("queued preflight lost the link or changed the serving pool", err, out.String())
	}
	installed = true
	out.Reset()
	if err := apply(); err != nil || requests != 1 || len(driver.deletes) != 2 ||
		!strings.Contains(out.String(), "MANAGED_REPLACEMENT_ACTIVE") {
		t.Fatal("rebuild did not resume from installed source after the native run", err)
	}
}

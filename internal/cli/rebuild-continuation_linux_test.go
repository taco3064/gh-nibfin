package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRebuildContinuationRetrySkipsCompletedReplacementAndStaging(t *testing.T) {
	for _, lost := range []bool{false, true} {
		d := newRebuildDriver(t)
		d.lostActivation, d.continuationPending = lost, true
		var out bytes.Buffer
		if err := applyDriver(d, &out); err == nil || d.retirement.Release != "released" {
			t.Fatal("unfinished continuation reported completion", err, out.String())
		}
		d.continuationPending = false
		ops := d.operations()
		ops.compatible = func() error {
			t.Fatal("completed replacement reverified")
			return nil
		}
		ops.plan = func(preparationRepository, *rebuildIntent) (InitPlan, error) {
			t.Fatal("completed replacement replanned")
			return InitPlan{}, nil
		}
		ops.packages = func(InitPlan, rebuildIntent) error {
			t.Fatal("removed staging credentials read again")
			return nil
		}
		ops.prepare = func(InitPlan) error { t.Fatal("replacement prepared again"); return nil }
		ops.register = func(InitPlan, rebuildIntent,
			func([]sharedRegistration) error) error {
			t.Fatal("replacement registered again")
			return nil
		}
		before := len(d.calls)
		out.Reset()
		err := (App{API: d}).applyRebuild(
			d.api.data["repos/example/project"].(Repository),
			d.api.data["user"].(User), "claude", []byte("fixture-secret"),
			json.NewEncoder(&out), ops)
		if err != nil || len(d.calls) != before+1 ||
			d.calls[before] != "complete-installed-lifecycle" ||
			!strings.Contains(out.String(), `"status":"completed"`) {
			t.Fatal("retry failed to resume only continuation", err, d.calls, out.String())
		}
	}
}

func TestRebuildMigrationMustFinishBeforeRetirement(t *testing.T) {
	d := newRebuildDriver(t)
	ops := d.operations()
	ops.migrate = func(sharedHost) error { return errors.New("NATIVE_MIGRATION_PR_PENDING") }
	var out bytes.Buffer
	err := (App{API: d}).applyRebuild(d.api.data["repos/example/project"].(Repository),
		d.api.data["user"].(User), "claude", []byte("fixture-secret"),
		json.NewEncoder(&out), ops)
	if err == nil || len(d.deletes) != 0 || len(d.calls) != 0 {
		t.Fatal("unfinished migration retired working pool", err, d.calls, d.deletes)
	}
}

func TestRebuildRootPreparationPreservesDeniedResultAndSafeCause(t *testing.T) {
	for _, status := range []string{"denied", "unsupported"} {
		d := newRebuildDriver(t)
		ops := d.operations()
		ops.maintenance = func() error {
			return pendingCoreOperation([]byte(`{"status":"`+status+`",`+
				`"code":"ROOT_DENIED","message":"private",`+
				`"githubApi":{"kind":"http","httpStatus":422,"body":"private"}}`),
				"UNEXPECTED")
		}
		var out bytes.Buffer
		err := (App{API: d}).applyRebuild(d.api.data["repos/example/project"].(Repository),
			d.api.data["user"].(User), "claude", []byte("fixture-secret"),
			json.NewEncoder(&out), ops)
		var command *CommandError
		if !errors.As(err, &command) || command.Exit != 1 ||
			!strings.Contains(out.String(), `"httpStatus":422`) ||
			!strings.Contains(out.String(), `"status":"`+status+`"`) ||
			strings.Contains(out.String(), "private") || len(d.calls) != 0 ||
			len(d.deletes) != 0 {
			t.Fatal("root denial was flattened or disclosed", err, out.String(), d.calls)
		}
	}
}

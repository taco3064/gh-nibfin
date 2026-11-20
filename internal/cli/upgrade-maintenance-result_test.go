package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestUpgradeMaintenanceUsesBoundRunAndRetainsLegacyURL(t *testing.T) {
	execution := SharedExecution{"example/execution", 42}
	const run = "https://github.com/example/execution/actions/runs/73"
	for _, tc := range []struct {
		name, input, output string
	}{
		{"adopted", run, run},
		{"legacy", "", "https://github.com/taco3064/nibfin-core/actions/runs/73"},
		{"other repository", "https://github.com/example/other/actions/runs/73", ""},
		{"other run", "https://github.com/example/execution/actions/runs/9", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			progress := managedUpgradeProgress{OperationID: handoffTestID,
				Execution: execution.Repository, ExecutionID: execution.RepositoryID,
				Handoff: "confirmed", Installation: "pending", Readiness: "pending",
				Continuation: "pending", CoreRunID: 73, CoreRunURL: tc.input,
				InstallationCause: &coreOperationDiagnostic{Code: coreActivationQueued,
					CoreRunID: 99, CoreRunURL: "unexpected diagnostic material"}}
			wire, err := json.Marshal(map[string]any{
				"schemaVersion": 1, "status": "complete", "upgrade": progress,
			})
			if err != nil {
				t.Fatal(err)
			}
			progress, err = decodeManagedUpgrade(wire, nil, execution, handoffTestID)
			if tc.output == "" {
				if err == nil || progress.CoreRunURL != "" {
					t.Fatal("unverified run was accepted", progress.CoreRunURL, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = reportUpgradeHandoff(&out, upgradeHandoffOutcome{
				Status: "pending", Upgrade: &progress,
			}, nil)
			var result upgradeHandoffOutcome
			var command *CommandError
			if outErr := json.Unmarshal(out.Bytes(), &result); outErr != nil ||
				!errors.As(err, &command) || command.Exit != 2 ||
				result.Code != coreActivationQueued || result.CoreRunURL != tc.output ||
				result.Upgrade == nil || result.Upgrade.Installation != "pending" ||
				bytes.Contains(out.Bytes(), []byte("unexpected diagnostic material")) {
				t.Fatal("native status was lost or shown as installed", err, out.String())
			}
		})
	}
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestRebuildRejectsMalformedOrUnmatchedCorePermission(t *testing.T) {
	for _, change := range []string{"id", "pool", "member", "runners", "denied",
		"malformed", "lost"} {
		t.Run(change, func(t *testing.T) {
			d := newRebuildDriver(t)
			ops := d.operations()
			ops.core = func(op string) ([]byte, error) {
				output, _ := d.core(op)
				var r rebuildRetirementResult
				_ = json.Unmarshal(output, &r)
				switch change {
				case "id":
					r.ID = "unknown"
				case "pool":
					r.Removal.RepositoryID++
				case "member":
					r.Removal.MemberID++
				case "runners":
					r.Removal.Runners[1].ID++
				case "denied":
					r.Removal.Permitted = false
				case "malformed":
					return []byte("private diagnostic"), nil
				case "lost":
					return jsonRelay(r), errors.New("PROCESS_RESULT_UNAVAILABLE")
				}
				return jsonRelay(r), nil
			}
			i := rebuildIntent{Version: 1, Old: d.old, Agent: d.binding,
				Repositories: d.repositories}
			err := retireRebuild(d, &i, ops, func() error { return nil })
			if err == nil || len(d.deletes) != 0 {
				t.Fatal("invalid permission caused removal", err, d.deletes)
			}
		})
	}
}

type rebuildReadAPI struct {
	API
	failure string
}

func (a rebuildReadAPI) Call(method, path string, input, output any) error {
	if method == "GET" && strings.Contains(path, "actions/runners") {
		if a.failure == "authorization" {
			return errors.New("PERMISSION_DENIED")
		}
		if strings.Contains(path, "?per_page=") {
			return json.Unmarshal([]byte(`{"total_count":2,"runners":[]}`), output)
		}
		return errors.New("NOT_FOUND")
	}
	return a.API.Call(method, path, input, output)
}

func TestRebuildAbsenceNeedsIndividualAndCompleteListing(t *testing.T) {
	d := newRebuildDriver(t)
	for _, mode := range []string{"authorization", "incomplete"} {
		api := rebuildReadAPI{d, mode}
		err := removeRebuildRegistration(api, d.old,
			sharedRegistration{ID: 25, Name: "old-one"})
		if err == nil || len(d.deletes) != 0 {
			t.Fatal("unproven absence accepted", mode, err)
		}
	}
	// Successful individual/list readback must also match member and labels.
	d.runners[0].Name = "unrelated"
	if err := removeRebuildRegistration(d, d.old,
		sharedRegistration{ID: 25, Name: "old-one"}); err == nil {
		t.Fatal("different native identity removed")
	}
}

func TestRebuildActivationBindsBothFreshIDsAndHistoricalRepositories(t *testing.T) {
	d := newRebuildDriver(t)
	var out bytes.Buffer
	if err := applyDriver(d, &out); err != nil {
		t.Fatal(err)
	}
	i := rebuildIntent{Old: d.old, RetirementID: fixtureRetirementID,
		Repositories: d.repositories,
		Fresh: []sharedRegistration{{ID: 31, Name: "fresh-one"},
			{ID: 32, Name: "fresh-two"}}}
	output, _ := d.core("activate-shared-replacement")
	for _, key := range []string{"retirementId", "repositoryId", "memberId", "status",
		"runners", "adoptedRepositoryIds"} {
		var receipt map[string]any
		_ = json.Unmarshal(output, &receipt)
		delete(receipt, key)
		if verifyRebuildActivation(jsonRelay(receipt), i) == nil {
			t.Fatal("missing activation identity accepted", key)
		}
	}
	i.Fresh[1].ID++
	if verifyRebuildActivation(output, i) == nil {
		t.Fatal("wrong second native ID released")
	}
}

func TestRebuildCapabilityAllowsOnlyFixedActivation(t *testing.T) {
	for _, command := range []string{"", "sh arbitrary", "activate-shared-replacement"} {
		data := jsonRelay(map[string]any{"schemaVersion": 1,
			"managedPoolReplacement": map[string]string{
				"kind": "managed-pool-replacement-v1", "activationCommand": command}})
		err := requireRebuildCapability(data, "codex")
		if (err == nil) != (command == "activate-shared-replacement") {
			t.Fatal(command, err)
		}
	}
	if _, err := (App{}).initAgentCapability("claude"); err == nil {
		t.Fatal("production init must refuse Claude before credential or host effects")
	}
}

func TestCorePendingOutputIsNarrowAndStderrStaysPrivate(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"1", "2"} {
		script := `process.stdout.write('{"status":"pending"}');
process.stderr.write('fixture-private-diagnostic'); process.exit(` + code + `);`
		output, err := runResolvedNativeProcess(context.Background(), "node", node, []string{
			"--input-type=module"}, []byte(script), true)
		if err == nil || bytes.Contains(output, []byte("private")) ||
			strings.Contains(err.Error(), "private") {
			t.Fatal("private diagnostic escaped", err)
		}
		if code == "2" && (len(output) == 0 || err.Error() != "CORE_PENDING_RESULT") ||
			code == "1" && len(output) != 0 {
			t.Fatal("non-pending failure output retained", string(output), err)
		}
	}
}

func TestReplacementResourcesRequireCompleteNativeReceipt(t *testing.T) {
	image := "sha256:" + strings.Repeat("a", 64)
	receipt := map[string]any{"status": "prepared", "code": "MANAGED_RESOURCES_PREPARED",
		"profile": "/etc/nibfin/shared-host-profile.json", "adoption": false,
		"servicesReady": false, "images": map[string]string{"agent": image, "hooks": image}}
	if err := decodeReplacementPreparation(jsonRelay(receipt), "codex"); err != nil {
		t.Fatal(err)
	}
	for key, value := range receipt {
		delete(receipt, key)
		if decodeReplacementPreparation(jsonRelay(receipt), "codex") == nil {
			t.Fatal("incomplete preparation accepted", key)
		}
		receipt[key] = value
	}
	receipt["adoption"] = true
	if decodeReplacementPreparation(jsonRelay(receipt), "codex") == nil {
		t.Fatal("preparation cannot assert adoption or release")
	}
}

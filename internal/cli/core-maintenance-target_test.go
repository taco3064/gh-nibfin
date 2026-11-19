package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func activationPayload(t *testing.T, pins Producers) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	entry, err := writer.Create("activation.json")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(pins)
	if _, err := entry.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func nativeTargetFixture(t *testing.T) (*upgradeAPI, Producers) {
	t.Helper()
	f := upgradeFixture(t, true, true, true)
	alternative := fixtureProducers(t, f.fakeAPI, f.old.Core.Commit,
		strings.Repeat("6", 40), 103, true, true)
	base := "repos/" + officialCore
	f.data[base] = Repository{ID: 501, FullName: officialCore, DefaultBranch: "main"}
	f.data[base+"/actions/workflows/cd.yml"] = map[string]any{
		"id": 12, "path": ".github/workflows/cd.yml", "state": "active"}
	artifacts := []nativeSourceArtifact{}
	for n, pins := range []Producers{alternative, f.old} {
		id, run := int64(901+n), int64(801+n)
		item := nativeSourceArtifact{ID: id, Name: "nibfin-activate-" + pins.Core.Commit}
		item.Run.ID = run
		artifacts = append(artifacts, item)
		f.data[fmt.Sprintf("%s/actions/artifacts/%d/zip", base, id)] =
			activationPayload(t, pins)
		f.data[fmt.Sprintf("%s/actions/runs/%d", base, run)] = map[string]any{
			"id": run, "workflow_id": 12, "path": ".github/workflows/cd.yml",
			"repository":      map[string]any{"id": 501},
			"head_repository": map[string]any{"id": 501}, "head_branch": "main",
			"head_sha": pins.Core.Commit, "status": "completed",
			"conclusion": "success",
			"event":      "workflow_dispatch"}
	}
	f.data[base+"/actions/artifacts?name=nibfin-activate-"+f.old.Core.Commit+
		"&per_page=100&page=1"] = map[string]any{
		"total_count": len(artifacts), "artifacts": artifacts}
	return f, f.old
}

func TestNativeActivationSelectionVerifiesWholePairInsteadOfFirstSameCommit(
	t *testing.T) {
	f, wanted := nativeTargetFixture(t)
	target, err := findNativeSourceTarget(f, wanted)
	if err != nil || target != (nativeSourceTarget{802, 902}) || len(f.writes) != 0 {
		t.Fatal("same-commit different Action was chosen", target, err, f.writes)
	}
}

type rejectSourceObservation struct{ t *testing.T }

func (api rejectSourceObservation) Call(string, string, any, any) error {
	api.t.Fatal("already admitted operation demanded old CD/artifact observation")
	return nil
}

func TestNativeActivationSelectionReusesOriginalAdmittedOperation(t *testing.T) {
	f, wanted := nativeTargetFixture(t)
	observed := wanted
	observed.Core.ArchiveHash = ""
	receipt := maintenanceReceipt{OperationTarget: &nativeSourceTarget{700, 701},
		OperationSources: &observed}
	target, err := selectNativeOperationTarget(rejectSourceObservation{t}, wanted, receipt)
	if err != nil || target != *receipt.OperationTarget || len(f.writes) != 0 {
		t.Fatal("installed operation was replaced by a new selection", target, err)
	}
}

func TestNativeActivationReadFailureDoesNotPickWrongPair(t *testing.T) {
	f, wanted := nativeTargetFixture(t)
	f.denyRead = "repos/" + officialCore + "/actions/artifacts/902/zip"
	_, err := findNativeSourceTarget(f, wanted)
	if err == nil || err.Error() != "READ_BACK_UNAVAILABLE" || len(f.writes) != 0 {
		t.Fatal("failed target observation became a different operation", err, f.writes)
	}
}

func TestMaintenanceOptionalOperationReadbackDoesNotInvalidateInstallation(t *testing.T) {
	f, _ := nativeTargetFixture(t)
	for _, optional := range []any{nil, "malformed", map[string]any{
		"runId": "bad", "artifactId": 2}, map[string]any{"runId": 7, "artifactId": 8}} {
		data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "status": "installed",
			"capability": maintenanceCapability, "source": f.old.Core,
			"operationTarget": optional, "operationSources": "malformed"})
		result, err := decodeMaintenanceReceipt(data, nil)
		if err != nil || result.Status != "installed" || result.OperationSources != nil {
			t.Fatal("optional old bookkeeping became a global gate", result, err)
		}
	}
}

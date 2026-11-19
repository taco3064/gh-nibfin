package cli

import (
	"encoding/json"
	"errors"
)

type maintenanceReceipt struct {
	Schema           int                 `json:"schemaVersion"`
	Status           string              `json:"status"`
	Capability       string              `json:"capability"`
	Source           SourcePin           `json:"source"`
	Code             string              `json:"code"`
	OperationTarget  *nativeSourceTarget `json:"-"`
	OperationSources *Producers          `json:"-"`
}

func decodeMaintenanceReceipt(data []byte, callErr error) (maintenanceReceipt, error) {
	var receipt maintenanceReceipt
	if len(data) == 0 && callErr != nil {
		return receipt, callErr
	}
	if json.Unmarshal(data, &receipt) != nil {
		return receipt, errors.New("CORE_MAINTENANCE_READBACK_REQUIRED")
	}
	if callErr != nil || receipt.Schema != 1 || receipt.Status != "installed" ||
		receipt.Capability != maintenanceCapability ||
		receipt.Source.Repository != officialCore ||
		!commitPattern.MatchString(receipt.Source.Commit) ||
		!digestPattern.MatchString(receipt.Source.ManifestHash) {
		return receipt, pendingCoreOperation(data, "CORE_MAINTENANCE_PREPARATION_PENDING")
	}
	var optional struct {
		Target  json.RawMessage `json:"operationTarget"`
		Sources json.RawMessage `json:"operationSources"`
	}
	_ = json.Unmarshal(data, &optional)
	var target nativeSourceTarget
	if json.Unmarshal(optional.Target, &target) == nil && target.RunID > 0 &&
		target.ArtifactID > 0 {
		receipt.OperationTarget = &target
	}
	var sources Producers
	if json.Unmarshal(optional.Sources, &sources) == nil &&
		nativeOperationSourcesMatch(sources, sources) {
		receipt.OperationSources = &sources
	}
	return receipt, nil
}

func missingMaintenanceBootstrap(data []byte, callErr error) bool {
	var probe struct {
		Status string `json:"status"`
	}
	return callErr == nil && json.Unmarshal(data, &probe) == nil &&
		probe.Status == "missing"
}

func nativeOperationSourcesMatch(left, right Producers) bool {
	return nativeCoreSourcesMatch(left.Core, right.Core) &&
		left.Action.Repository == officialAction &&
		commitPattern.MatchString(left.Action.Commit) &&
		left.Action.Repository == right.Action.Repository &&
		left.Action.Commit == right.Action.Commit
}

func nativeCoreSourcesMatch(left, right SourcePin) bool {
	return left.Repository == officialCore && commitPattern.MatchString(left.Commit) &&
		digestPattern.MatchString(left.ManifestHash) &&
		left.Repository == right.Repository && left.Commit == right.Commit &&
		left.ManifestHash == right.ManifestHash
}

func maintenanceOperationMatches(receipt maintenanceReceipt, pins Producers) bool {
	return receipt.OperationTarget != nil && receipt.OperationSources != nil &&
		receipt.OperationTarget.RunID > 0 && receipt.OperationTarget.ArtifactID > 0 &&
		nativeOperationSourcesMatch(*receipt.OperationSources, pins)
}

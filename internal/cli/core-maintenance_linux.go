package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

//go:embed core-maintenance-bootstrap.py
var maintenanceBootstrap string

func (a App) readPreparedMaintenance() (maintenanceReceipt, error) {
	if err := trustedMaintenanceBridge(); err != nil {
		return maintenanceReceipt{}, err
	}
	output, callErr := a.runMaintenanceBridge(map[string]any{"kind": "inspect"})
	return decodeMaintenanceReceipt(output, callErr)
}

// This runs outside CLI host admission. Normal admission is acquired only after
// the root preparation result and actual host readback.
func (a App) prepareCoreMaintenance(pins Producers) error {
	host, err := discoverSharedHost(a.Process)
	if err != nil || host == nil {
		return err
	}
	repo, user, err := repository(a.API, host.Repository, true)
	if err != nil {
		return err
	}
	if repo.ID != host.RepositoryID || user.ID != host.MemberID {
		return errors.New("CORE_MAINTENANCE_MEMBER_CHANGED")
	}
	if _, err := verifySharedHost(a.API, *host); err != nil {
		return err
	}
	if _, err := os.Lstat(maintenanceBridge); err == nil {
		if err := trustedMaintenanceBridge(); err != nil {
			return err
		}
		return (maintenanceBridgeClient{a.Process, trustedMaintenanceBridge}).prepare()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Reuse a complete currently installed bootstrap before consulting this CLI's
	// older package metadata. The root loader independently verifies that source.
	output, callErr := runSharedDeployment(rebuildResultProcess(a.Process), nil,
		"bootstrap", "{}")
	if !missingMaintenanceBootstrap(output, callErr) {
		_, err := decodeMaintenanceReceipt(output, callErr)
		return err
	}
	if err := verifyProducers(a.API, pins); err != nil {
		return err
	}
	selection, err := sharedInstallationForProducers(a.API, pins)
	if err != nil {
		return err
	}
	files, err := readCoreArchive(selection.archive)
	if err != nil {
		return err
	}
	if err := verifyCoreArchiveManifest(files, pins.Core); err != nil {
		return err
	}
	var contract struct {
		Schema int    `json:"schemaVersion"`
		Kind   string `json:"kind"`
	}
	if json.Unmarshal(files["src/core-maintenance-installation.json"].data,
		&contract) != nil || contract.Schema != 1 || contract.Kind != maintenanceCapability {
		return errors.New("CORE_MAINTENANCE_BOOTSTRAP_PACKAGE_REQUIRED")
	}
	if _, err := prepareSelectedSharedNode(a.Process, selection.target); err != nil {
		return err
	}
	pin, _ := json.Marshal(pins.Core)
	output, callErr = runSharedDeployment(rebuildResultProcess(a.Process),
		selection.archive, "bootstrap", string(pin))
	_, err = decodeMaintenanceReceipt(output, callErr)
	return err
}

func (a App) runMaintenanceBridge(request any) ([]byte, error) {
	return (maintenanceBridgeClient{a.Process, trustedMaintenanceBridge}).call(request)
}

type maintenanceBridgeClient struct {
	process Process
	verify  func() error
}

func (client maintenanceBridgeClient) call(request any) ([]byte, error) {
	if err := client.verify(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return runSharedAdministrator(rebuildResultProcess(client.process), maintenanceBridge,
		nil, data)
}

func (client maintenanceBridgeClient) prepare() error {
	output, callErr := client.call(map[string]any{"kind": "inspect"})
	if _, err := decodeMaintenanceReceipt(output, callErr); err == nil {
		return nil
	}
	output, callErr = client.call(map[string]any{"kind": "prepare"})
	_, err := decodeMaintenanceReceipt(output, callErr)
	return err
}

func trustedMaintenanceBridge() error {
	return trustedMaintenanceExecutable(maintenanceBridge)
}

func trustedMaintenanceExecutable(path string) error {
	if err := requireRootOwnedHostPath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("CORE_MAINTENANCE_FIXED_EXECUTABLE_REQUIRED")
	}
	return nil
}

func sharedDeploymentSource(values []string) string {
	if len(values) > 0 && values[0] == "bootstrap" {
		return strings.Replace(sharedCoreDeploy, "# CORE_MAINTENANCE_EXTENSION",
			maintenanceBootstrap, 1)
	}
	return sharedCoreDeploy
}

func (a App) maintainNativeCoreSource(pins Producers, execution SharedExecution) error {
	if err := a.prepareCoreMaintenance(pins); err != nil {
		return err
	}
	receipt, err := a.readPreparedMaintenance()
	if err != nil {
		return err
	}
	return completeCoreSourceMaintenance(a.API, pins, execution, receipt, installedSharedPin,
		a.runMaintenanceBridge)
}

func completeCoreSourceMaintenance(api API, pins Producers, execution SharedExecution,
	receipt maintenanceReceipt, readInstalled func() (SourcePin, error),
	bridge func(any) ([]byte, error)) error {
	current, err := readInstalled()
	if err != nil {
		return err
	}
	if nativeCoreSourcesMatch(current, pins.Core) &&
		!maintenanceOperationMatches(receipt, pins) {
		return nil
	}
	// An admitted matching operation resumes its finish after apply without
	// demanding another archive/CD proof; other targets use native readback.
	target, err := selectNativeOperationTarget(api, pins, receipt)
	if err != nil {
		return err
	}
	output, callErr := bridge(map[string]any{
		"kind": "administrator", "coreOnly": true, "target": target})
	if err := decodeMaintenanceForExecution(output, callErr, execution); err != nil {
		return err
	}
	current, err = readInstalled()
	if err != nil || !nativeCoreSourcesMatch(current, pins.Core) {
		return errors.New("CORE_SOURCE_MAINTENANCE_REQUIRES_READBACK")
	}
	return nil
}

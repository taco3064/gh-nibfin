package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
)

func (a App) nativeMigrationOperations() nativeMigrationOperations {
	return nativeMigrationOperations{
		target: a.selectManagedNativeSource,
		bindings: func() (relayAdoptions, error) {
			var result relayLocalSnapshot
			var bindings relayAdoptions
			data, err := runRelayLocal(a.Process, relayLocalRequest{Operation: "bindings"})
			if err != nil || json.Unmarshal(data, &result) != nil ||
				json.Unmarshal(result.Bindings, &bindings) != nil {
				return bindings, errors.New("NATIVE_MIGRATION_BINDINGS_REQUIRES_READBACK")
			}
			return bindings, nil
		},
		installed: func(plan InitPlan, pins Producers) (
			*sharedHost, []sharedRegistration, error) {
			return readNativeInstallationWithPins(a.API, plan,
				func() (*sharedHost, error) { return discoverSharedHost(a.Process) }, pins)
		},
		state:   nativeMigrationState,
		settled: removeNativeMigrationState,
		refresh: a.refreshManagedCaller,
		retire: func() ([]byte, error) {
			return runInstalledSharedAdmin(rebuildResultProcess(a.Process), nil,
				"retire-notification-relay")
		},
		complete: func() ([]byte, error) {
			return runInstalledSharedAdmin(rebuildResultProcess(a.Process), nil,
				"complete-installed-lifecycle")
		},
	}
}

// The caller still holds normal per-install admission. Keep that lock inode and
// the original install intent; only the completed migration's progress expires.
func removeNativeMigrationState(plan InitPlan) error {
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		return err
	}
	path += ".native-migration"
	file, err := privateInstallFile(path, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}

func nativeMigrationState(plan InitPlan) (*nativeWakeSetup, func() error,
	func(), error) {
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		return nil, nil, nil, err
	}
	// Share normal per-install admission. Preserve the original install intent;
	// this nonsecret record belongs to its later routing migration.
	unlock, err := installIntentLock(path)
	if err != nil {
		return nil, nil, nil, err
	}
	path += ".native-migration"
	intent, exists, err := readManagedIntent(path)
	if err != nil {
		unlock()
		return nil, nil, nil, err
	}
	if exists && (intent.Plan.Repository.ID != plan.Repository.ID ||
		intent.Plan.Repository.FullName != plan.Repository.FullName ||
		intent.Plan.Member.ID != plan.Member.ID ||
		!sameSharedExecution(intent.Plan.Execution, plan.Execution)) {
		unlock()
		return nil, nil, nil, errors.New("NATIVE_MIGRATION_IDENTITY_CHANGED")
	}
	intent.Plan = plan
	if intent.Native == nil {
		intent.Native = &nativeWakeSetup{}
	}
	return intent.Native, func() error { return saveManagedIntent(path, intent) },
		unlock, nil
}

func (a App) refreshManagedCaller(plan InitPlan, host sharedHost) error {
	output, err := runInstalledSharedAdmin(a.Process, nil,
		"refresh-managed-issue-caller", plan.Repository.FullName,
		strconv.FormatInt(plan.Repository.ID, 10))
	var receipt struct {
		Status       string          `json:"status"`
		RepositoryID int64           `json:"repositoryId"`
		Runners      json.RawMessage `json:"runners"`
	}
	pool, _ := json.Marshal(host.Runners)
	if err != nil || json.Unmarshal(output, &receipt) != nil ||
		(receipt.Status != "adopted" && receipt.Status != "unchanged") ||
		receipt.RepositoryID != plan.Repository.ID ||
		!equalRelayJSON(pool, receipt.Runners) {
		return errors.New("MANAGED_CALLER_REFRESH_REQUIRES_READBACK")
	}
	return recheckSharedHost(a.Process, a.API, host)
}

func (a App) applyNativeUpgrade(host sharedHost, version string, out io.Writer) error {
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	var user User
	if err := a.API.Call("GET", "user", nil, &user); err != nil {
		return err
	}
	if user.ID != host.MemberID {
		return errors.New("NATIVE_MIGRATION_IDENTITY_CHANGED")
	}
	api, err := selectedLogin(ctx, user)
	if err != nil {
		return err
	}
	defer clear(api.token)
	a.API = api
	a.Process = onboardingProcess{ctx: ctx, process: NativeProcess{}}
	return a.applyUpgradeHandoff(host, version, out)
}

func (a App) continueNativeUpgrade(host sharedHost, version string, out io.Writer) error {
	finishCleanup := beginCacheCleanup(out, "", reconcileCLIInvocation)
	defer finishCleanup()
	var selected upgradeSource
	var migration nativeMigrationOperations
	results, continuation, callErr := nativeUpgradeLifecycle(nativeUpgradeOperations{
		prepare: func() error {
			var err error
			selected, err = a.selectManagedNativeSource(host, version)
			if err != nil {
				return err
			}
			if err := a.prepareCoreMaintenance(selected.Producers); err != nil {
				return err
			}
			migration, err = a.preparedNativeMigrationOperations(selected)
			return err
		},
		lock: sharedMutationLock,
		host: func() (sharedHost, error) {
			if err := requireNoReplacement(); err != nil {
				return sharedHost{}, err
			}
			return a.readNativeUpgradeHost(host)
		},
		migrate: func(current sharedHost) ([]nativeMigrationResult,
			*nativeContinuation, error) {
			return migrateNativeConsumers(a.API, current, version,
				migration, true)
		},
		maintain: func(pins Producers) error {
			if err := a.maintainNativeCoreSource(pins); err != nil {
				return err
			}
			var err error
			migration, err = a.preparedNativeMigrationOperations(selected)
			return err
		},
	})
	status := "completed"
	if callErr != nil {
		status = "pending"
	}
	if err := json.NewEncoder(out).Encode(map[string]any{"status": status,
		"migration": results, "continuation": continuation,
		"cause": coreOperationCause(callErr)}); err != nil {
		return &CommandError{Code: "OUTPUT_UNAVAILABLE_AFTER_UPGRADE", Exit: 2}
	}
	if callErr != nil {
		var command *CommandError
		if errors.As(callErr, &command) {
			return callErr
		}
		return &CommandError{Code: callErr.Error(), Exit: 2}
	}
	return nil
}

func (a App) preparedNativeMigrationOperations(target upgradeSource) (
	nativeMigrationOperations, error) {
	ops := a.nativeMigrationOperations()
	ops.selected = &target
	receipt, err := a.readPreparedMaintenance()
	if err != nil {
		return ops, err
	}
	ops.sourcePending = maintenanceOperationMatches(receipt, target.Producers)
	return ops, nil
}

func (a App) readNativeUpgradeHost(previous sharedHost) (sharedHost, error) {
	current, err := discoverSharedHost(a.Process)
	if err != nil || current == nil {
		return sharedHost{}, errors.New("NATIVE_MIGRATION_HOST_REQUIRES_READBACK")
	}
	if current.MemberID != previous.MemberID ||
		current.SharedExecution != previous.SharedExecution {
		return sharedHost{}, errors.New("NATIVE_MIGRATION_IDENTITY_CHANGED")
	}
	if _, err := verifySharedHost(a.API, *current); err != nil {
		return sharedHost{}, err
	}
	adopted, err := a.nativeMigrationOperations().bindings()
	if err != nil {
		return sharedHost{}, err
	}
	if err := nativeMigrationBindings(adopted, *current); err != nil {
		return sharedHost{}, err
	}
	return *current, nil
}

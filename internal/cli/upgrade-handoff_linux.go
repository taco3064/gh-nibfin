package cli

import (
	"errors"
	"io"
)

func (a App) applyUpgradeHandoff(previous sharedHost, name, version string,
	out io.Writer) error {
	finish := beginCacheCleanup(out, "", reconcileCLIInvocation)
	defer finish()
	target, err := a.selectManagedNativeSource(previous, version)
	if err != nil {
		return reportUpgradeHandoff(out, pendingUpgradeHandoff(), err)
	}
	selection, err := a.prepareUpgradeHandoff(previous, name, version, target)
	if err != nil {
		return reportUpgradeHandoff(out, pendingUpgradeHandoff(), err)
	}
	ports := upgradeHandoffPorts{
		bridge: a.runMaintenanceBridge,
		publish: func(plan upgradePlan) (InitResult, error) {
			return a.publishUpgradeHandoff(previous, version, plan, selection.recipe)
		},
		ready: func(plan upgradePlan, proposal managedUpgradeProposal) error {
			unlock, err := sharedMutationLock()
			if err != nil {
				return err
			}
			defer unlock()
			if err := requireNoReplacement(); err != nil {
				return err
			}
			if _, err := a.readNativeUpgradeHost(previous); err != nil {
				return err
			}
			return readyBoundUpgradeProposal(a.API, plan, version, proposal)
		},
		recover: func(plan upgradePlan, proposal managedUpgradeProposal, retry bool) (
			upgradeRecoveryObservation, error) {
			unlock, err := sharedMutationLock()
			if err != nil {
				return upgradeRecoveryObservation{}, err
			}
			defer unlock()
			if err := requireNoReplacement(); err != nil {
				return upgradeRecoveryObservation{}, err
			}
			if _, err := a.readNativeUpgradeHost(previous); err != nil {
				return upgradeRecoveryObservation{}, err
			}
			return recoverMergedUpgrade(a.API, plan, proposal, selection.recipe, retry)
		},
	}
	result, err := runUpgradeHandoff(selection.plans, selection.executionReady,
		selection.recipe, target.Producers.Core, selection.activation, ports)
	return reportUpgradeHandoff(out, result, err)
}

type upgradeHandoffSelection struct {
	recipe         upgradeHandoffRecipe
	plans          []upgradePlan
	executionReady bool
	activation     *nativeSourceTarget
}

func pendingUpgradeHandoff() upgradeHandoffOutcome {
	return upgradeHandoffOutcome{Status: "pending", Migration: []nativeMigrationResult{}}
}

func (a App) prepareUpgradeHandoff(host sharedHost, name, version string,
	target upgradeSource) (upgradeHandoffSelection, error) {
	var choice upgradeHandoffSelection
	if err := requireNoReplacement(); err != nil {
		return choice, err
	}
	recipe, err := readUpgradeHandoffRecipe(a.API, target.Producers.Core,
		host.SharedExecution)
	if err != nil {
		return choice, err
	}
	if recipe == nil {
		return choice, errors.New("CORE_UPGRADE_RECEIVER_REQUIRED")
	}
	choice.recipe, target.handoff = *recipe, recipe
	adopted, err := a.nativeMigrationOperations().bindings()
	if err != nil {
		return choice, err
	}
	choice.plans, choice.executionReady, err = upgradeHandoffPlans(a.API, host,
		name, version, target, adopted)
	if err != nil {
		return choice, err
	}
	choice.recipe = *choice.plans[0].Target.handoff
	// The existing administrator bridge owns host admission. Never call it
	// while the CLI holds the same host directory's mutation lease.
	if err := a.prepareCoreMaintenance(target.Producers); err != nil {
		return choice, err
	}
	receipt, err := a.readPreparedMaintenance()
	if err != nil {
		return choice, err
	}
	installedRecipe, err := readUpgradeHandoffRecipe(a.API, receipt.Source,
		host.SharedExecution)
	if err != nil {
		return choice, err
	}
	if installedRecipe == nil {
		return choice, errors.New("CORE_UPGRADE_RECEIVER_REQUIRED")
	}
	if !nativeCoreSourcesMatch(adopted.Pin, target.Producers.Core) {
		activation, err := selectNativeOperationTarget(a.API, target.Producers, receipt)
		if err != nil {
			return choice, err
		}
		choice.activation = &activation
	}
	return choice, nil
}

func (a App) previewUpgradeHandoff(host sharedHost, name, version string,
	target upgradeSource) ([]upgradePlan, bool, error) {
	adopted, err := a.nativeMigrationOperations().bindings()
	if err != nil {
		return nil, false, err
	}
	return upgradeHandoffPlans(a.API, host, name, version, target, adopted)
}

func (a App) publishUpgradeHandoff(previous sharedHost, version string,
	plan upgradePlan, recipe upgradeHandoffRecipe) (
	InitResult, error) {
	var result InitResult
	unlock, err := sharedMutationLock()
	if err != nil {
		return result, err
	}
	defer unlock()
	if err := requireNoReplacement(); err != nil {
		return result, err
	}
	host, err := a.readNativeUpgradeHost(previous)
	if err != nil {
		return result, err
	}
	if err := nativeTargetStillCurrent(a.API, plan); err != nil {
		return result, err
	}
	installation, err := nativeMigrationPlan(a.API, host, plan)
	if err != nil {
		return result, err
	}
	ops := a.nativeMigrationOperations()
	ops.selected = &plan.Target
	state, save, release, err := ops.state(installation)
	if err != nil {
		return result, err
	}
	defer release()
	if state == nil || save == nil {
		return result, errors.New("NATIVE_MIGRATION_STATE_UNAVAILABLE")
	}
	if err := prepareUpgradeRoute(a.API, installation, plan, version, recipe,
		&state.Upgrade, save); err != nil {
		return result, err
	}
	if !plan.issueOnly {
		if err := prepareNativeConsumerWake(a.API, host, version, ops, installation,
			plan, state, save); err != nil {
			return result, err
		}
	}
	return applyUpgrade(a.API, plan, version)
}

package cli

import (
	"encoding/json"
	"errors"
	"io"
)

func (a App) runUpgrade(args []string, out io.Writer) error {
	o, err := parseUpgradeOptions(args, false)
	if err != nil {
		return err
	}
	name, version, apply := &o.Repo, &o.Version, &o.Apply
	ops := a.upgradeCommandOperations()
	host, err := ops.discover()
	if err != nil {
		return err
	}
	if host != nil {
		if err := ops.verify(*host); err != nil {
			return err
		}
	}
	if err := a.checkUpgradeExpectation(o, host); err != nil {
		return err
	}
	repo, _, err := repository(a.API, *name, false)
	if err != nil {
		return err
	}
	installed := host != nil && host.adoptionStatus(repo.ID) == "observed"
	if o.Installation != "" && !installed {
		return errors.New("REQUESTED_REPOSITORY_NOT_ADOPTED")
	}
	nativeLifecycle := installed && *version == "latest"
	var plan upgradePlan
	if nativeLifecycle {
		var target upgradeSource
		target, err = ops.selectSource(*host, *version)
		if err == nil {
			plan, err = planNativeUpgradeTarget(a.API, *name, *version, target)
		}
	} else {
		plan, err = planNativeUpgrade(a.API, *name, *version)
	}
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	status := "preview"
	if len(plan.Changes) == 0 {
		status = "no_change"
	}
	if err := encoder.Encode(map[string]any{"status": status, "plan": plan}); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if !*apply {
		return nil
	}
	if err := a.checkUpgradeExpectation(o, host); err != nil {
		return err
	}
	if nativeLifecycle {
		return ops.applyNative(*host, *version, out)
	}
	if host != nil {
		if err := ops.recheck(*host); err != nil {
			return err
		}
	}
	result, err := applyUpgrade(a.API, plan, *version)
	if err != nil {
		result.Code = err.Error()
		_ = encoder.Encode(result)
		return &CommandError{Code: "UPGRADE_PENDING_INSPECT_READ_BACK_BEFORE_RETRY", Exit: 2}
	}
	if err := encoder.Encode(result); err != nil {
		return &CommandError{Code: "OUTPUT_UNAVAILABLE_AFTER_UPGRADE", Exit: 2}
	}
	return nil
}

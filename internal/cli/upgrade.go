package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
)

func (a App) runUpgrade(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "Initialized public OWNER/REPO")
	version := flags.String("version", "latest", "Official vMAJOR.MINOR.PATCH or latest")
	managed := flags.Bool("managed-issue", false,
		"Propose the managed Issue caller for a verified inactive installation")
	apply := flags.Bool("apply", false, "Propose an upgrade through a protected PR")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *name == "" ||
		*version != "latest" && !versionPattern.MatchString(*version) {
		return errors.New("INVALID_UPGRADE_ARGUMENTS")
	}
	if *managed {
		// Retain historical plan decoding without publishing a withdrawn route.
		return errors.New("MANAGED_ARTIFACT_ROUTE_RETIRED_USE_NATIVE_UPGRADE")
	}
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
	repo, _, err := repository(a.API, *name, false)
	if err != nil {
		return err
	}
	installed := host != nil && host.adoptionStatus(repo.ID) == "observed"
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

package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type upgradeDesktopTarget struct {
	target   desktopTarget
	entry    string
	observed upgradeGuestObservation
	gap      string
}

func decodeUpgradePool(value string) (*sharedHost, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return decodeSharedHost(data)
}

func observeUpgradeTarget(p Process, target desktopTarget, repo string) (
	upgradeDesktopTarget, error,
) {
	result := upgradeDesktopTarget{target: target}
	data, err := guestQuery(p, target, "/usr/bin/python3", "-I", "-B", "-c",
		overviewBootstrap)
	var entry struct {
		Entry string `json:"entry"`
	}
	if err != nil || json.Unmarshal(data, &entry) != nil ||
		!validOverviewEntry(entry.Entry) {
		return result, errors.New("INSTALLED_GUEST_CLI_UNAVAILABLE")
	}
	result.entry = entry.Entry
	data, err = guestQuery(p, target, "/usr/bin/python3", "-I", "-B", "-c",
		upgradeProbeScript, entry.Entry, upgradeObserveCommand, repo)
	if err != nil {
		return result, errors.New("GUEST_UPGRADE_CAPABILITY_OR_OBSERVATION_UNAVAILABLE")
	}
	var denial struct {
		Gap string `json:"gap"`
	}
	if json.Unmarshal(data, &denial) == nil && denial.Gap != "" {
		switch denial.Gap {
		case "INSTALLED_CLI_UPGRADE_TRANSPORT_UNSUPPORTED",
			"GUEST_UPGRADE_OBSERVATION_UNAVAILABLE", "INSTALLED_GUEST_CLI_UNAVAILABLE":
			return result, errors.New(denial.Gap)
		default:
			return result, errors.New("GUEST_UPGRADE_OBSERVATION_UNAVAILABLE")
		}
	}
	var o upgradeGuestObservation
	if !boundedDesktopJSON(data) || json.Unmarshal(data, &o) != nil ||
		!strings.EqualFold(o.Repository, repo) || !machineIdentity.MatchString(o.Identity) ||
		(o.Architecture != "amd64" && o.Architecture != "arm64") {
		return result, errors.New("GUEST_UPGRADE_OBSERVATION_UNAVAILABLE")
	}
	result.observed = o
	if o.Adoption == "required" {
		return result, nil
	}
	if o.Adoption != "observed" || o.RepositoryID <= 0 {
		return result, errors.New("REQUESTED_REPOSITORY_ADOPTION_UNVERIFIED")
	}
	if _, err := decodeUpgradePool(o.Installation); err != nil {
		return result, errors.New("GUEST_INSTALLATION_UNAVAILABLE")
	}
	return result, nil
}

func discoverUpgradeTargets(p Process, repo string) ([]upgradeDesktopTarget, error) {
	inventory, err := wslInventory(p)
	if err != nil {
		return nil, err
	}
	var targets []upgradeDesktopTarget
	for _, target := range inventory {
		if target.Version != 2 {
			continue
		}
		item := upgradeDesktopTarget{target: target, gap: "STOPPED_GUEST_NOT_INSPECTED"}
		if target.Running {
			item, err = observeUpgradeTarget(p, target, repo)
			if err != nil {
				item.gap = err.Error()
			}
		}
		targets = append(targets, item)
	}
	return targets, nil
}

func eligibleUpgradeTarget(t upgradeDesktopTarget) bool {
	return t.target.Running && t.gap == "" && t.observed.Adoption == "observed"
}

func selectUpgradeTarget(targets []upgradeDesktopTarget, explicit string,
	preference upgradePreference) (upgradeDesktopTarget, error) {
	invalid := errors.New("NO_ADOPTED_WSL_INSTALLATION_USE_INIT")
	var eligible []upgradeDesktopTarget
	for _, t := range targets {
		key := "wsl:" + t.target.Name
		if explicit == key {
			return t, nil
		}
		if explicit == "" && preference.Target == key && !t.target.Running {
			return t, nil
		}
		if explicit == "" && preference.Target == key && t.target.Running && t.gap == "" {
			if preference.Observation.Installation != "" &&
				!sameUpgradeObservation(preference.Observation, t.observed) {
				return upgradeDesktopTarget{}, errors.New(
					"REMEMBERED_INSTALLATION_CHANGED_SELECT_TARGET")
			}
			if eligibleUpgradeTarget(t) {
				return t, nil
			}
		}
		if eligibleUpgradeTarget(t) {
			eligible = append(eligible, t)
		}
	}
	if explicit != "" {
		return upgradeDesktopTarget{}, errors.New("SELECTED_TARGET_NOT_FOUND")
	}
	if len(eligible) == 1 {
		return eligible[0], nil
	}
	if len(eligible) > 1 {
		return upgradeDesktopTarget{}, errors.New("SELECT_TARGET_WITH_TARGET_OPTION")
	}
	for _, t := range targets {
		if !t.target.Running {
			invalid = errors.New("SELECT_STOPPED_TARGET_AND_CONSENT_OPEN_TARGET")
		} else if t.gap != "" {
			invalid = errors.New(t.gap)
		}
	}
	return upgradeDesktopTarget{}, invalid
}

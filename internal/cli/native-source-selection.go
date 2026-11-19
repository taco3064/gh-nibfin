package cli

import (
	"encoding/json"
	"errors"
	"net/url"
)

func (source upgradeSource) MarshalJSON() ([]byte, error) {
	if source.Kind == "installed-current" {
		return json.Marshal(struct {
			Kind      string           `json:"kind"`
			Execution *SharedExecution `json:"execution"`
			Producers Producers        `json:"producers"`
		}{source.Kind, source.Execution, source.Producers})
	}
	// Preserve all historical release encodings, including zero-valued fixtures
	// and legacy plan IDs. The current variant never masquerades as a release.
	type releaseSource upgradeSource
	return json.Marshal(releaseSource(source))
}

func maintainedUpgradeRecipes(api API, pins Producers) (map[string][]string, error) {
	if pins.Core.Repository != officialCore || pins.Action.Repository != officialAction {
		return nil, errors.New("OFFICIAL_PRODUCERS_REQUIRED")
	}
	if err := verifyProducers(api, pins); err != nil {
		return nil, err
	}
	return callerRecipes(api, pins.Core)
}

// A native source pair is indivisible. Native ancestry establishes direction;
// neither source is assembled from a different release or installed pair.
func chooseNativeSource(api API, current upgradeSource,
	release *upgradeSource) (upgradeSource, error) {
	if release == nil {
		return current, nil
	}
	core, err := nativeSourceDirection(api, current.Producers.Core, release.Producers.Core)
	if err != nil {
		return upgradeSource{}, err
	}
	action, err := nativeSourceDirection(api, current.Producers.Action,
		release.Producers.Action)
	if err != nil {
		return upgradeSource{}, err
	}
	if core >= 0 && action >= 0 {
		return *release, nil
	}
	if core <= 0 && action <= 0 {
		return current, nil
	}
	return upgradeSource{}, errors.New("NATIVE_MIGRATION_SOURCE_DIRECTIONS_CONFLICT")
}

// Positive means the complete candidate is newer, negative means current is
// newer. A genuinely diverged source has no automatic migration direction.
func nativeSourceDirection(api API, current, candidate SourcePin) (int, error) {
	if current.Repository != candidate.Repository ||
		!commitPattern.MatchString(current.Commit) ||
		!commitPattern.MatchString(candidate.Commit) {
		return 0, errors.New("NATIVE_MIGRATION_SOURCE_CONFLICT")
	}
	if current.Commit == candidate.Commit {
		if current.ManifestHash != candidate.ManifestHash {
			return 0, errors.New("NATIVE_MIGRATION_SOURCE_INTEGRITY_CHANGED")
		}
		return 0, nil
	}
	var comparison struct {
		Status string `json:"status"`
		Ahead  *int   `json:"ahead_by"`
		Behind *int   `json:"behind_by"`
	}
	path := "repos/" + current.Repository + "/compare/" + url.PathEscape(
		current.Commit+"..."+candidate.Commit)
	if err := api.Call("GET", path, nil, &comparison); err != nil {
		return 0, err
	}
	if comparison.Ahead == nil || comparison.Behind == nil {
		return 0, errors.New("NATIVE_MIGRATION_SOURCE_ANCESTRY_UNAVAILABLE")
	}
	if comparison.Status == "ahead" && *comparison.Ahead > 0 && *comparison.Behind == 0 {
		return 1, nil
	}
	if comparison.Status == "behind" && *comparison.Behind > 0 && *comparison.Ahead == 0 {
		return -1, nil
	}
	return 0, errors.New("NATIVE_MIGRATION_SOURCE_ANCESTRY_CONFLICT")
}

func nativeTargetStillCurrent(api API, plan upgradePlan) error {
	if plan.Target.Kind != "installed-current" && !plan.Target.nativeLatest {
		return nil
	}
	core, err := nativeSourceDirection(api, plan.Current.Core, plan.Target.Producers.Core)
	if err != nil {
		return err
	}
	action, err := nativeSourceDirection(api, plan.Current.Action,
		plan.Target.Producers.Action)
	if err != nil {
		return err
	}
	if core < 0 || action < 0 {
		return errors.New("NATIVE_MIGRATION_SOURCE_ADVANCED_READ_CURRENT_TARGET")
	}
	return nil
}

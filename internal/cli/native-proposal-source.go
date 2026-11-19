package cli

import (
	"encoding/json"
	"errors"
)

// Retain the proposal's actual source metadata; an equivalent current-source
// authorization does not rewrite that PR's release identity or original plan ID.
func retainedNativeProposalSource(api API, selected upgradeSource,
	metadata string) (upgradeSource, error) {
	var original upgradeSource
	conflict := errors.New("UPGRADE_BRANCH_CONFLICT_SOURCE_INTENT_CHANGED")
	if selected.Kind != "installed-current" && !selected.nativeLatest ||
		json.Unmarshal([]byte(metadata),
			&original) != nil || !sameProducerSources(original.Producers, selected.Producers) {
		return original, conflict
	}
	if original.Kind == "installed-current" {
		execution := selected.Execution
		if execution == nil {
			execution = selected.nativeExecution
		}
		if !sameSharedExecution(original.Execution, execution) {
			return original, conflict
		}
	} else {
		if original.Kind != "" || !versionPattern.MatchString(original.Version) ||
			original.CLI.Repository != officialCLI ||
			!commitPattern.MatchString(original.CLI.Commit) {
			return original, conflict
		}
		data, _, err := content(api, officialCLI, "internal/cli/producers.json",
			original.CLI.Commit)
		if err != nil {
			return original, err
		}
		var published Producers
		if decodeUpgradePins(data, &published) != nil ||
			!sameProducerSources(published, original.Producers) {
			return original, conflict
		}
	}
	var err error
	original.recipes, err = maintainedUpgradeRecipes(api, original.Producers)
	original.maintained = true
	return original, err
}

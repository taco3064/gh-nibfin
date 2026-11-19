package cli

import "errors"

func (a App) selectManagedNativeSource(host sharedHost,
	version string) (upgradeSource, error) {
	if version != "latest" {
		source, err := resolveUpgrade(a.API, version)
		source.maintained = true
		return source, err
	}
	actual, err := a.readNativeUpgradeHost(host)
	if err != nil {
		return upgradeSource{}, err
	}
	if _, err := trustedSharedTarget(); err != nil {
		return upgradeSource{}, err
	}
	installed, err := installedSharedPin()
	if err != nil {
		return upgradeSource{}, err
	}
	pins, err := currentRepositoryProducers(a.API, actual.Repository)
	if err != nil {
		return upgradeSource{}, err
	}
	recipes, err := maintainedUpgradeRecipes(a.API, pins)
	if err != nil {
		return upgradeSource{}, err
	}
	direction, err := nativeSourceDirection(a.API, installed, pins.Core)
	if err != nil {
		return upgradeSource{}, err
	}
	if direction < 0 {
		return upgradeSource{}, errors.New("NATIVE_EXECUTION_SOURCE_BEHIND_INSTALLED")
	}
	current := upgradeSource{Kind: "installed-current", Execution: &actual.SharedExecution,
		Producers: pins, recipes: recipes, maintained: true, nativeLatest: true}
	plan, err := planUpgradeSourceAt(a.API, actual.Repository, "latest", false,
		"", "", &current)
	if err != nil {
		return upgradeSource{}, err
	}
	if plan.Repository.ID != actual.RepositoryID || plan.MemberID != actual.MemberID ||
		!sameProducerSources(plan.Current, pins) {
		return upgradeSource{}, errors.New("NATIVE_MIGRATION_CURRENT_SOURCE_CHANGED")
	}
	if _, err := nativeMigrationPlan(a.API, actual, plan); err != nil {
		return upgradeSource{}, err
	}
	present, err := readSharedWakeDestination(a.API, actual.Repository,
		actual.SharedExecution)
	if err != nil {
		return upgradeSource{}, err
	}
	if !present {
		return upgradeSource{}, errors.New("NATIVE_MIGRATION_EXISTING_DESTINATION_REQUIRED")
	}
	if _, _, err := managedPool(a.API, actual.Repository); err != nil {
		return upgradeSource{}, err
	}
	release, err := resolveUpgradeMetadata(a.API, version)
	if err != nil {
		if err.Error() == "NOT_FOUND" {
			return current, nil
		}
		return upgradeSource{}, err
	}
	selected, err := chooseNativeSource(a.API, current, &release)
	if err != nil {
		return upgradeSource{}, err
	}
	if selected.Kind == "" {
		selected.recipes, err = upgradeRecipes(a.API, selected.Producers)
	}
	selected.maintained, selected.nativeLatest = true, true
	selected.nativeExecution = &actual.SharedExecution
	return selected, err
}

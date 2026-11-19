package cli

import (
	"errors"
	"net/url"
)

// Maintained repository state supplies its current sources. Retrieval artifact
// locators do not authorize a different package or downgrade an installed host.
func currentRepositoryProducers(api API, name string) (Producers, error) {
	var pins Producers
	repo, _, err := repository(api, name, false)
	if err != nil {
		return pins, err
	}
	var ref reference
	if err := api.Call("GET", "repos/"+name+"/git/ref/heads/"+
		url.PathEscape(repo.DefaultBranch), nil, &ref); err != nil {
		return pins, err
	}
	if !commitPattern.MatchString(ref.Object.SHA) {
		return pins, errors.New("TARGET_HEAD_UNAVAILABLE")
	}
	data, _, err := content(api, name, ".nibfin/producer-pins.json", ref.Object.SHA)
	if err != nil {
		return pins, err
	}
	if decodeUpgradePins(data, &pins) != nil || pins.Core.Repository != officialCore ||
		pins.Action.Repository != officialAction {
		return pins, errors.New("INVALID_INSTALLED_PRODUCERS")
	}
	return pins, nil
}

func planCurrentInitRecipe(api API, name, execution, recipe string) (InitPlan, error) {
	pins, err := currentRepositoryProducers(api, name)
	if err != nil {
		return InitPlan{}, err
	}
	plan, err := planInitRecipeWithPins(api, name, execution, recipe, pins)
	plan.Sources = &pins
	return plan, err
}

func planNativeHostInit(api API, name, execution string, host sharedHost) (
	InitPlan, error) {
	pins, err := currentRepositoryProducers(api, host.Repository)
	if err != nil {
		if err.Error() == "NOT_FOUND" {
			return planInitRecipe(api, name, execution, "issue")
		}
		return InitPlan{}, err
	}
	plan, err := planInitRecipeWithPins(api, name, execution, "issue", pins)
	plan.Sources = &pins
	if err == nil || host.adoptionStatus(plan.Repository.ID) != "observed" {
		return plan, err
	}
	// Recognize unchanged adopted files as migration input; the ordinary upgrade
	// publisher owns their native proposal. New peers never adopt artifact files.
	for _, recipe := range []string{"issue", "issue-managed"} {
		observed, readErr := planCurrentInitRecipe(api, name, execution, recipe)
		if readErr == nil && len(observed.Files) == 0 {
			return observed, nil
		}
	}
	return plan, err
}

func managedPlanProducers(plan InitPlan) Producers {
	if plan.Sources != nil {
		return *plan.Sources
	}
	return Pins()
}

func refreshNativeInitPlan(api API, plan InitPlan) (InitPlan, error) {
	if plan.Execution == nil {
		return InitPlan{}, errors.New("NATIVE_WAKE_PLAN_REQUIRED")
	}
	pins := managedPlanProducers(plan)
	if plan.Execution != nil {
		current, err := currentRepositoryProducers(api, plan.Execution.Repository)
		if err == nil {
			pins = current
		} else if err.Error() != "NOT_FOUND" {
			return InitPlan{}, err
		}
	}
	current, err := planInitRecipeWithPins(api, plan.Repository.FullName,
		plan.Execution.Repository, "issue", pins)
	current.Sources, current.Agent, current.NativeManaged = &pins, plan.Agent, true
	return current, err
}

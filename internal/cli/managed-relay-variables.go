package cli

import (
	"encoding/json"
	"errors"
)

func requireManagedRouteEligible(api API, repo string) error {
	var secret struct{ Name string }
	err := api.Call("GET", "repos/"+repo+"/actions/secrets/NIBFIN_WAKE_TOKEN", nil, &secret)
	if err == nil || err.Error() != "NOT_FOUND" {
		return errors.New("MANAGED_LEGACY_ROUTE_REQUIRES_EXPLICIT_MIGRATION")
	}
	return nil
}

func managedPool(api API, repo string) (actionsVariable, bool, error) {
	var pool actionsVariable
	err := api.Call("GET", "repos/"+repo+"/actions/variables/"+sharedPoolVariable,
		nil, &pool)
	if err != nil && err.Error() == "NOT_FOUND" {
		return pool, false, nil
	}
	if err != nil || pool.Name != sharedPoolVariable ||
		(pool.Value != "false" && pool.Value != "true") {
		return pool, false, errors.New("MANAGED_POOL_REQUIRES_READBACK")
	}
	return pool, true, nil
}

func requireManagedPoolEligibility(api API, repo string, config relayConfiguration,
	id int64) error {
	pool, _, err := managedPool(api, repo)
	if err != nil || pool.Value != "true" {
		return err
	}
	for _, origin := range config.Origins {
		if origin.RepositoryID == id && origin.Repository == repo {
			return nil
		}
	}
	return errors.New("MANAGED_LEGACY_ROUTE_REQUIRES_EXPLICIT_MIGRATION")
}

func setManagedPool(api API, repo string) error {
	pool, exists, err := managedPool(api, repo)
	if err != nil || pool.Value == "true" {
		return err
	}
	base := "repos/" + repo + "/actions/variables"
	method, path := "POST", base
	if exists {
		method, path = "PATCH", base+"/"+sharedPoolVariable
	}
	expected := actionsVariable{Name: sharedPoolVariable, Value: "true"}
	// The write may succeed despite a lost response. Never mutate again before
	// an authenticated GET establishes its effect.
	_ = api.Call(method, path, expected, nil)
	observed, present, err := managedPool(api, repo)
	if err != nil || !present || observed != expected {
		return errors.New("MANAGED_POOL_EFFECT_UNVERIFIED_INSPECT_BEFORE_RETRY")
	}
	return nil
}

func verifyManagedPools(api API, config relayConfiguration,
	execution SharedExecution) error {
	for _, origin := range config.Origins {
		pool, exists, err := managedPool(api, origin.Repository)
		if err != nil || !exists || pool.Value != "true" {
			return errors.New("MANAGED_POOL_REQUIRES_READBACK")
		}
		var destination actionsVariable
		err = api.Call("GET", "repos/"+origin.Repository+"/actions/variables/"+
			sharedDestinationVariable, nil, &destination)
		var binding SharedExecution
		if err != nil || destination.Name != sharedDestinationVariable ||
			json.Unmarshal([]byte(destination.Value), &binding) != nil || binding != execution {
			return errors.New("MANAGED_DESTINATION_REQUIRES_READBACK")
		}
	}
	return nil
}

package cli

import (
	"errors"
	"fmt"
)

// Both an individual read and a complete listing are required, including after
// a lost removal response. A 404 alone may mean authorization was lost.
func rebuildRegistration(api API, pool sharedHost, expected sharedRegistration,
) (bool, error) {
	path := fmt.Sprintf("repos/%s/actions/runners/%d", pool.Repository, expected.ID)
	var individual RunnerInfo
	readErr := api.Call("GET", path, nil, &individual)
	all, listErr := listRunners(api, pool.Repository)
	if listErr != nil || readErr != nil && readErr.Error() != "NOT_FOUND" {
		return false, errors.New("REBUILD_REGISTRATION_READBACK_UNKNOWN")
	}
	matches := []RunnerInfo{}
	seen := map[int64]bool{}
	for _, runner := range all {
		if runner.ID <= 0 || seen[runner.ID] {
			return false, errors.New("REBUILD_REGISTRATION_LIST_AMBIGUOUS")
		}
		seen[runner.ID] = true
		if runner.ID == expected.ID {
			matches = append(matches, runner)
		}
	}
	if readErr != nil && len(matches) == 0 {
		return false, nil
	}
	if readErr != nil || len(matches) != 1 {
		return false, errors.New("REBUILD_REGISTRATION_READBACK_UNKNOWN")
	}
	for _, runner := range []RunnerInfo{individual, matches[0]} {
		if runner.ID != expected.ID || runner.Name != expected.Name ||
			runner.OS != "Linux" || !hasMemberLabel(runner, pool.MemberID) ||
			!hasRunnerLabel(runner, sharedRunnerLabel) {
			return false, errors.New("REBUILD_REGISTRATION_IDENTITY_CHANGED")
		}
	}
	return true, nil
}

func removeRebuildRegistration(api API, pool sharedHost,
	runner sharedRegistration) error {
	present, err := rebuildRegistration(api, pool, runner)
	if err != nil || !present {
		return err
	}
	path := fmt.Sprintf("repos/%s/actions/runners/%d", pool.Repository, runner.ID)
	// GitHub owns this removal. Core owns all local cleanup and settlement.
	removalErr := api.Call("DELETE", path, nil, nil)
	present, err = rebuildRegistration(api, pool, runner)
	if err != nil || present || removalErr != nil &&
		removalErr.Error() == "PERMISSION_DENIED" {
		return errors.New("REBUILD_REMOVAL_INCOMPLETE_READ_BACK_BEFORE_RETRY")
	}
	return nil
}

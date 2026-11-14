package cli

import (
	"errors"
	"path/filepath"
	"time"
)

const sharedRunnerLabel = "nibfin-shared"

func (a App) registerSharedSlot(repo Repository, user User, path string,
	record RunnerInstall, now time.Time) (RunnerInstall, error) {
	first := sharedRunnerSlotName(repo.ID, user.ID, 1)
	second := sharedRunnerSlotName(repo.ID, user.ID, 2)
	if record.Name != first && record.Name != second ||
		filepath.Base(path) != record.Name {
		return record, errors.New("SHARED_RUNNER_SLOT_IDENTITY_MISMATCH")
	}
	host, err := discoverSharedHost(a.Process)
	if err != nil {
		return record, err
	}
	if host != nil {
		return record, errors.New("SHARED_RUNNER_POOL_ALREADY_ADOPTED")
	}
	pin := Pins().Core
	reviewed, _, err := content(a.API, pin.Repository, sharedRunnerWorkflow, pin.Commit)
	if err != nil {
		return record, err
	}
	if err := verifySharedExecutionWorkflow(a.API, repo, reviewed); err != nil {
		return record, err
	}
	runners, err := listRunners(a.API, repo.FullName)
	if err != nil {
		return record, err
	}
	for _, runner := range runners {
		if hasRunnerLabel(runner, sharedRunnerLabel) &&
			(!hasMemberLabel(runner, user.ID) ||
				runner.Name != first && runner.Name != second) {
			return record, errors.New("EXISTING_SHARED_RUNNER_POOL_REQUIRES_INSPECTION")
		}
	}
	if record.State == "registered" {
		return readRegisteredSharedSlot(a.API, repo, user, path, record)
	}
	if err := verifyRunnerPackage(path, record.Application, true,
		runnerManagementLock); err != nil {
		return record, err
	}
	record, err = registerRunnerWithLabel(a.API, a.Runner, repo, user, path, record, now,
		sharedRunnerLabel)
	if err != nil {
		return record, err
	}
	return readRegisteredSharedSlot(a.API, repo, user, path, record)
}

func readRegisteredSharedSlot(api API, repo Repository, user User, path string,
	record RunnerInstall) (RunnerInstall, error) {
	if err := localRunnerMatches(path, record); err != nil {
		return record, err
	}
	runner, err := ownedRunner(api, repo, user, record)
	if err != nil {
		return record, err
	}
	if runner.Status != "offline" || runner.Busy ||
		!hasRunnerLabel(runner, sharedRunnerLabel) {
		return record, errors.New("SHARED_OFFLINE_RUNNER_REGISTRATION_CHANGED")
	}
	return record, nil
}

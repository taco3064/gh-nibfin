package cli

import (
	"errors"
	"io"
)

// Use the same lock, verified package and read-back path as explicit registration.
// On interruption a rerun observes the first slot before registering the second.
func (a App) registerInitSharedSlots(plan InitPlan,
	directories []string) ([]sharedRegistration, error) {
	if len(directories) != 2 || directories[0] == directories[1] {
		return nil, errors.New("TWO_SHARED_RUNNER_DIRECTORIES_REQUIRED")
	}
	var observed []sharedRegistration
	for _, directory := range directories {
		args := []string{"register", "--shared", "--apply",
			"--repo", plan.Repository.FullName, "--directory", directory}
		if err := a.runRunnerForInstall(args, io.Discard, &plan); err != nil {
			return observed, err
		}
		record, err := readRunnerRecord(directory, plan.Repository, plan.Member)
		if err != nil {
			return observed, err
		}
		if record.State != "registered" {
			return observed, errors.New("SHARED_OFFLINE_RUNNER_REGISTRATION_CHANGED")
		}
		record, err = readRegisteredSharedSlot(
			a.API, plan.Repository, plan.Member, directory, record)
		if err != nil {
			return observed, err
		}
		observed = append(observed, sharedRegistration{
			ID: record.RunnerID, Name: record.Name, Status: "offline",
		})
	}
	if observed[0].ID == observed[1].ID {
		return observed, errors.New("SHARED_RUNNER_REGISTRATION_IDENTITIES_CONFLICT")
	}
	return observed, nil
}

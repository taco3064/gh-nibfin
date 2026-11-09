package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

type initRunner struct {
	Directory string `json:"directory"`
	Name      string `json:"name"`
}

func (a App) planInitRunner(plan InitPlan, path, name string) (*initRunner, error) {
	if path == "" || !runnerNamePattern.MatchString(name) {
		return nil, errors.New("INVALID_INIT_RUNNER_OPTIONS")
	}
	repo, user, err := repository(a.API, plan.Repository.FullName, true)
	if err != nil {
		return nil, err
	}
	if repo.ID != plan.Repository.ID || user.ID != plan.Member.ID {
		return nil, errors.New("INSTALLATION_PLAN_CHANGED")
	}
	directory, err := runnerDirectory(path)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateRunnerParent(directory); err != nil {
		return nil, err
	}
	return &initRunner{Directory: directory, Name: name}, nil
}

func (a App) applyInitRunner(plan InitPlan, runner initRunner, out io.Writer) error {
	if _, err := a.planInitRunner(plan, runner.Directory, runner.Name); err != nil {
		return err
	}
	args := []string{"--repo", plan.Repository.FullName, "--directory", runner.Directory,
		"--apply"}
	if _, err := os.Lstat(runner.Directory); errors.Is(err, os.ErrNotExist) {
		create := append([]string{"create", "--name", runner.Name}, args...)
		if err := a.runRunner(create, out); err != nil {
			return err
		}
	} else if err != nil {
		return errors.New("RUNNER_DIRECTORY_UNAVAILABLE")
	}
	record, err := readRunnerRecord(runner.Directory, plan.Repository, plan.Member)
	if err != nil {
		return err
	}
	if record.Name != runner.Name {
		return errors.New("RUNNER_MANAGEMENT_IDENTITY_MISMATCH")
	}
	if record.State == "created" {
		if err := a.runRunner(append([]string{"register"}, args...), out); err != nil {
			return err
		}
		record, err = readRunnerRecord(runner.Directory, plan.Repository, plan.Member)
		if err != nil {
			return err
		}
	}
	if record.State != "registered" {
		return &CommandError{Code: "RUNNER_REGISTRATION_REQUIRES_RECONCILIATION", Exit: 2}
	}
	if err := localRunnerMatches(runner.Directory, record); err != nil {
		return err
	}
	observed, err := ownedRunner(a.API, plan.Repository, plan.Member, record)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]any{
		"status": "runner_registered", "runner": observed, "host_adoption": "required",
		"protected_execution": "unverified",
	})
}

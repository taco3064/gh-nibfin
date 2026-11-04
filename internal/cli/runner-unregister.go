package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func runnerAbsent(api API, repo string, id int64) error {
	runners, err := listRunners(api, repo)
	if err != nil {
		return err
	}
	for _, runner := range runners {
		if runner.ID == id {
			return errors.New("RUNNER_STILL_REGISTERED")
		}
	}
	return nil
}

func unregisterRunner(api API, host RunnerHost, repo Repository, user User,
	directory string, record RunnerInstall, now time.Time) (RunnerInstall, error) {
	servicePath := filepath.Join(directory, ".service")
	if _, err := os.Lstat(servicePath); !errors.Is(err, os.ErrNotExist) {
		return record, errors.New("RUNNER_SERVICE_MUST_BE_MANAGED_OUTSIDE_CLI")
	}
	if record.State == "remote-removed-local-config-retained" {
		return unregisterLocal(api, host, repo, directory, record)
	}
	if record.RunnerID <= 0 || record.State != "registered" {
		return record, errors.New("REGISTERED_RUNNER_REQUIRED_INSPECT_PENDING_STATE")
	}
	runner, err := ownedRunner(api, repo, user, record)
	if err != nil {
		return record, err
	}
	if runner.Busy || runner.Status != "offline" {
		return record, errors.New("STOP_RUNNER_LOCALLY_BEFORE_UNREGISTER")
	}
	if err := localRunnerMatches(directory, record); err != nil {
		return record, err
	}
	app, err := runnerApplication(api, repo.FullName)
	if err != nil {
		return record, err
	}
	if app != record.Application {
		return record, errors.New("RUNNER_SOURCE_CHANGED_USE_OFFICIAL_MANUAL_REMOVAL")
	}
	if err := verifyRunnerFiles(directory, app); err != nil {
		return record, err
	}
	if err := host.Check(directory); err != nil {
		return record, err
	}
	record.State = "unregister-pending"
	if err := writeRunnerRecord(directory, record); err != nil {
		return record, err
	}
	token, err := registrationToken(api, repo.FullName, "remove-token", now)
	if err != nil {
		if err.Error() == "PERMISSION_DENIED" || err.Error() == "NOT_FOUND" ||
			err.Error() == "RUNNER_TOKEN_MISSING_OR_EXPIRED" {
			record.State = "registered"
			return record, errors.Join(err, writeRunnerRecord(directory, record))
		}
		return record, err
	}
	commandErr := host.Configure(directory, "remove", map[string]string{
		"TOKEN": token, "UNATTENDED": "true"})
	path := fmt.Sprintf("repos/%s/actions/runners/%d", repo.FullName, record.RunnerID)
	var observed RunnerInfo
	readErr := api.Call("GET", path, nil, &observed)
	if readErr == nil || readErr.Error() != "NOT_FOUND" ||
		runnerAbsent(api, repo.FullName, record.RunnerID) != nil {
		return record, errors.Join(commandErr, errors.New("RUNNER_UNREGISTER_RESULT_UNKNOWN"))
	}
	if err := runnerConfigAbsent(directory); err != nil {
		return record, errors.Join(commandErr, err)
	}
	if commandErr != nil {
		return record, commandErr
	}
	record.State, record.RunnerID = "created", 0
	return record, writeRunnerRecord(directory, record)
}

func unregisterLocal(api API, host RunnerHost, repo Repository, directory string,
	record RunnerInstall) (RunnerInstall, error) {
	if err := runnerAbsent(api, repo.FullName, record.RunnerID); err != nil {
		return record, err
	}
	if err := localRunnerMatches(directory, record); err != nil {
		return record, err
	}
	app, err := runnerApplication(api, repo.FullName)
	if err != nil {
		return record, err
	}
	if app != record.Application || verifyRunnerFiles(directory, app) != nil {
		return record, errors.New("RUNNER_PACKAGE_CHANGED_USE_OFFICIAL_MANUAL_REMOVAL")
	}
	if err := host.Check(directory); err != nil {
		return record, err
	}
	record.State = "unregister-pending"
	if err := writeRunnerRecord(directory, record); err != nil {
		return record, err
	}
	if err := host.Configure(directory, "remove-local",
		map[string]string{"UNATTENDED": "true"}); err != nil {
		return record, err
	}
	if err := runnerConfigAbsent(directory); err != nil {
		return record, err
	}
	record.State, record.RunnerID = "created", 0
	return record, writeRunnerRecord(directory, record)
}

func runnerConfigAbsent(directory string) error {
	for _, file := range []string{".runner", ".credentials", ".credentials_rsaparams"} {
		path := filepath.Join(directory, file)
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("RUNNER_LOCAL_CONFIG_REMAINS")
		}
	}
	return nil
}

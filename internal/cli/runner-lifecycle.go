package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func createRunner(api API, download RunnerDownload, repo Repository, user User,
	name, directory string) (result RunnerInstall, err error) {
	app, err := runnerApplication(api, repo.FullName)
	if err != nil {
		return result, err
	}
	return createRunnerPackage(download, repo, user, name, directory, app)
}

func createRunnerPackage(download RunnerDownload, repo Repository, user User,
	name, directory string, app RunnerApplication) (result RunnerInstall, err error) {
	if err := requirePrivateRunnerParent(directory); err != nil {
		return result, err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return result, errors.New("NEW_RUNNER_DIRECTORY_REQUIRED")
	}
	defer func() {
		if err != nil {
			if cleanupErr := os.RemoveAll(directory); cleanupErr != nil {
				err = errors.Join(err, errors.New("FAILED_RUNNER_DIRECTORY_CLEANUP_INCOMPLETE"))
			}
		}
	}()
	if err := initializePrivateRunnerDirectory(directory); err != nil {
		return result, err
	}
	archive := filepath.Join(directory, ".nibfin-runner-package")
	if err := download(app, archive); err != nil {
		return result, err
	}
	zipped := strings.HasSuffix(app.Filename, ".zip")
	if err := unpackRunner(archive, directory, zipped); err != nil {
		return result, err
	}
	if err := verifyRunnerFiles(directory, app); err != nil {
		return result, err
	}
	result = RunnerInstall{Repository: repo.FullName, RepositoryID: repo.ID,
		MemberID: user.ID, Name: name, Application: app, State: "created"}
	return result, writeRunnerRecord(directory, result)
}

func registerRunner(api API, host RunnerHost, repo Repository, user User,
	directory string, record RunnerInstall, now time.Time) (RunnerInstall, error) {
	return registerRunnerWithLabel(api, host, repo, user, directory, record, now, "")
}

func registerRunnerWithLabel(api API, host RunnerHost, repo Repository, user User,
	directory string, record RunnerInstall, now time.Time,
	additionalLabel string) (RunnerInstall, error) {
	if record.State != "created" || record.RunnerID != 0 {
		return record, errors.New("RUNNER_ALREADY_REGISTERED_OR_PENDING_INSPECT_FIRST")
	}
	app, err := runnerApplicationForRegistration(api, repo.FullName, additionalLabel)
	if err != nil {
		return record, err
	}
	if app != record.Application {
		return record, errors.New("RUNNER_SOURCE_CHANGED_RECREATE_FROM_OFFICIAL_SOURCE")
	}
	if err := verifyRunnerFiles(directory, app); err != nil {
		return record, err
	}
	if err := host.Check(directory); err != nil {
		return record, err
	}
	runners, err := listRunners(api, repo.FullName)
	if err != nil {
		return record, err
	}
	for _, runner := range runners {
		if runner.Name == record.Name {
			return record, errors.New("RUNNER_NAME_ALREADY_EXISTS")
		}
	}
	record.State = "registration-pending"
	if err := writeRunnerRecord(directory, record); err != nil {
		return record, err
	}
	token, err := registrationToken(api, repo.FullName, "registration-token", now)
	if err != nil {
		if err.Error() == "PERMISSION_DENIED" || err.Error() == "NOT_FOUND" ||
			err.Error() == "RUNNER_TOKEN_MISSING_OR_EXPIRED" {
			record.State = "created"
			return record, errors.Join(err, writeRunnerRecord(directory, record))
		}
		return record, err
	}
	labels := memberLabel(user.ID)
	if additionalLabel != "" {
		labels += "," + additionalLabel
	}
	inputs := map[string]string{
		"TOKEN": token, "URL": "https://github.com/" + repo.FullName,
		"NAME": record.Name, "LABELS": labels, "WORK": "_work",
		"UNATTENDED": "true"}
	if additionalLabel == sharedRunnerLabel {
		inputs["DISABLEUPDATE"] = "true"
	}
	commandErr := host.Configure(directory, "configure", inputs)
	runners, err = listRunners(api, repo.FullName)
	if err != nil {
		return record, errors.Join(commandErr, errors.New("RUNNER_RESULT_READBACK_UNAVAILABLE"))
	}
	var matches []RunnerInfo
	for _, runner := range runners {
		if runner.Name == record.Name && hasMemberLabel(runner, user.ID) &&
			(additionalLabel == "" || hasRunnerLabel(runner, additionalLabel)) {
			matches = append(matches, runner)
		}
	}
	if len(matches) != 1 || matches[0].ID <= 0 {
		return record, errors.Join(commandErr, errors.New("RUNNER_RESULT_UNKNOWN_NO_RETRY"))
	}
	record.RunnerID = matches[0].ID
	if err := localRunnerMatches(directory, record); err == nil && commandErr == nil {
		record.State = "registered"
	}
	if err := writeRunnerRecord(directory, record); err != nil {
		return record, err
	}
	if record.State != "registered" {
		return record, errors.Join(commandErr, errors.New("RUNNER_LOCAL_RESULT_INCOMPLETE"))
	}
	return record, nil
}

func runnerApplicationForRegistration(api API, repo, label string) (
	RunnerApplication, error,
) {
	app, err := runnerApplication(api, repo)
	if label != sharedRunnerLabel || err == nil || err.Error() !=
		"OFFICIAL_RUNNER_SOURCE_UNAVAILABLE" {
		return app, err
	}
	// GitHub may return no per-repository downloads before the first
	// registration. The shared Worker supports this exact public release.
	return sharedRunnerApplication(api)
}

func localRunnerMatches(directory string, record RunnerInstall) error {
	var settings struct {
		ID   int64  `json:"agentId"`
		Name string `json:"agentName"`
		URL  string `json:"gitHubUrl"`
	}
	if err := readJSONFile(filepath.Join(directory, ".runner"), &settings); err != nil {
		return err
	}
	if settings.ID != record.RunnerID || settings.Name != record.Name ||
		settings.URL != "https://github.com/"+record.Repository {
		return errors.New("LOCAL_RUNNER_IDENTITY_MISMATCH")
	}
	return nil
}

func removeRunner(api API, repo Repository, user User, directory string,
	record RunnerInstall) (RunnerInstall, error) {
	if record.State != "registered" && record.State != "removal-pending" &&
		record.State != "remote-removed-local-config-retained" {
		return record, errors.New("RUNNER_PENDING_CONFIRM_OFFICIAL_PROCESS_STOPPED")
	}
	runner, err := ownedRunner(api, repo, user, record)
	if err != nil {
		if err.Error() == "NOT_FOUND" && (record.State == "removal-pending" ||
			record.State == "remote-removed-local-config-retained") &&
			runnerAbsent(api, repo.FullName, record.RunnerID) == nil {
			record.State = "remote-removed-local-config-retained"
			return record, writeRunnerRecord(directory, record)
		}
		return record, err
	}
	if runner.Busy || runner.Status != "offline" {
		return record, errors.New("STOP_RUNNER_LOCALLY_BEFORE_REMOVAL")
	}
	path := fmt.Sprintf("repos/%s/actions/runners/%d", repo.FullName, runner.ID)
	record.State = "removal-pending"
	if err := writeRunnerRecord(directory, record); err != nil {
		return record, err
	}
	writeErr := api.Call("DELETE", path, nil, nil)
	var observed RunnerInfo
	readErr := api.Call("GET", path, nil, &observed)
	if readErr == nil || readErr.Error() != "NOT_FOUND" ||
		runnerAbsent(api, repo.FullName, record.RunnerID) != nil {
		return record, errors.Join(writeErr, errors.New("RUNNER_REMOVAL_UNKNOWN_NO_RETRY"))
	}
	record.State = "remote-removed-local-config-retained"
	return record, writeRunnerRecord(directory, record)
}

package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func quickInstall(state quickState) RunnerInstall {
	return RunnerInstall{Repository: state.Request.Repository,
		RepositoryID: state.Request.RepositoryID, MemberID: state.Request.MemberID,
		Name: state.Request.Name, Application: state.App,
		RunnerID: state.RunnerID, State: "registration-pending"}
}

func (a App) quickRegister(repo Repository, user User, directory string,
	state quickState, q quickFlags, clock func() time.Time, out io.Writer) error {
	if state.Stage != "requested" || state.Directory == "" {
		return quickPending(out, "HANDOFF_ALREADY_USED_INSPECT_NO_CONFIGURATION_RETRY")
	}
	data, err := readQuick(*q.secret, true)
	if err != nil {
		return err
	}
	defer clear(data)
	if quickDigest(data) != *q.origin {
		return errors.New("HANDOFF_MANAGEMENT_ORIGIN_UNCONFIRMED")
	}
	var capability quickCapability
	if decodeQuick(data, &capability) != nil ||
		capability.Kind != "nibfin-registration-v1" || capability.Request != state.Request ||
		state.Request.Platform != runtime.GOOS ||
		state.Request.Architecture != runtime.GOARCH {
		return errors.New("HANDOFF_ATTEMPT_OR_RECIPIENT_MISMATCH")
	}
	if err := quickTokenLifetime(capability.Token, clock()); err != nil {
		return err
	}
	app, err := selectRunnerApplication([]RunnerApplication{capability.App},
		runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	path, err := runnerDirectory(state.Directory)
	if err != nil || path != state.Directory {
		return errors.New("HANDOFF_RUNNER_DIRECTORY_CHANGED")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("NEW_RUNNER_DIRECTORY_REQUIRED_NO_CONFIGURATION_RETRY")
	}
	state.Stage, state.App = "registration-pending", app
	state.Origin, state.Expires = *q.origin, capability.Token.Expires
	if err := saveQuick(directory, state, false); err != nil {
		return quickPending(out, "HANDOFF_STATE_UNRESOLVED_NO_CONFIGURATION")
	}
	record, err := createRunnerPackage(a.Download, repo, user, state.Request.Name,
		path, app)
	if err != nil {
		return quickPending(out, "HANDOFF_PACKAGE_SETUP_INCOMPLETE_NO_CONFIGURATION")
	}
	if err := a.Runner.Check(path); err != nil {
		return quickPending(out, "HANDOFF_OFFICIAL_RUNNER_UNAVAILABLE_NO_CONFIGURATION")
	}
	// Downloading may take longer than the remaining provider token lifetime.
	if err := quickTokenLifetime(capability.Token, clock()); err != nil {
		return quickPending(out, "HANDOFF_EXPIRED_DURING_PACKAGE_SETUP")
	}
	record.State = "registration-pending"
	if err := writeRunnerRecord(path, record); err != nil {
		return quickPending(out, "HANDOFF_RUNNER_STATE_UNRESOLVED_NO_CONFIGURATION")
	}
	err = a.Runner.Configure(path, "configure", map[string]string{
		"TOKEN": capability.Token.Value, "URL": "https://github.com/" + repo.FullName,
		"NAME": state.Request.Name, "LABELS": memberLabel(user.ID), "WORK": "_work",
		"UNATTENDED": "true"})
	capability.Token.Value = ""
	if err != nil {
		return quickPending(out, "HANDOFF_CONFIGURATION_UNKNOWN_INSPECT_NO_RETRY")
	}
	return quickInspect(directory, state, out)
}

func quickLocalRecord(state quickState) (RunnerInstall, error) {
	record := quickInstall(state)
	var settings struct {
		ID int64 `json:"agentId"`
	}
	err := readJSONFile(filepath.Join(state.Directory, ".runner"), &settings)
	if err != nil || settings.ID <= 0 {
		return record, errors.New("HANDOFF_LOCAL_SETTINGS_UNAVAILABLE")
	}
	record.RunnerID = settings.ID
	if err := localRunnerMatches(state.Directory, record); err != nil {
		return record, err
	}
	if err := verifyRunnerFiles(state.Directory, state.App); err != nil {
		return record, err
	}
	return record, nil
}

func quickInspect(directory string, state quickState, out io.Writer) error {
	if state.Directory == "" || state.Stage != "registration-pending" {
		return errors.New("MEMBER_PENDING_ATTEMPT_REQUIRED")
	}
	record, err := quickLocalRecord(state)
	if err != nil {
		return quickPending(out, "HANDOFF_LOCAL_RESULT_UNRESOLVED_NO_RETRY")
	}
	result := quickResult{Kind: "local-observation-v1", Request: state.Request,
		RunnerID: record.RunnerID}
	path := filepath.Join(directory, "result.json")
	_, statErr := os.Lstat(path)
	_, err = writeQuick(path, result, errors.Is(statErr, os.ErrNotExist))
	if err != nil {
		return quickPending(out, "HANDOFF_LOCAL_OBSERVATION_WRITE_UNRESOLVED")
	}
	return quickPending(out, "Send result.json for management read-back; then reconcile")
}

func quickReconcile(repo Repository, user User, directory string, state quickState,
	q quickFlags, out io.Writer) error {
	if state.Directory == "" || state.Stage != "registration-pending" {
		return errors.New("MEMBER_PENDING_ATTEMPT_REQUIRED")
	}
	data, err := readQuick(*q.confirmation, false)
	if err != nil {
		return err
	}
	if quickDigest(data) != *q.origin {
		return errors.New("HANDOFF_MANAGEMENT_ORIGIN_UNCONFIRMED")
	}
	var confirmation quickResult
	if decodeQuick(data, &confirmation) != nil ||
		confirmation.Kind != "management-observation-v1" ||
		confirmation.Request != state.Request || confirmation.RunnerID <= 0 {
		return errors.New("HANDOFF_MANAGEMENT_CONFIRMATION_MISMATCH")
	}
	// Validate the official native settings here, rather than trusting the
	// metadata that the recipient sent to management or a routing label alone.
	record, err := quickLocalRecord(state)
	if err != nil || record.RunnerID != confirmation.RunnerID {
		return quickPending(out, "HANDOFF_LOCAL_AND_MANAGEMENT_RESULTS_DISAGREE")
	}
	if _, err := readRunnerRecord(state.Directory, repo, user); err != nil {
		return quickPending(out, "HANDOFF_RUNNER_RECORD_UNAVAILABLE")
	}
	record.State = "registered"
	if err := writeRunnerRecord(state.Directory, record); err != nil {
		return quickPending(out, "HANDOFF_RUNNER_PERSISTENCE_UNRESOLVED")
	}
	state.Stage, state.RunnerID = "registered", record.RunnerID
	if err := saveQuick(directory, state, false); err != nil {
		return quickPending(out, "HANDOFF_COMPLETION_PERSISTENCE_UNRESOLVED")
	}
	return quickOutput(out, "runner_registered", "Separate trusted host adoption required")
}

package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func quickPending() error {
	return &CommandError{Code: "QUICK_PENDING_RECONCILE_NO_REGISTRATION_RETRY", Exit: 2}
}

func (a App) quickIssue(o quickOptions, state quickState, repo Repository,
	out io.Writer) error {
	app, err := runnerApplication(a.API, repo.FullName)
	if err != nil {
		return err
	}
	runners, err := listRunners(a.API, repo.FullName)
	if err != nil {
		return err
	}
	for _, runner := range runners {
		if runner.Name == state.Request.Name {
			return errors.New("RUNNER_NAME_ALREADY_EXISTS")
		}
	}
	// Keep an immutable nonsecret recovery request beside the journal.
	requestPath := filepath.Join(o.directory, quickRequestFile)
	if err := quickWrite(requestPath, state.Request); err != nil {
		return err
	}
	state.Application = app
	if err := quickWrite(filepath.Join(o.directory, quickStateFile), state); err != nil {
		return err
	}
	token, expiry, err := runnerToken(a.API, repo.FullName,
		"registration-token", a.quickNow())
	if err != nil {
		return quickPending()
	}
	handoff := quickHandoff{Request: state.Request, Application: app, Token: token,
		Expires: expiry.Format(time.RFC3339)}
	path := filepath.Join(o.directory, quickHandoffFile)
	if err := quickWrite(path, handoff); err != nil {
		return errors.Join(quickPending(), quickRetireManagementSecret(o.directory))
	}
	var observed quickHandoff
	data, err := quickRead(path, &observed)
	if err != nil {
		return errors.Join(quickPending(), quickRetireManagementSecret(o.directory))
	}
	state.Phase = "issued-pending"
	if err := quickWrite(filepath.Join(o.directory, quickStateFile), state); err != nil {
		return errors.Join(quickPending(), quickRetireManagementSecret(o.directory))
	}
	// Only a digest goes to ordinary output. Send it over an independently
	// authenticated management channel; sending it beside the file proves nothing.
	return quickOutput(out, "pending", "issue", quickDigest(data))
}

func (a App) quickReceive(o quickOptions, state quickState, repo Repository,
	user User, out io.Writer) (err error) {
	if state.Phase != "awaiting" {
		if err := quickRetireInput(o.handoff); err != nil {
			return err
		}
		return quickPending()
	}
	// --apply consumes only the designated private handoff, including malformed
	// input. It cannot be used to retire an arbitrary state or Runner file.
	present, err := quickInputPresent(o.handoff)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("QUICK_FILE_UNAVAILABLE")
	}
	defer func() {
		if cleanupErr := quickRetireInput(o.handoff); cleanupErr != nil {
			err = cleanupErr
		}
	}()
	var handoff quickHandoff
	data, err := quickRead(o.handoff, &handoff)
	if err != nil {
		return err
	}
	if quickDigest(data) != o.approval {
		return errors.New("QUICK_MANAGEMENT_ORIGIN_UNCONFIRMED")
	}
	if handoff.Request != state.Request || handoff.Token == "" ||
		len(handoff.Token) > 16384 {
		return errors.New("QUICK_HANDOFF_CONTEXT_MISMATCH")
	}
	if err := quickLifetime(handoff.Expires, a.quickNow()); err != nil {
		return err
	}
	app, err := selectRunnerApplication([]RunnerApplication{handoff.Application})
	if err != nil {
		return err
	}
	// This journal is written before acquisition or configuration. Unknown effects
	// never reset it to awaiting; only management observation can reconcile them.
	state.Phase, state.Application = "configuration-pending", app
	if err := quickWrite(filepath.Join(o.directory, quickStateFile), state); err != nil {
		return quickPending()
	}
	record, err := createRunnerPackage(a.Download, repo, user, state.Request.Name,
		state.Directory, app)
	if err != nil {
		return quickPending()
	}
	if err := a.Runner.Check(state.Directory); err != nil {
		return quickPending()
	}
	record.State = "registration-pending"
	if err := writeRunnerRecord(state.Directory, record); err != nil {
		return quickPending()
	}
	// Package acquisition may have consumed most of the remaining token lifetime.
	if err := quickLifetime(handoff.Expires, a.quickNow()); err != nil {
		return err
	}
	configureErr := a.Runner.Configure(state.Directory, "configure", map[string]string{
		"TOKEN": handoff.Token, "URL": "https://github.com/" + repo.FullName,
		"NAME": state.Request.Name, "LABELS": memberLabel(user.ID), "WORK": "_work",
		"UNATTENDED": "true"})
	handoff.Token = ""
	// Even a successful official configuration is not management confirmation.
	// A failed command may have registered; preserve both journals and local files.
	if configureErr != nil {
		return quickPending()
	}
	if err := quickOutput(out, "pending", "receive", ""); err != nil {
		return err
	}
	return quickPending()
}

func (a App) quickConfirm(o quickOptions, state quickState, repo Repository,
	out io.Writer) error {
	if state.Phase != "issue-pending" && state.Phase != "issued-pending" &&
		state.Phase != "observed-pending" && state.Phase != "aborted-pending" {
		return errors.New("QUICK_MANAGEMENT_PHASE_INVALID")
	}
	runners, err := listRunners(a.API, repo.FullName)
	if err != nil {
		return quickPending()
	}
	var matches []RunnerInfo
	for _, runner := range runners {
		if runner.Name == state.Request.Name {
			matches = append(matches, runner)
		}
	}
	if len(matches) != 1 || matches[0].ID <= 0 ||
		!hasMemberLabel(matches[0], state.Request.Member.ID) ||
		!quickExclusiveMemberLabel(matches[0], state.Request.Member.ID) {
		return quickPending()
	}
	// Observe using the management credential, not recipient-supplied Runner IDs.
	record := RunnerInstall{Name: state.Request.Name, RunnerID: matches[0].ID}
	observedRunner, err := ownedRunner(a.API, repo, state.Request.Member, record)
	if err != nil || !quickExclusiveMemberLabel(observedRunner, state.Request.Member.ID) {
		return quickPending()
	}
	state.Phase, state.RunnerID = "observed-pending", matches[0].ID
	if err := quickWrite(filepath.Join(o.directory, quickStateFile), state); err != nil {
		return quickPending()
	}
	now := a.quickNow()
	proof := quickConfirmation{Request: state.Request, RunnerID: state.RunnerID,
		ObservedAt: now.Format(time.RFC3339),
		Expires:    now.Add(5 * time.Minute).Format(time.RFC3339)}
	path := filepath.Join(o.directory, quickConfirmationFile)
	if err := quickWrite(path, proof); err != nil {
		return quickPending()
	}
	var observed quickConfirmation
	data, err := quickRead(path, &observed)
	if err != nil {
		return quickPending()
	}
	if err := quickRetireManagementSecret(o.directory); err != nil {
		return err
	}
	return quickOutput(out, "pending", "confirm", quickDigest(data))
}

func (a App) quickFinish(o quickOptions, state quickState, repo Repository,
	user User, out io.Writer) error {
	if state.Phase != "configuration-pending" && state.Phase != "confirmed" &&
		state.Phase != "aborted-pending" {
		return quickPending()
	}
	var proof quickConfirmation
	data, err := quickRead(o.confirmation, &proof)
	if err != nil {
		return err
	}
	if quickDigest(data) != o.approval || proof.Request != state.Request ||
		proof.RunnerID <= 0 || !proof.fresh(a.quickNow()) {
		return errors.New("QUICK_MANAGEMENT_CONFIRMATION_UNTRUSTED")
	}
	record, err := readRunnerRecord(state.Directory, repo, user)
	if err != nil {
		return quickPending()
	}
	if record.Application != state.Application || record.Name != state.Request.Name {
		return errors.New("QUICK_LOCAL_RECORD_MISMATCH")
	}
	record.RunnerID = proof.RunnerID
	if err := localRunnerMatches(state.Directory, record); err != nil {
		return quickPending()
	}
	record.State = "registered"
	if err := writeRunnerRecord(state.Directory, record); err != nil {
		return quickPending()
	}
	state.Phase, state.RunnerID = "confirmed", proof.RunnerID
	if err := quickWrite(filepath.Join(o.directory, quickStateFile), state); err != nil {
		return quickPending()
	}
	return quickOutput(out, "runner_registered", "finish", "")
}

func quickRetireManagementSecret(directory string) error {
	path := filepath.Join(directory, quickHandoffFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &CommandError{Code: "QUICK_SECRET_CLEANUP_INCOMPLETE", Exit: 2}
	}
	return nil
}

func quickExclusiveMemberLabel(runner RunnerInfo, member int64) bool {
	for _, label := range runner.Labels {
		lower := strings.ToLower(label.Name)
		if strings.HasPrefix(lower, "nibfin-user-") && lower != memberLabel(member) {
			return false
		}
	}
	return true
}

// An explicit receive/abort may retire a designated private input even when a
// prior invocation has already advanced the attempt. Never follow links or
// delete a caller-selected ordinary state file.
func quickInputPresent(path string) (bool, error) {
	if filepath.Base(path) != quickHandoffFile || !filepath.IsAbs(path) ||
		hasGitAncestor(filepath.Dir(path)) {
		return false, errors.New("DESIGNATED_HANDOFF_FILE_REQUIRED")
	}
	stat, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !stat.Mode().IsRegular() {
		return false, errors.New("UNTRUSTED_QUICK_FILE")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != filepath.Clean(path) {
		return false, errors.New("UNTRUSTED_QUICK_FILE")
	}
	if err := requirePrivateQuickPaths([]string{filepath.Dir(path), path}); err != nil {
		return false, err
	}
	return true, nil
}

func quickRetireInput(path string) error {
	present, err := quickInputPresent(path)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	if os.Remove(path) != nil {
		return &CommandError{Code: "QUICK_SECRET_CLEANUP_INCOMPLETE", Exit: 2}
	}
	return nil
}

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func eligibleQuickMember(api API, repo Repository, login string, id int64) error {
	if !quickLogin.MatchString(login) {
		return errors.New("INVALID_HANDOFF_MEMBER")
	}
	var user User
	if err := api.Call("GET", "users/"+login, nil, &user); err != nil {
		return errors.New("HANDOFF_MEMBER_IDENTITY_UNAVAILABLE")
	}
	if user.ID != id || user.Type != "User" || !strings.EqualFold(user.Login, login) {
		return errors.New("HANDOFF_MEMBER_IDENTITY_MISMATCH")
	}
	var permission struct {
		Permission string `json:"permission"`
		User       User   `json:"user"`
	}
	path := "repos/" + repo.FullName + "/collaborators/" + login + "/permission"
	if err := api.Call("GET", path, nil, &permission); err != nil {
		return errors.New("HANDOFF_MEMBER_ELIGIBILITY_UNAVAILABLE")
	}
	if permission.User.ID != id || permission.User.Type != "User" ||
		!strings.EqualFold(permission.User.Login, login) ||
		(permission.Permission != "write" && permission.Permission != "admin" &&
			permission.Permission != "maintain") {
		return errors.New("HANDOFF_MEMBER_NOT_ELIGIBLE")
	}
	return nil
}

func (a App) quickIssue(repo Repository, directory string, q quickFlags,
	now time.Time, out io.Writer) error {
	data, err := readQuick(*q.request, false)
	if err != nil {
		return err
	}
	var request quickRequest
	if decodeQuick(data, &request) != nil || !validQuickRequest(request) ||
		request.RepositoryID != repo.ID || request.Repository != repo.FullName ||
		!strings.EqualFold(request.Login, *q.member) {
		return errors.New("HANDOFF_REQUEST_IDENTITY_MISMATCH")
	}
	if err := eligibleQuickMember(a.API, repo, *q.member, request.MemberID); err != nil {
		return err
	}
	secretPath, err := quickFilePath(*q.secret)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(secretPath); !errors.Is(err, os.ErrNotExist) {
		return errors.New("NEW_HANDOFF_FILE_REQUIRED")
	}
	var applications []RunnerApplication
	if err := a.API.Call("GET", "repos/"+repo.FullName+"/actions/runners/downloads",
		nil, &applications); err != nil {
		return errors.New("OFFICIAL_RUNNER_SOURCE_UNAVAILABLE")
	}
	app, err := selectRunnerApplication(applications, request.Platform,
		request.Architecture)
	if err != nil {
		return err
	}
	runners, err := listRunners(a.API, repo.FullName)
	if err != nil {
		return errors.New("HANDOFF_MANAGEMENT_READBACK_UNAVAILABLE")
	}
	for _, runner := range runners {
		if runner.Name == request.Name {
			return errors.New("RUNNER_NAME_ALREADY_EXISTS")
		}
	}
	if err := createQuickDirectory(directory); err != nil {
		return err
	}
	state := quickState{Request: request, App: app, Stage: "issuance-pending"}
	return quickLocked(directory, out, func() error {
		return a.quickIssueToken(repo, directory, secretPath, state, now, out)
	})
}

func (a App) quickIssueToken(repo Repository, directory, secretPath string,
	state quickState, now time.Time, out io.Writer) error {
	if err := saveQuick(directory, state, true); err != nil {
		return err
	}
	token, err := requestRunnerToken(a.API, repo.FullName, "registration-token", now)
	if err != nil {
		return quickPending(out, "HANDOFF_ISSUANCE_UNKNOWN_NO_AUTOMATIC_RETRY")
	}
	if err := quickTokenLifetime(token, now); err != nil {
		return quickPending(out, "HANDOFF_PROVIDER_LIFETIME_INSUFFICIENT")
	}
	capability := quickCapability{Kind: "nibfin-registration-v1", Request: state.Request,
		App: state.App, Token: token}
	digest, err := writeQuick(secretPath, capability, true)
	if err != nil {
		return quickPending(out, "HANDOFF_DELIVERY_UNKNOWN_NO_AUTOMATIC_RETRY")
	}
	state.Stage, state.Expires = "issued", token.Expires
	if err := saveQuick(directory, state, false); err != nil {
		if os.Remove(secretPath) != nil {
			return quickPending(out, "HANDOFF_SECRET_CLEANUP_INCOMPLETE")
		}
		return quickPending(out, "HANDOFF_ISSUANCE_STATE_UNRESOLVED")
	}
	// This digest is safe to disclose, but only a separately authenticated
	// management channel makes it an origin check. It is never a bearer token.
	err = quickOutput(out, "issued", "Authenticate this digest separately: "+digest)
	if err != nil {
		if os.Remove(secretPath) != nil {
			return quickPending(out, "HANDOFF_SECRET_CLEANUP_INCOMPLETE")
		}
		return quickPending(out, "HANDOFF_OUTPUT_UNAVAILABLE_NO_AUTOMATIC_RETRY")
	}
	return nil
}

func (a App) quickConfirm(repo Repository, directory string, state quickState,
	q quickFlags, out io.Writer) error {
	if state.Directory != "" || (state.Stage != "issued" &&
		state.Stage != "issuance-pending" && state.Stage != "observed") {
		return errors.New("MANAGEMENT_ATTEMPT_REQUIRED")
	}
	if err := eligibleQuickMember(a.API, repo, state.Request.Login,
		state.Request.MemberID); err != nil {
		return err
	}
	data, err := readQuick(*q.result, false)
	if err != nil {
		return err
	}
	var result quickResult
	if decodeQuick(data, &result) != nil || result.Kind != "local-observation-v1" ||
		result.Request != state.Request || result.RunnerID <= 0 ||
		state.RunnerID > 0 && result.RunnerID != state.RunnerID {
		return errors.New("HANDOFF_LOCAL_OBSERVATION_MISMATCH")
	}
	// The recipient observation is only a claim. Management independently
	// enumerates the repository before constructing a confirmation.
	runners, err := listRunners(a.API, repo.FullName)
	if err != nil {
		return quickPending(out, "HANDOFF_MANAGEMENT_READBACK_UNAVAILABLE")
	}
	var matches []RunnerInfo
	for _, runner := range runners {
		if runner.Name == state.Request.Name {
			matches = append(matches, runner)
		}
	}
	if len(matches) != 1 || matches[0].ID != result.RunnerID ||
		!hasMemberLabel(matches[0], state.Request.MemberID) {
		return quickPending(out, "HANDOFF_NATIVE_RESULT_MISSING_OR_MISMATCHED")
	}
	record := quickInstall(state)
	record.RunnerID = result.RunnerID
	if _, err := ownedRunner(a.API, repo, User{ID: state.Request.MemberID},
		record); err != nil {
		return quickPending(out, "HANDOFF_NATIVE_IDENTITY_UNCONFIRMED")
	}
	result.Kind = "management-observation-v1"
	digest, err := writeQuick(*q.confirmation, result, true)
	if err != nil {
		return err
	}
	state.Stage, state.RunnerID = "observed", result.RunnerID
	if err := saveQuick(directory, state, false); err != nil {
		return quickPending(out, "HANDOFF_CONFIRMATION_STATE_UNRESOLVED")
	}
	return quickOutput(out, "observed", fmt.Sprintf(
		"Authenticate confirmation digest separately: %s; local reconciliation required",
		digest))
}

func quickTokenLifetime(token runnerToken, now time.Time) error {
	expires, err := time.Parse(time.RFC3339, token.Expires)
	if err != nil || token.Value == "" || len(token.Value) > 16384 ||
		!expires.After(now.Add(runnerConfigurationTimeout+30*time.Second)) {
		return errors.New("HANDOFF_TOKEN_EXPIRED_OR_INSUFFICIENT_LIFETIME")
	}
	return nil
}

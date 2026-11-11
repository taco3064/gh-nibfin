package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

var quickLoginPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

type quickOptions struct {
	command, repo, directory, runner, name   string
	request, handoff, approval, confirmation string
	apply                                    bool
}

func (a App) runQuickRunner(args []string, out io.Writer) (err error) {
	var o quickOptions
	f := flag.NewFlagSet("quick-runner", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.command, "quick-runner", "", "request|issue|receive|confirm|finish|abort")
	f.StringVar(&o.repo, "repo", "", "Public repository")
	f.StringVar(&o.directory, "quick-directory", "", "Private attempt directory")
	f.StringVar(&o.runner, "runner-directory", "", "New private Runner directory")
	f.StringVar(&o.name, "runner-name", "", "Unique Runner name")
	f.StringVar(&o.request, "request", "", "Recipient request file")
	f.StringVar(&o.handoff, "handoff", "", "Private consumable token handoff file")
	f.StringVar(&o.approval, "approval", "", "Digest independently received from Admin")
	f.StringVar(&o.confirmation, "confirmation", "", "Management read-back file")
	f.BoolVar(&o.apply, "apply", false, "Perform this operation")
	if f.Parse(args) != nil || f.NArg() != 0 || !o.valid() {
		return errors.New("INVALID_QUICK_RUNNER_ARGUMENTS")
	}
	repo, user, err := repository(a.API, o.repo,
		o.command == "issue" || o.command == "confirm")
	if err != nil {
		return err
	}
	directory, err := runnerDirectory(o.directory)
	if err != nil {
		return err
	}
	o.directory = directory
	var state quickState
	switch o.command {
	case "request":
		path, err := runnerDirectory(o.runner)
		if err != nil {
			return err
		}
		if path == directory {
			return errors.New("SEPARATE_RUNNER_DIRECTORY_REQUIRED")
		}
		if err := requirePrivateRunnerParent(path); err != nil {
			return err
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("NEW_RUNNER_DIRECTORY_REQUIRED")
		}
		state = quickState{Role: "member", Phase: "awaiting", Directory: path,
			Request: quickRequest{Repository: repo.FullName, RepositoryID: repo.ID,
				Member: user, Name: o.name, OS: runtime.GOOS, Arch: runtime.GOARCH}}
	case "issue":
		if _, err := quickRead(o.request, &state.Request); err != nil {
			return err
		}
		if err := quickValidateRequest(state.Request, repo); err != nil {
			return err
		}
		if err := quickEligible(a.API, repo, state.Request.Member); err != nil {
			return err
		}
		state.Role, state.Phase = "management", "issue-pending"
	default:
		if _, err := quickRead(filepath.Join(directory, quickStateFile), &state); err != nil {
			return err
		}
		if err := a.quickValidateState(o, state, repo, user); err != nil {
			return err
		}
	}
	if err := quickOutput(out, "preview", o.command, ""); err != nil {
		return err
	}
	if !o.apply {
		return nil
	}
	if o.command == "request" || o.command == "issue" {
		if err := quickNewDirectory(directory); err != nil {
			return err
		}
		if o.command == "request" {
			state.Request.Attempt, err = quickNonce()
			if err != nil {
				return err
			}
		}
		if err := quickWrite(filepath.Join(directory, quickStateFile), state); err != nil {
			return err
		}
	} else {
		lock := filepath.Join(directory, ".quick.lock")
		file, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("QUICK_LOCKED_INSPECT_BEFORE_RETRY")
		}
		if file.Close() != nil {
			return errors.New("QUICK_LOCK_UNRESOLVED")
		}
		defer func() {
			if os.Remove(lock) != nil {
				err = &CommandError{Code: "QUICK_LOCK_CLEANUP_INCOMPLETE", Exit: 2}
			}
		}()
		// Re-read under the lock; a competing invocation may have advanced the attempt.
		if _, err := quickRead(filepath.Join(directory, quickStateFile), &state); err != nil {
			return err
		}
		if err := a.quickValidateState(o, state, repo, user); err != nil {
			return err
		}
	}
	switch o.command {
	case "request":
		return quickWrite(filepath.Join(directory, quickRequestFile), state.Request)
	case "issue":
		return a.quickIssue(o, state, repo, out)
	case "receive":
		return a.quickReceive(o, state, repo, user, out)
	case "confirm":
		return a.quickConfirm(o, state, repo, out)
	case "finish":
		return a.quickFinish(o, state, repo, user, out)
	case "abort":
		// No provider revocation is claimed; management uncertainty is retained.
		if state.Phase == "confirmed" {
			return errors.New("QUICK_ALREADY_CONFIRMED")
		}
		if state.Role == "management" {
			if err := quickRetireManagementSecret(directory); err != nil {
				return err
			}
		}
		if o.handoff != "" {
			if err := quickRetireInput(o.handoff); err != nil {
				return err
			}
		}
		state.Phase = "aborted-pending"
		if err := quickWrite(filepath.Join(directory, quickStateFile), state); err != nil {
			return err
		}
		return quickOutput(out, "pending", o.command, "")
	}
	return errors.New("INVALID_QUICK_COMMAND")
}

func (o quickOptions) valid() bool {
	if o.repo == "" || o.directory == "" {
		return false
	}
	switch o.command {
	case "request":
		return o.runner != "" && runnerNamePattern.MatchString(o.name) &&
			o.request == "" && o.handoff == "" && o.approval == "" && o.confirmation == ""
	case "issue":
		return o.request != "" && o.runner == "" && o.name == "" &&
			o.handoff == "" && o.approval == "" && o.confirmation == ""
	case "receive":
		return o.handoff != "" && digestPattern.MatchString(o.approval) &&
			o.runner == "" && o.name == "" && o.request == "" && o.confirmation == ""
	case "finish":
		return o.confirmation != "" && digestPattern.MatchString(o.approval) &&
			o.runner == "" && o.name == "" && o.request == "" && o.handoff == ""
	case "abort":
		return o.runner == "" && o.name == "" && o.request == "" &&
			o.approval == "" && o.confirmation == ""
	case "confirm":
		return o.runner == "" && o.name == "" && o.request == "" &&
			o.handoff == "" && o.approval == "" && o.confirmation == ""
	}
	return false
}

func quickOutput(out io.Writer, status, operation, approval string) error {
	result := map[string]string{"status": status, "operation": operation,
		"core_binding": "unverified", "protected_execution": "unverified",
		"host_adoption": "required"}
	if approval != "" {
		result["approval"] = approval
	}
	if json.NewEncoder(out).Encode(result) != nil {
		if status != "preview" {
			return &CommandError{Code: "QUICK_OUTPUT_UNAVAILABLE_INSPECT_ATTEMPT", Exit: 2}
		}
		return errors.New("QUICK_OUTPUT_UNAVAILABLE_INSPECT_ATTEMPT")
	}
	return nil
}

func quickEligible(api API, repo Repository, member User) error {
	if !quickLoginPattern.MatchString(member.Login) {
		return errors.New("INVALID_QUICK_MEMBER")
	}
	var observed User
	if api.Call("GET", "users/"+member.Login, nil, &observed) != nil ||
		observed != member || member.Type != "User" {
		return errors.New("QUICK_MEMBER_IDENTITY_MISMATCH")
	}
	var permission struct {
		Permission string `json:"permission"`
		User       User   `json:"user"`
	}
	path := "repos/" + repo.FullName + "/collaborators/" + member.Login + "/permission"
	if api.Call("GET", path, nil, &permission) != nil || permission.User != member ||
		(permission.Permission != "write" && permission.Permission != "maintain" &&
			permission.Permission != "admin") {
		return errors.New("QUICK_MEMBER_ELIGIBILITY_UNCONFIRMED")
	}
	return nil
}

func (a App) quickNow() time.Time {
	if a.Clock != nil {
		return a.Clock()
	}
	return time.Now()
}

func (a App) quickValidateState(o quickOptions, state quickState,
	repo Repository, user User) error {
	if err := quickValidateRequest(state.Request, repo); err != nil {
		return err
	}
	if o.command == "confirm" || o.command == "abort" && state.Role == "management" {
		if state.Role != "management" || !repo.Permissions["admin"] {
			return errors.New("MANAGEMENT_ATTEMPT_AND_ADMIN_REQUIRED")
		}
		if o.command == "confirm" {
			return quickEligible(a.API, repo, state.Request.Member)
		}
		// Revocation must not prevent the authorized manager retiring a token copy.
		return nil
	}
	if state.Role != "member" || state.Request.Member != user {
		return errors.New("QUICK_AUTHENTICATED_RECIPIENT_MISMATCH")
	}
	if o.command == "abort" {
		return nil
	}
	directory, err := runnerDirectory(state.Directory)
	if err != nil {
		return err
	}
	if directory != state.Directory || directory == o.directory {
		return errors.New("QUICK_LOCAL_DIRECTORY_MISMATCH")
	}
	return requirePrivateRunnerParent(directory)
}

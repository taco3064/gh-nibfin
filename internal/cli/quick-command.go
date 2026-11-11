package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type quickFlags struct {
	set                                   *flag.FlagSet
	operation, directory, request, member *string
	secret, result, confirmation, origin  *string
}

func addQuickFlags(flags *flag.FlagSet) quickFlags {
	return quickFlags{set: flags,
		operation:    flags.String("quick", "", "Operator-mediated member registration step"),
		directory:    flags.String("handoff-directory", "", "Private attempt state directory"),
		request:      flags.String("request", "", "Member request file (nonsecret)"),
		member:       flags.String("member", "", "Intended member login; management only"),
		secret:       flags.String("secret-file", "", "Private one-use capability file"),
		result:       flags.String("result", "", "Member local observation (nonsecret)"),
		confirmation: flags.String("confirmation", "", "Management read-back file"),
		origin:       flags.String("trusted-digest", "", "Authenticated management digest"),
	}
}

func (q quickFlags) used() bool {
	used := false
	q.set.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "quick", "handoff-directory", "request", "member", "secret-file",
			"result", "confirmation", "trusted-digest":
			used = true
		}
	})
	return used
}

func (q quickFlags) validate(path, name string) error {
	allowed := map[string]string{
		"request":   "runner-directory runner-name",
		"issue":     "request member secret-file",
		"register":  "secret-file trusted-digest",
		"confirm":   "result confirmation",
		"reconcile": "confirmation trusted-digest",
		"inspect":   "", "cancel": "",
	}
	rules, exists := allowed[*q.operation]
	if !exists || *q.directory == "" {
		return errors.New("INVALID_QUICK_OPERATION")
	}
	values := map[string]string{"runner-directory": path, "runner-name": name,
		"request": *q.request, "member": *q.member, "secret-file": *q.secret,
		"result": *q.result, "confirmation": *q.confirmation,
		"trusted-digest": *q.origin}
	seen := map[string]bool{}
	q.set.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	for option, value := range values {
		if *q.operation == "cancel" && option == "secret-file" {
			continue
		}
		required := false
		for _, field := range strings.Fields(rules) {
			required = required || field == option
		}
		if required != (value != "") || !required && seen[option] {
			return errors.New("CONFLICTING_OR_MISSING_QUICK_OPTIONS")
		}
	}
	if *q.origin != "" && !digestPattern.MatchString(*q.origin) ||
		*q.member != "" && !quickLogin.MatchString(*q.member) {
		return errors.New("INVALID_QUICK_IDENTITY_OR_ORIGIN")
	}
	if *q.operation == "request" &&
		(!runnerNamePattern.MatchString(name) || len(name) > 31) {
		return errors.New("INVALID_RUNNER_NAME_PREFIX_MAX_31_CHARACTERS")
	}
	for _, file := range []string{*q.request, *q.secret, *q.result, *q.confirmation} {
		if file != "" {
			if _, err := quickFilePath(file); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a App) runQuick(name, runnerPath, runnerName string, apply bool,
	q quickFlags, out io.Writer) error {
	if err := q.validate(runnerPath, runnerName); err != nil {
		return err
	}
	directory, err := runnerDirectory(*q.directory)
	if err != nil {
		return err
	}
	if err := requirePrivateRunnerParent(directory); err != nil {
		return err
	}
	if *q.operation == "request" {
		if _, err := runnerDirectory(runnerPath); err != nil {
			return err
		}
	}
	management := *q.operation == "issue" || *q.operation == "confirm"
	repo, user, err := repository(a.API, name, management)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	if err := encoder.Encode(map[string]any{"status": "preview", "quick": *q.operation,
		"repository": repo.FullName, "authenticated_user": user.ID,
		"core_binding": "unverified", "host_adoption": "required"}); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if !apply {
		return nil
	}
	clock := a.Clock
	if clock == nil {
		clock = time.Now
	}
	if *q.operation == "request" {
		return quickRequestMember(repo, user, directory, runnerPath, runnerName, out)
	}
	if *q.operation == "issue" {
		return a.quickIssue(repo, directory, q, clock(), out)
	}
	return quickLocked(directory, out, func() error {
		state, err := loadQuick(directory)
		if err != nil {
			return quickPending(out, "HANDOFF_STATE_UNRESOLVED_INSPECT_NO_RETRY")
		}
		if !validQuickRequest(state.Request) || state.Request.RepositoryID != repo.ID ||
			state.Request.Repository != repo.FullName ||
			!management && (state.Request.MemberID != user.ID ||
				state.Request.Login != user.Login) {
			return errors.New("HANDOFF_AUTHENTICATED_IDENTITY_MISMATCH")
		}
		switch *q.operation {
		case "register":
			return a.quickRegister(repo, user, directory, state, q, clock, out)
		case "confirm":
			return a.quickConfirm(repo, directory, state, q, out)
		case "reconcile":
			return quickReconcile(repo, user, directory, state, q, out)
		case "inspect":
			return quickInspect(directory, state, out)
		case "cancel":
			if *q.secret != "" {
				data, err := readQuick(*q.secret, true)
				clear(data)
				if err != nil {
					return err
				}
			}
			if state.Stage != "requested" {
				return quickPending(out, "REGISTRATION_EFFECTS_REQUIRE_RECONCILIATION")
			}
			state.Stage = "cancelled"
			if err := saveQuick(directory, state, false); err != nil {
				return err
			}
			return quickOutput(out, "cancelled", "Disclosed tokens remain valid until expiry")
		}
		return errors.New("INVALID_QUICK_OPERATION")
	})
}

func quickRequestMember(repo Repository, user User, directory, path, name string,
	out io.Writer) error {
	path, err := runnerDirectory(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) ||
		path == directory || filepath.Dir(directory) == path {
		return errors.New("NEW_SEPARATE_RUNNER_DIRECTORY_REQUIRED")
	}
	attempt, err := newQuickAttempt()
	if err != nil {
		return err
	}
	request := quickRequest{Version: 1, Attempt: attempt, Repository: repo.FullName,
		RepositoryID: repo.ID, MemberID: user.ID, Login: user.Login,
		Name:     name + "-" + attempt[:32],
		Platform: runtime.GOOS, Architecture: runtime.GOARCH}
	if err := createQuickDirectory(directory); err != nil {
		return err
	}
	return quickLocked(directory, out, func() error {
		state := quickState{Request: request, Directory: path, Stage: "requested"}
		if err := saveQuick(directory, state, true); err != nil {
			return err
		}
		_, err = writeQuick(filepath.Join(directory, "request.json"), request, true)
		if err != nil {
			return err
		}
		return quickOutput(out, "requested", "Send request.json to the Repository Admin")
	})
}

func quickOutput(out io.Writer, status, next string) error {
	return json.NewEncoder(out).Encode(map[string]any{"status": status, "next": next,
		"core_binding": "unverified", "protected_execution": "unverified",
		"host_adoption": "required"})
}

func quickPending(out io.Writer, code string) error {
	_ = quickOutput(out, "pending", code)
	return &CommandError{Code: code, Exit: 2}
}

package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var runnerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

const runnerManagementLock = ".nibfin-management.lock"

func (a App) runRunner(args []string, out io.Writer) (err error) {
	if len(args) == 0 {
		return errors.New("RUNNER_COMMAND_REQUIRED")
	}
	command := args[0]
	if command != "create" && command != "register" && command != "list" &&
		command != "unregister" && command != "remove" {
		return errors.New("UNKNOWN_RUNNER_COMMAND")
	}
	flags := flag.NewFlagSet("runner", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "Public OWNER/REPO")
	directory := flags.String("directory", "", "Absolute private Runner directory")
	runnerName := flags.String("name", "", "Runner name for creation")
	apply := flags.Bool("apply", false, "Perform the specified management operation")
	shared := flags.Bool("shared", false, "Register an offline shared Runner slot")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *name == "" {
		return errors.New("INVALID_RUNNER_ARGUMENTS")
	}
	if *shared && (command != "register" || !nativeSharedHostSupported()) {
		return errors.New("SHARED_RUNNER_REGISTRATION_REQUIRES_LINUX_X64")
	}
	repo, user, err := repository(a.API, *name, true)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	if command == "list" {
		if *apply || *directory != "" || *runnerName != "" {
			return errors.New("INVALID_RUNNER_LIST_ARGUMENTS")
		}
		runners, err := listRunners(a.API, repo.FullName)
		if err != nil {
			return err
		}
		return encoder.Encode(map[string]any{"status": "completed", "runners": runners,
			"repository": repo.FullName, "core_binding": "unverified"})
	}
	path, err := runnerDirectory(*directory)
	if err != nil {
		return err
	}
	var record RunnerInstall
	if command == "create" {
		if !runnerNamePattern.MatchString(*runnerName) {
			return errors.New("INVALID_RUNNER_NAME")
		}
		app, err := runnerApplication(a.API, repo.FullName)
		if err != nil {
			return err
		}
		record = RunnerInstall{Repository: repo.FullName, RepositoryID: repo.ID,
			MemberID: user.ID, Name: *runnerName, Application: app, State: "created"}
	} else {
		if *runnerName != "" {
			return errors.New("RUNNER_NAME_COMES_FROM_MANAGED_INSTALLATION")
		}
		record, err = readRunnerRecord(path, repo, user)
		if err != nil {
			return err
		}
	}
	if err := encoder.Encode(map[string]any{"status": "preview", "command": command,
		"directory": path, "record": record, "member_label": memberLabel(user.ID),
		"core_binding": "unverified"}); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if !*apply {
		return nil
	}
	if command != "create" {
		lockPath := filepath.Join(path, runnerManagementLock)
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("RUNNER_MANAGEMENT_IN_PROGRESS_INSPECT_BEFORE_RETRY")
		}
		if lock.Close() != nil {
			return errors.New("RUNNER_MANAGEMENT_LOCK_UNRESOLVED")
		}
		defer func() {
			if cleanupErr := os.Remove(lockPath); cleanupErr != nil {
				err = errors.Join(err, &CommandError{
					Code: "RUNNER_MANAGEMENT_LOCK_CLEANUP_INCOMPLETE", Exit: 2})
			}
		}()
		record, err = readRunnerRecord(path, repo, user)
		if err != nil {
			return err
		}
	}
	clock := a.Clock
	if clock == nil {
		clock = time.Now
	}
	switch command {
	case "create":
		record, err = createRunner(a.API, a.Download, repo, user, *runnerName, path)
	case "register":
		if *shared {
			record, err = a.registerSharedSlot(repo, user, path, record, clock())
		} else {
			record, err = registerRunner(a.API, a.Runner, repo, user, path, record, clock())
		}
	case "unregister":
		record, err = unregisterRunner(a.API, a.Runner, repo, user, path, record, clock())
	case "remove":
		record, err = removeRunner(a.API, repo, user, path, record)
	}
	if err != nil {
		status, exit := "denied", 1
		if strings.HasSuffix(record.State, "-pending") ||
			strings.Contains(err.Error(), "CLEANUP_INCOMPLETE") {
			status, exit = "pending", 2
		}
		_ = encoder.Encode(map[string]any{"status": status, "record": record,
			"code": err.Error(), "core_binding": "unverified"})
		return &CommandError{Code: "RUNNER_OPERATION_INCOMPLETE_INSPECT_BEFORE_RETRY",
			Exit: exit}
	}
	return encoder.Encode(map[string]any{"status": "completed", "command": command,
		"record": record, "core_binding": "unverified", "service_changes": "none"})
}

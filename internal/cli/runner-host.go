package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type RunnerHost interface {
	Check(string) error
	Configure(string, string, map[string]string) error
}

type NativeRunnerHost struct{}

const runnerConfigurationTimeout = 2 * time.Minute

func runnerEntrypoint(directory string) string {
	entry := "Runner.Listener"
	if runtime.GOOS == "windows" {
		entry += ".exe"
	}
	return filepath.Join(directory, "bin", entry)
}

func (NativeRunnerHost) Check(directory string) error {
	if err := rejectRunnerOverrides(directory); err != nil {
		return err
	}
	// Preserve the official config.sh non-root guard without invoking a shell.
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		return errors.New("OFFICIAL_RUNNER_REQUIRES_NONROOT_CONFIGURATION")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, runnerEntrypoint(directory), "--version")
	cmd.Dir, cmd.Env = directory, runnerEnvironment(nil)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if cmd.Run() != nil {
		return errors.New("OFFICIAL_RUNNER_RUNTIME_UNAVAILABLE")
	}
	return nil
}

func runnerEnvironment(inputs map[string]string) []string {
	var env []string
	for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "HOME",
		"USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	path := "/usr/bin:/bin"
	if runtime.GOOS == "windows" {
		path = filepath.Join(os.Getenv("SystemRoot"), "System32")
	}
	env = append(env, "PATH="+path)
	keys := []string{"TOKEN", "URL", "NAME", "LABELS", "WORK", "UNATTENDED", "DISABLEUPDATE"}
	for _, key := range keys {
		if value, ok := inputs[key]; ok {
			env = append(env, "ACTIONS_RUNNER_INPUT_"+key+"="+value)
		}
	}
	return env
}

func (NativeRunnerHost) Configure(directory, command string,
	inputs map[string]string) error {
	if err := rejectRunnerOverrides(directory); err != nil {
		return err
	}
	if command != "configure" && command != "remove" && command != "remove-local" ||
		command != "remove-local" && inputs["TOKEN"] == "" || inputs["UNATTENDED"] != "true" {
		return errors.New("INVALID_OFFICIAL_RUNNER_COMMAND")
	}
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		return errors.New("OFFICIAL_RUNNER_REQUIRES_NONROOT_CONFIGURATION")
	}
	ctx, cancel := context.WithTimeout(context.Background(), runnerConfigurationTimeout)
	defer cancel()
	args := []string{command}
	if command == "remove-local" {
		args = []string{"remove", "--local"}
	}
	cmd := exec.CommandContext(ctx, runnerEntrypoint(directory), args...)
	cmd.Dir, cmd.Env = directory, runnerEnvironment(inputs)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	err := cmd.Run()
	if command == "configure" && inputs["DISABLEUPDATE"] == "true" {
		if readbackErr := protectSharedRegistrationFiles(directory); readbackErr != nil {
			return readbackErr
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("RUNNER_TIMEOUT_RESULT_UNKNOWN")
		}
		return errors.New("RUNNER_CONFIGURATION_FAILED_READ_BACK_REQUIRED")
	}
	return nil
}

func protectSharedRegistrationFiles(directory string) error {
	for _, name := range []string{".runner", ".credentials", ".credentials_rsaparams"} {
		path := filepath.Join(directory, name)
		stat, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !stat.Mode().IsRegular() || os.Chmod(path, 0600) != nil {
			return errors.New("SHARED_RUNNER_REGISTRATION_PERMISSIONS_UNVERIFIED")
		}
	}
	return nil
}

func hasMemberLabel(runner RunnerInfo, member int64) bool {
	want := memberLabel(member)
	found := false
	for _, label := range runner.Labels {
		if strings.EqualFold(label.Name, want) {
			found = true
		} else if strings.HasPrefix(strings.ToLower(label.Name), "nibfin-user-") {
			return false
		}
	}
	return found
}

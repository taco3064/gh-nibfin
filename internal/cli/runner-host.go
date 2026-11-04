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
	Configure(string, string, map[string]string) error
}

type NativeRunnerHost struct{}

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
	for _, key := range []string{"TOKEN", "URL", "NAME", "LABELS", "WORK", "UNATTENDED"} {
		if value, ok := inputs[key]; ok {
			env = append(env, "ACTIONS_RUNNER_INPUT_"+key+"="+value)
		}
	}
	return env
}

func (NativeRunnerHost) Configure(directory, command string,
	inputs map[string]string) error {
	if command != "configure" && command != "remove" && command != "remove-local" ||
		command != "remove-local" && inputs["TOKEN"] == "" || inputs["UNATTENDED"] != "true" {
		return errors.New("INVALID_OFFICIAL_RUNNER_COMMAND")
	}
	entry := "Runner.Listener"
	if runtime.GOOS == "windows" {
		entry += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	args := []string{command}
	if command == "remove-local" {
		args = []string{"remove", "--local"}
	}
	cmd := exec.CommandContext(ctx, filepath.Join(directory, "bin", entry), args...)
	cmd.Dir, cmd.Env = directory, runnerEnvironment(inputs)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return errors.New("RUNNER_TIMEOUT_RESULT_UNKNOWN")
		}
		return errors.New("RUNNER_CONFIGURATION_FAILED_READ_BACK_REQUIRED")
	}
	return nil
}

func hasMemberLabel(runner RunnerInfo, member int64) bool {
	want := memberLabel(member)
	for _, label := range runner.Labels {
		if strings.EqualFold(label.Name, want) {
			return true
		}
	}
	return false
}

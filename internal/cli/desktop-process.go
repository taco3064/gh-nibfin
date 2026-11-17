package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
)

// DesktopTerminal is deliberately separate from buffered Process execution.
// The caller must obtain consent before invoking it.
type DesktopTerminal interface {
	EnterGuest(context.Context, string, []string, io.Writer) error
}

func desktopToolPath(name string) (string, error) {
	if name == "wsl.exe" && runtime.GOOS == "windows" {
		root := os.Getenv("SystemRoot")
		if !filepath.IsAbs(root) {
			return "", errors.New("WSL_TOOL_UNAVAILABLE")
		}
		path := filepath.Join(root, "System32", "wsl.exe")
		stat, err := os.Lstat(path)
		if err != nil || !stat.Mode().IsRegular() {
			return "", errors.New("WSL_TOOL_UNAVAILABLE")
		}
		return path, nil
	}
	if name == "multipass" && runtime.GOOS == "darwin" {
		// The official macOS package installs this entry, outside repository PATHs.
		return toolPath("/usr/local/bin/multipass")
	}
	return "", errors.New("DESKTOP_PROVIDER_UNAVAILABLE")
}

func desktopEnvironment() []string {
	var env []string
	// In particular, exclude tokens, WSLENV and desktop login directories.
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func (NativeProcess) DesktopQuery(name string, args []string) ([]byte, error) {
	path, err := desktopToolPath(name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), desktopQueryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = desktopEnvironment()
	var out, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	if out.Len() > desktopOutputLimit {
		return nil, inventoryError()
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 2 {
			return out.Bytes(), &CommandError{Code: "GUEST_EXIT_2", Exit: 2}
		}
		return nil, errors.New("DESKTOP_QUERY_UNAVAILABLE")
	}
	return out.Bytes(), nil
}

func (NativeProcess) EnterGuest(ctx context.Context, name string,
	args []string, out io.Writer) error {
	path, err := desktopToolPath(name)
	if err != nil {
		return err
	}
	// init does not otherwise install a process-wide signal handler. Keep signal
	// cancellation scoped to interactive delegation and restore it on return.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = desktopEnvironment()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, out, out
	if cmd.Run() != nil {
		return errors.New("GUEST_DISCONNECTED_READ_BACK_REQUIRED")
	}
	return nil
}

func desktopQuery(p Process, tool string, args ...string) ([]byte, error) {
	if query, ok := p.(interface {
		DesktopQuery(string, []string) ([]byte, error)
	}); ok {
		return query.DesktopQuery(tool, args)
	}
	return p.Run(tool, args, nil)
}

func guestCommand(t desktopTarget, command ...string) (string, []string) {
	if t.Provider == "wsl" {
		args := []string{"--distribution", t.Name, "--cd", "~", "--exec"}
		return "wsl.exe", append(args, command...)
	}
	args := []string{"exec", t.Name, "--"}
	return "multipass", append(args, command...)
}

func guestQuery(p Process, t desktopTarget, command ...string) ([]byte, error) {
	tool, args := guestCommand(t, command...)
	return desktopQuery(p, tool, args...)
}

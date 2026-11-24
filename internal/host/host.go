// Package host runs Podman commands where the Runners live: directly against
// the local Podman on Linux, or against the rootless Podman of the default
// `podman machine` VM's user on Windows and macOS.
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Errors returned by Detect and Start. Callers map them to their own codes
// with errors.Is.
var (
	ErrPodmanMissing  = errors.New("podman missing")
	ErrMachineMissing = errors.New("podman machine missing")
	ErrMachineStart   = errors.New("podman machine start failed")
	ErrUnsupported    = errors.New("unsupported host")
	ErrPodman         = errors.New("podman failed")
)

// InstallHint is where the user installs Podman.
const InstallHint = "https://podman.io/docs/installation"

// Process creation and platform lookups go through these hooks so tests need
// no Podman.
var (
	lookPath = exec.LookPath
	command  = exec.CommandContext
	goos     = runtime.GOOS
)

// tokenVars are removed from every child's environment so a GitHub token
// never reaches Podman or the containers it starts.
var tokenVars = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}

// Host is where the Runners' Podman commands run.
type Host struct {
	podman string
	// Machine is the podman machine name; empty for the local Linux host.
	Machine string
	// Running reports whether the machine is running; always true locally.
	Running bool
}

type machineEntry struct {
	Name    string
	Default bool
	Running bool
}

// Detect finds Podman and, on Windows and macOS, the default podman machine.
func Detect(ctx context.Context) (*Host, error) {
	switch goos {
	case "linux", "windows", "darwin":
	default:
		return nil, errorf(ErrUnsupported, "gh nibfin runs Runners on Linux, Windows or macOS, not %s", goos)
	}
	path, err := lookPath("podman")
	if err != nil {
		return nil, errorf(ErrPodmanMissing, "podman is not installed; install it from %s", InstallHint)
	}
	h := &Host{podman: path}
	if goos == "linux" {
		h.Running = true
		return h, nil
	}
	out, err := h.output(ctx, "machine", "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	var list []machineEntry
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, errorf(ErrPodman, "cannot read podman machine list: %v", err)
	}
	var m *machineEntry
	for i := range list {
		if list[i].Default {
			m = &list[i]
			break
		}
	}
	if m == nil && len(list) == 1 {
		m = &list[0]
	}
	switch {
	case len(list) == 0:
		return nil, errorf(ErrMachineMissing, "no podman machine exists; create one with `podman machine init`")
	case m == nil || m.Name == "":
		return nil, errorf(ErrMachineMissing, "no default podman machine among %d machines; set one with `podman system connection default <name>`", len(list))
	}
	h.Machine = m.Name
	h.Running = m.Running
	return h, nil
}

// NeedsStart reports whether the machine must be started first.
func (h *Host) NeedsStart() bool { return !h.Running }

// Start starts the machine when it is stopped.
func (h *Host) Start(ctx context.Context) error {
	if h.Running {
		return nil
	}
	var stderr bytes.Buffer
	c := h.raw(ctx, "machine", "start", h.Machine)
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return errorf(ErrMachineStart, "podman machine start %s: %s", h.Machine, message(err, &stderr))
	}
	h.Running = true
	return nil
}

// Podman returns a podman command that acts on the Runners' store. On a
// machine host it uses the machine's rootless connection, named exactly as
// the machine, even when the default connection is the machine's root one.
// The caller sets the child's stdin, stdout and stderr.
func (h *Host) Podman(ctx context.Context, args ...string) *exec.Cmd {
	if h.Machine != "" {
		args = append([]string{"--connection", h.Machine}, args...)
	}
	return h.raw(ctx, args...)
}

// Describe names the host for plans and results.
func (h *Host) Describe() string {
	if h.Machine == "" {
		return "local podman"
	}
	return "podman machine " + h.Machine
}

func (h *Host) raw(ctx context.Context, args ...string) *exec.Cmd {
	c := command(ctx, h.podman, args...)
	base := c.Env
	if base == nil {
		base = os.Environ()
	}
	c.Env = childEnv(base)
	return c
}

func (h *Host) output(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	c := h.raw(ctx, args...)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return nil, errorf(ErrPodman, "podman %s: %s", strings.Join(args, " "), message(err, &stderr))
	}
	return stdout.Bytes(), nil
}

// hostError carries one of the Err values above without repeating its text.
type hostError struct {
	kind error
	msg  string
}

func (e *hostError) Error() string { return e.msg }
func (e *hostError) Unwrap() error { return e.kind }

func errorf(kind error, format string, args ...any) error {
	return &hostError{kind, fmt.Sprintf(format, args...)}
}

func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
next:
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		for _, t := range tokenVars {
			// Windows environment names are case-insensitive.
			if strings.EqualFold(name, t) {
				continue next
			}
		}
		out = append(out, kv)
	}
	return out
}

func message(err error, stderr *bytes.Buffer) string {
	if s := strings.TrimSpace(stderr.String()); s != "" {
		return s
	}
	return err.Error()
}

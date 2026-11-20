package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// UpgradeDesktopTerminal preserves guest exit status separately from transport
// failure. Init/preflight retain their existing DesktopTerminal safety contract.
type UpgradeDesktopTerminal interface {
	EnterUpgradeGuest(context.Context, string, []string, io.Writer) error
}

// Upgrade queries use the command's cancellation, without a new deadline.
// Existing init/preflight query semantics remain unchanged.
type upgradeDesktopTransport struct {
	ctx     context.Context
	process Process
}

func (p upgradeDesktopTransport) Run(name string, args []string,
	input []byte) ([]byte, error) {
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	return p.process.Run(name, args, input)
}

func (p upgradeDesktopTransport) DesktopQuery(name string,
	args []string) ([]byte, error) {
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	if q, ok := p.process.(interface {
		DesktopReadContext(context.Context, []string, []byte, int) ([]byte, error)
	}); ok {
		return q.DesktopReadContext(p.ctx, args, nil, desktopOutputLimit)
	}
	if q, ok := p.process.(interface {
		RunContext(context.Context, string, []string, []byte) ([]byte, error)
	}); ok {
		return q.RunContext(p.ctx, name, args, nil)
	}
	return desktopQuery(p.process, name, args...)
}

// The installed CLI runs inside a fixed guest-side supervisor. Its exit travels
// in a separate control file descriptor (stdout); guest output uses stderr.
// Missing control output is transport loss, never proof of guest completion.
const upgradeExitScript = `import json, subprocess, sys
try:
    result = subprocess.run(sys.argv[1:], stdout=sys.stderr, stderr=sys.stderr)
    print(json.dumps({"guest_exit": result.returncode}), flush=True)
except OSError:
    print(json.dumps({"launch_failed": True}), flush=True)
`

func (NativeProcess) EnterUpgradeGuest(ctx context.Context, name string,
	args []string, out io.Writer) error {
	path, err := desktopToolPath(name)
	if err != nil {
		return err
	}
	return runResolvedUpgradeGuest(ctx, path, args, out)
}

func runResolvedUpgradeGuest(ctx context.Context, path string,
	args []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = desktopEnvironment()
	cmd.Stdin, cmd.Stderr = os.Stdin, out
	var control boundedBuffer
	cmd.Stdout = &control
	err := cmd.Start()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("GUEST_LAUNCH_FAILED")
	}
	// Wait also reports output-copy failures after the guest has executed.
	// Every post-launch failure requires read-back, even with an exit record.
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return errors.New("GUEST_TRANSPORT_LOST_READ_BACK_REQUIRED")
	}
	return decodeUpgradeExit(control.Bytes())
}

func upgradeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

func guestUpgradeExit(code int) error {
	if code == 0 {
		return nil
	}
	if code < 0 {
		return errors.New("GUEST_INTERRUPTED_READ_BACK_REQUIRED")
	}
	return &CommandError{Code: fmt.Sprintf("GUEST_EXIT_%d", code), Exit: code}
}

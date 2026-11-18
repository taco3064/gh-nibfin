package cli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
)

// Private desktop transport has operation-specific bounds and caller cancellation.
func (NativeProcess) DesktopReadContext(ctx context.Context, args []string,
	input []byte, limit int) ([]byte, error) {
	if len(input) > observationInputLimit || len(input) > 0 && !overviewPrivateArgs(args) {
		return nil, errors.New("GUEST_READ_UNAVAILABLE")
	}
	path, err := desktopToolPath("wsl.exe")
	if err != nil {
		return nil, err
	}
	return runResolvedDesktopRead(ctx, path, args, input, limit)
}

func runResolvedDesktopRead(ctx context.Context, path string, args []string,
	input []byte, limit int) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = desktopEnvironment()
	cmd.Stdin = bytes.NewReader(input)
	var out, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if out.Len() > limit || ctx.Err() != nil {
		return nil, errors.New("GUEST_READ_UNAVAILABLE")
	}
	if err != nil {
		var exit *exec.ExitError
		if len(input) > 0 && errors.As(err, &exit) && exit.ExitCode() == 1 {
			if data, ok := observationDenial(out.Bytes()); ok {
				return data, nil
			}
		}
		return nil, errors.New("GUEST_READ_UNAVAILABLE")
	}
	return out.Bytes(), nil
}

type overviewTransport struct {
	ctx     context.Context
	process Process
}

func (p overviewTransport) Run(name string, args []string,
	input []byte) ([]byte, error) {
	if p.ctx.Err() != nil || p.process == nil {
		return nil, errors.New("GUEST_READ_UNAVAILABLE")
	}
	if _, native := p.process.(NativeProcess); native && len(args) == 2 &&
		args[1] == "observe-local-work" {
		return runNativeProcess(p.ctx, name, args, input, true)
	}
	if process, ok := p.process.(interface {
		RunContext(context.Context, string, []string, []byte) ([]byte, error)
	}); ok {
		return process.RunContext(p.ctx, name, args, input)
	}
	return p.process.Run(name, args, input)
}

func (p overviewTransport) DesktopQuery(name string, args []string) ([]byte, error) {
	return p.read(args, nil, desktopOutputLimit)
}

func (p overviewTransport) read(args []string, input []byte, limit int) ([]byte, error) {
	if p.ctx.Err() != nil || p.process == nil || len(input) > observationInputLimit ||
		len(input) > 0 && !overviewPrivateArgs(args) {
		return nil, errors.New("GUEST_READ_UNAVAILABLE")
	}
	var data []byte
	var err error
	if process, ok := p.process.(interface {
		DesktopReadContext(context.Context, []string, []byte, int) ([]byte, error)
	}); ok {
		data, err = process.DesktopReadContext(p.ctx, args, input, limit)
	} else {
		// Controlled Process fixtures implement the same private-input contract.
		data, err = p.Run("wsl.exe", args, input)
	}
	if len(data) > limit || p.ctx.Err() != nil {
		return nil, errors.New("GUEST_READ_UNAVAILABLE")
	}
	return data, err
}

// Private stdin is supported only for the fixed, bound observation bridge.
func overviewPrivateArgs(args []string) bool {
	if len(args) != 13 || args[0] != "--distribution" ||
		!guestName.MatchString(args[1]) || args[2] != "--cd" || args[3] != "~" ||
		args[4] != "--exec" || !validOverviewEntry(args[5]) ||
		args[6] != overviewBridgeCommand || args[7] != "observe" ||
		!dashboardLogin.MatchString(args[9]) || !machineIdentity.MatchString(args[10]) ||
		(args[11] != "amd64" && args[11] != "arm64") ||
		!overviewStamp.MatchString(args[12]) {
		return false
	}
	member, err := strconv.ParseInt(args[8], 10, 64)
	return err == nil && member > 0
}

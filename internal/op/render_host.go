package op

import (
	"context"
	"os/exec"
)

// RenderPodman returns podman on this host's Runner store for rendering the
// workflow files in the Nibfin image (internal/templates.Render). Missing
// Podman, a missing machine or an unsupported host is the host's own error
// with its install hint; a stopped machine is machine_stopped, naming the
// command to run again once it is started.
func RenderPodman(ctx context.Context, command string) (func(context.Context, ...string) *exec.Cmd, error) {
	h, err := detectHost(ctx)
	if err != nil {
		return nil, err
	}
	if h.NeedsStart() {
		return nil, Errorf(CodeMachineStopped, "%s is stopped, and the workflow files are rendered in the Nibfin image there; start it (podman machine start) and run gh nibfin %s again", h.Describe(), command)
	}
	return h.Podman, nil
}

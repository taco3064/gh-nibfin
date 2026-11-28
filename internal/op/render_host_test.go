package op

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/host"
)

func TestRenderPodman(t *testing.T) {
	old := detectHost
	t.Cleanup(func() { detectHost = old })

	f := &fakeHost{}
	detectHost = func(context.Context) (runnerHost, error) { return f, nil }
	podman, err := RenderPodman(context.Background(), "upgrade")
	if err != nil || podman == nil {
		t.Fatalf("RenderPodman = %v", err)
	}
	podman(context.Background(), "image", "exists", "x")
	if len(f.calls) != 1 || strings.Join(f.calls[0], " ") != "image exists x" {
		t.Errorf("podman ran %v, want the host's podman", f.calls)
	}

	f.stopped = true
	_, err = RenderPodman(context.Background(), "upgrade")
	if Code(err) != CodeMachineStopped || !strings.Contains(err.Error(), "fake is stopped") || !strings.Contains(err.Error(), "run gh nibfin upgrade again") {
		t.Errorf("stopped: err = %v", err)
	}
	if f.starts != 0 {
		t.Errorf("RenderPodman started the machine")
	}

	detectHost = func(context.Context) (runnerHost, error) {
		return nil, hostError(errors.Join(host.ErrPodmanMissing, errors.New("podman is not installed; install it from "+host.InstallHint)))
	}
	if _, err := RenderPodman(context.Background(), "init"); Code(err) != CodePodmanMissing || !strings.Contains(err.Error(), host.InstallHint) {
		t.Errorf("missing: err = %v", err)
	}
}

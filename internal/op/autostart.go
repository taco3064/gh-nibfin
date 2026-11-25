package op

import (
	"context"
	"fmt"

	"github.com/taco3064/gh-nibfin/internal/host"
)

// The host steps that keep the podman machine up while the host is on.
const (
	actSetWSLConfig      = "set wslconfig"
	actRegisterAutostart = "register autostart"
	actRemoveAutostart   = "remove autostart"
	actKeepWSLConfig     = "keep wslconfig"

	wslConfigValue = "[wsl2] vmIdleTimeout=-1"
	keepWSLConfig  = "left as it is; other WSL users may rely on vmIdleTimeout"
)

// autostartError maps a host autostart error to its code.
func autostartError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return Errorf(CodeCancelled, "cancelled")
	}
	return hostError(err)
}

// entryName names the autostart entry in plans and messages.
func entryName(as host.Autostart) string {
	switch as.Kind {
	case host.KindTask:
		return fmt.Sprintf("task %q", as.Name)
	case host.KindLaunchAgent:
		return "LaunchAgent " + as.Name
	}
	return ""
}

// entryTriggers says when the entry starts the machine.
func entryTriggers(kind string) string {
	if kind == host.KindTask {
		return "runs podman machine start at logon and on resume from sleep"
	}
	return "runs podman machine start at login"
}

func hasStep(steps []Step, action string) bool {
	for _, s := range steps {
		if s.Action == action {
			return true
		}
	}
	return false
}

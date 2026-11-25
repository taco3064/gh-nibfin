//go:build !darwin

package host

import (
	"context"
	"errors"
)

// waitPID is only used for the macOS LaunchAgent.
func waitPID(context.Context, int) error {
	return errors.New("waiting for a LaunchAgent's process needs macOS")
}

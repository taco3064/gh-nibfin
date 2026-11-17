//go:build !linux

package cli

import "errors"

func verifySharedRunnerExecutables(string, sharedTarget) error {
	return errors.New("UNSUPPORTED_SHARED_PLATFORM")
}

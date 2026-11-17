//go:build !linux

package cli

import (
	"errors"
	"io"
)

func (a App) runRebuild([]string, io.Writer) error {
	return errors.New("REBUILD_REQUIRES_MANAGED_LINUX")
}

func lockSharedInit(InitPlan) (func(), error) { return func() {}, nil }

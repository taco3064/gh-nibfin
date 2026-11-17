//go:build !linux

package cli

import (
	"errors"
	"io"
)

func (a App) runManagedInit(InitPlan, bool, io.Writer) error {
	return errors.New("MANAGED_INSTALL_REQUIRES_SUPPORTED_LINUX")
}

func managedIntentAvailable(Repository) bool { return false }

func managedIntentAgent(InitPlan) (*agentBinding, error) { return nil, nil }

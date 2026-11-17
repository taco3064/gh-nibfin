//go:build !linux

package cli

import "errors"

func (a App) agentCapability(string) (agentBinding, error) {
	return agentBinding{}, errors.New("AGENT_SELECTION_REQUIRES_MANAGED_LINUX")
}

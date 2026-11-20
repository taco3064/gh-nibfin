//go:build !linux

package cli

import "errors"

func (a App) observeInstalledUpgrade(string) (upgradeGuestObservation, error) {
	return upgradeGuestObservation{}, errors.New("UPGRADE_OBSERVATION_REQUIRES_LINUX")
}

func (a App) checkUpgradeExpectation(o upgradeOptions, _ *sharedHost) error {
	if o.Identity != "" || o.Architecture != "" || o.Installation != "" {
		return errors.New("UPGRADE_HANDOFF_REQUIRES_LINUX")
	}
	return nil
}

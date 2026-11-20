//go:build !linux

package cli

import (
	"errors"
	"io"
)

func (a App) applyNativeUpgrade(_ sharedHost, _, _ string, _ io.Writer) error {
	return errors.New("NATIVE_MIGRATION_REQUIRES_MANAGED_LINUX")
}

func (a App) previewUpgradeHandoff(_ sharedHost, _, _ string, _ upgradeSource) (
	[]upgradePlan, bool, error) {
	return nil, false, errors.New("NATIVE_MIGRATION_REQUIRES_MANAGED_LINUX")
}

func (a App) selectManagedNativeSource(host sharedHost,
	version string) (upgradeSource, error) {
	return resolveUpgrade(a.API, version)
}

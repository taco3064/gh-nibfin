//go:build !linux

package cli

import (
	"errors"
	"io"
)

func (a App) applyNativeUpgrade(host sharedHost, version string, out io.Writer) error {
	return errors.New("NATIVE_MIGRATION_REQUIRES_MANAGED_LINUX")
}

func (a App) selectManagedNativeSource(host sharedHost,
	version string) (upgradeSource, error) {
	return resolveUpgrade(a.API, version)
}

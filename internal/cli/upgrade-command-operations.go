package cli

import "io"

type upgradeCommandOperations struct {
	discover     func() (*sharedHost, error)
	verify       func(sharedHost) error
	selectSource func(sharedHost, string) (upgradeSource, error)
	applyNative  func(sharedHost, string, io.Writer) error
	recheck      func(sharedHost) error
}

func (a App) upgradeCommandOperations() upgradeCommandOperations {
	if a.upgrade != nil {
		return *a.upgrade
	}
	return upgradeCommandOperations{
		discover: func() (*sharedHost, error) { return discoverSharedHost(a.Process) },
		verify: func(host sharedHost) error {
			_, err := verifySharedHost(a.API, host)
			return err
		},
		selectSource: a.selectManagedNativeSource,
		applyNative:  a.applyNativeUpgrade,
		recheck: func(host sharedHost) error {
			return recheckSharedHost(a.Process, a.API, host)
		},
	}
}

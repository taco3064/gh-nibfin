package cli

import "io"

type upgradeCommandOperations struct {
	discover       func() (*sharedHost, error)
	verify         func(sharedHost) error
	selectSource   func(sharedHost, string) (upgradeSource, error)
	applyNative    func(sharedHost, string, io.Writer) error
	applyHandoff   func(sharedHost, string, string, io.Writer) error
	previewHandoff managedUpgradePreview
	recheck        func(sharedHost) error
}

type managedUpgradePreview func(sharedHost, string, string, upgradeSource) (
	[]upgradePlan, bool, error)

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
		selectSource:   a.selectManagedNativeSource,
		applyHandoff:   a.applyNativeUpgrade,
		previewHandoff: a.previewUpgradeHandoff,
		recheck: func(host sharedHost) error {
			return recheckSharedHost(a.Process, a.API, host)
		},
	}
}

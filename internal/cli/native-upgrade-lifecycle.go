package cli

import "errors"

type nativeUpgradeOperations struct {
	prepare  func() error
	lock     func() (func(), error)
	host     func() (sharedHost, error)
	migrate  func(sharedHost) ([]nativeMigrationResult, *nativeContinuation, error)
	maintain func(Producers) error
}

// Each stage owns its normal admission. Root maintenance never runs while the
// CLI holds the same host directory gate, and continuation uses fresh state.
func nativeUpgradeLifecycle(ops nativeUpgradeOperations) ([]nativeMigrationResult,
	*nativeContinuation, error) {
	if err := ops.prepare(); err != nil {
		return nil, nil, err
	}
	results, continuation, err := nativeUpgradeAdmitted(ops)
	var source *nativeSourceRequired
	if !errors.As(err, &source) {
		return results, continuation, err
	}
	if err := ops.maintain(source.Producers); err != nil {
		return results, continuation, err
	}
	return nativeUpgradeAdmitted(ops)
}

func nativeUpgradeAdmitted(ops nativeUpgradeOperations) ([]nativeMigrationResult,
	*nativeContinuation, error) {
	unlock, err := ops.lock()
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	host, err := ops.host()
	if err != nil {
		return nil, nil, err
	}
	return ops.migrate(host)
}

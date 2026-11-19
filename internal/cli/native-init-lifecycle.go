package cli

type nativeInitOperations struct {
	prepare func() error
	lock    func() (func(), error)
	resume  func() error
	verify  func() error
	finish  func() error
}

// Both existing-host preflight and post-adoption installation run outside the
// host gate. Each subsequent host phase reads current state under admission.
func nativeInitLifecycle(ops nativeInitOperations) error {
	if err := ops.prepare(); err != nil {
		return err
	}
	unlock, err := ops.lock()
	if err != nil {
		return err
	}
	runErr := ops.resume()
	unlock()
	if err := ops.prepare(); err != nil {
		return err
	}
	if runErr != nil {
		return runErr
	}
	unlock, err = ops.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if err := ops.verify(); err != nil {
		return err
	}
	return ops.finish()
}

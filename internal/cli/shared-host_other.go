//go:build !linux

package cli

func nativeSharedHostSupported() bool { return false }

func discoverSharedHost(Process) (*sharedHost, error) {
	return nil, nil
}

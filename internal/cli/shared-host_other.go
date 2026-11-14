//go:build !linux

package cli

func discoverSharedHost(Process) (*sharedHost, error) {
	return nil, nil
}

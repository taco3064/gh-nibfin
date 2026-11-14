//go:build !linux

package cli

import "errors"

func nativeSharedHostSupported() bool { return false }

func discoverSharedHost(Process) (*sharedHost, error) {
	return nil, nil
}

func installSharedService(Process) (string, error) {
	return "", errors.New("SHARED_SERVICE_INSTALLATION_REQUIRES_LINUX_X64")
}

//go:build !linux

package cli

import "errors"

func nativeSharedHostSupported() bool { return false }

func deploySharedCore(API, Process) (string, error) {
	return "", errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_LINUX_X64")
}

func prepareSharedNode(API, Process) (Process, error) {
	return nil, errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_LINUX_X64")
}

func discoverSharedHost(Process) (*sharedHost, error) {
	return nil, nil
}

func adoptInitialSharedHost(
	Process, InitPlan, []sharedRegistration,
) (*sharedHost, error) {
	return nil, errors.New("SHARED_HOST_ADMINISTRATION_REQUIRES_LINUX_X64")
}

func installSharedService(Process, []string, bool) (string, error) {
	return "", errors.New("SHARED_SERVICE_INSTALLATION_REQUIRES_LINUX_X64")
}

func runSharedHostAdmin(Process, string, ...string) ([]byte, error) {
	return nil, errors.New("SHARED_HOST_ADMINISTRATION_REQUIRES_LINUX_X64")
}

func nativeSharedTarget(sharedPlatform) (sharedTarget, error) {
	return sharedTarget{}, errors.New("UNSUPPORTED_SHARED_PLATFORM")
}

func managedTargetPreview(API, Process) any { return nil }

func nativeLinuxTarget() bool { return false }

func adoptedSharedTarget() (*sharedTarget, error) { return nil, nil }

//go:build !windows

package cli

func requirePrivateQuickPaths(paths []string) error {
	return requirePrivateRunnerPaths(paths)
}

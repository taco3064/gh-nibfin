//go:build !windows

package cli

func requirePrivateHandoffPaths(paths []string) error {
	return requirePrivateRunnerPaths(paths)
}

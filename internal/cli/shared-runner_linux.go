package cli

import "path/filepath"

func verifySharedRunnerExecutables(path string, target sharedTarget) error {
	for name, digest := range map[string]string{
		"Runner.Listener": target.ListenerHash, "Runner.Worker": target.WorkerHash,
	} {
		if err := verifySharedExecutableFile(filepath.Join(path, "bin", name), digest,
			target); err != nil {
			return err
		}
	}
	return nil
}

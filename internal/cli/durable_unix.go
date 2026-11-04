//go:build !windows

package cli

import (
	"errors"
	"os"
	"path/filepath"
)

func writeDurableState(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("RUNNER_STATE_WRITE_UNAVAILABLE")
	}
	_, writeErr := file.Write(data)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("RUNNER_STATE_WRITE_OR_SYNC_FAILED")
	}
	parent := filepath.Dir(path)
	for _, directory := range []string{parent, filepath.Dir(parent)} {
		file, err := os.Open(directory)
		if err != nil {
			return errors.New("RUNNER_STATE_DIRECTORY_SYNC_UNAVAILABLE")
		}
		syncErr, closeErr := file.Sync(), file.Close()
		if syncErr != nil || closeErr != nil {
			return errors.New("RUNNER_STATE_DIRECTORY_SYNC_UNAVAILABLE")
		}
	}
	return nil
}

func requirePrivateRunnerPaths(paths []string) error {
	for _, path := range paths {
		stat, err := os.Lstat(path)
		if err != nil || stat.Mode()&os.ModeSymlink != 0 || stat.Mode().Perm()&0077 != 0 {
			return errors.New("RUNNER_PRIVATE_FILE_PERMISSIONS_REQUIRED")
		}
	}
	return nil
}

func requirePrivateRunnerParent(directory string) error {
	for parent := filepath.Dir(directory); ; parent = filepath.Dir(parent) {
		stat, err := os.Stat(parent)
		if err != nil || stat.Mode().Perm()&0022 != 0 && stat.Mode()&os.ModeSticky == 0 {
			return errors.New("RUNNER_PRIVATE_PARENT_REQUIRED")
		}
		if filepath.Dir(parent) == parent {
			return nil
		}
	}
}

func initializePrivateRunnerDirectory(directory string) error {
	if os.Chmod(directory, 0700) != nil {
		return errors.New("RUNNER_PRIVATE_DIRECTORY_UNAVAILABLE")
	}
	return nil
}

package cli

import (
	"errors"
	"os"
	"path/filepath"
)

func hasGitAncestor(path string) bool {
	// A regular file cannot contain a .git entry. Keep probing its parents.
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		path = filepath.Dir(path)
	}
	for current := path; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); !errors.Is(err, os.ErrNotExist) {
			return true
		}
		if filepath.Dir(current) == current {
			return false
		}
	}
}

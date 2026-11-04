package cli

import (
	"errors"
	"os"
	"path/filepath"
)

func hasGitAncestor(path string) bool {
	for current := path; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); !errors.Is(err, os.ErrNotExist) {
			return true
		}
		if filepath.Dir(current) == current {
			return false
		}
	}
}

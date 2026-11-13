package cli

import (
	"errors"
	"os"
	"strings"
	"time"
)

func sweepEmptyVerifierDirectories(parent string) error {
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	// Leave newly reserved directories alone while another verifier starts.
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, entry := range entries {
		suffix, matches := strings.CutPrefix(entry.Name(), ".verify-")
		if !matches || suffix == "" || strings.Trim(suffix, "0123456789") != "" ||
			!entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		// Remove is atomic for empty directories and leaves any contents in place.
		if err := root.Remove(entry.Name()); err != nil && errors.Is(err, os.ErrPermission) {
			return errors.New("CANDIDATE_TEMPORARY_CLEANUP_INCOMPLETE")
		}
	}
	return nil
}

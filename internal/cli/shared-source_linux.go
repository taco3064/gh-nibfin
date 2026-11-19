package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func installedSharedPin() (SourcePin, error) {
	const path = "/etc/nibfin/host.json"
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return Pins().Core, nil
	}
	if err := requireRootOwnedHostPath(path); err != nil {
		return SourcePin{}, err
	}
	data, err := readSharedSourceFile(path, 256*1024)
	var host struct {
		Installation struct {
			Directory string    `json:"directory"`
			Pin       SourcePin `json:"pin"`
		} `json:"installation"`
	}
	if err != nil || json.Unmarshal(data, &host) != nil ||
		host.Installation.Directory != "/opt/nibfin/core" {
		return SourcePin{}, errors.New("INVALID_SHARED_HOST_INSTALLATION")
	}
	return host.Installation.Pin, nil
}

func readSharedSourceFile(path string, limit int64) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > limit ||
		stat.Sys().(*syscall.Stat_t).Nlink != 1 {
		return nil, errors.New("UNTRUSTED_SHARED_SOURCE_FILE")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("SHARED_SOURCE_LIMIT")
	}
	return data, nil
}

// Read and authenticate the manifest as data before importing installed Core.
// The caller also checks root ownership of the entire tree. The adopted pin
// preserves compatible installed versions instead of forcing a reinstall.
func verifyInstalledCoreManifest(root string, pin SourcePin) error {
	invalid := errors.New("SHARED_CORE_INSTALLED_SOURCE_MISMATCH")
	data, err := readSharedSourceFile(filepath.Join(root, coreManifestName), 1024*1024)
	if err != nil {
		return invalid
	}
	declarations, err := parseCoreManifest(data, pin)
	if err != nil {
		return invalid
	}
	seen := map[string]bool{coreManifestName: true}
	for _, source := range declarations {
		path := source.Path
		seen[path] = true
		full := filepath.Join(root, filepath.FromSlash(path))
		stat, err := os.Lstat(full)
		if err != nil || !stat.Mode().IsRegular() {
			return invalid
		}
		mode := "100644"
		if stat.Mode().Perm()&0111 != 0 {
			mode = "100755"
		}
		if source.Mode != mode || stat.Size() != source.Size {
			return invalid
		}
		file, err := os.OpenFile(full, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return invalid
		}
		opened, statErr := file.Stat()
		if statErr != nil || !opened.Mode().IsRegular() ||
			opened.Sys().(*syscall.Stat_t).Nlink != 1 {
			file.Close()
			return invalid
		}
		hash := sha256.New()
		size, readErr := io.Copy(hash, io.LimitReader(file, source.Size+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil ||
			!source.matches(mode, size, hex.EncodeToString(hash.Sum(nil))) {
			return invalid
		}
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return invalid
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || !seen[filepath.ToSlash(relative)] || !entry.Type().IsRegular() {
			return invalid
		}
		return nil
	})
}

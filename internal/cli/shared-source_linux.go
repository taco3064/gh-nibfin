package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	if pin.Repository != "taco3064/nibfin-core" || !commitPattern.MatchString(pin.Commit) ||
		!digestPattern.MatchString(pin.ManifestHash) {
		return invalid
	}
	data, err := readSharedSourceFile(filepath.Join(root, "nibfin-manifest.json"), 1024*1024)
	if err != nil {
		return invalid
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return invalid
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil ||
		sha256Hash(bytes.TrimSuffix(canonical.Bytes(), []byte("\n"))) != pin.ManifestHash {
		return invalid
	}
	var manifest struct {
		Schema     int    `json:"schemaVersion"`
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
		Files      []struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
			Size int64  `json:"size"`
			Hash string `json:"sha256"`
		} `json:"files"`
	}
	if json.Unmarshal(data, &manifest) != nil || manifest.Schema != 1 ||
		manifest.Repository != pin.Repository || manifest.Commit != pin.Commit ||
		len(manifest.Files) == 0 || len(manifest.Files) > 1000 {
		return invalid
	}
	seen := map[string]bool{"nibfin-manifest.json": true}
	var total int64
	safe := regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	for _, source := range manifest.Files {
		path := source.Path
		if len(path) > 100 || !safe.MatchString(path) || strings.HasPrefix(path, "/") ||
			seen[path] || source.Size < 0 || source.Size > 32*1024*1024 ||
			!digestPattern.MatchString(source.Hash) {
			return invalid
		}
		for _, part := range strings.Split(path, "/") {
			if part == "" || part == "." || part == ".." || part == ".git" {
				return invalid
			}
		}
		seen[path] = true
		total += source.Size
		if total > 32*1024*1024 {
			return invalid
		}
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
		if readErr != nil || closeErr != nil || size != source.Size ||
			hex.EncodeToString(hash.Sum(nil)) != source.Hash {
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

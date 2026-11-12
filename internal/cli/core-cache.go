package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func sha256Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func candidateArchive(api API, pins Producers) ([]byte, error) {
	path := fmt.Sprintf("repos/%s/actions/artifacts/%d",
		pins.Core.Repository, pins.Artifact.ID)
	var artifact struct {
		ID      int64  `json:"id"`
		Expired bool   `json:"expired"`
		Digest  string `json:"digest"`
	}
	if err := api.Call("GET", path, nil, &artifact); err != nil {
		return nil, err
	}
	if artifact.Expired || artifact.ID != pins.Artifact.ID ||
		artifact.Digest != pins.Artifact.Digest {
		return nil, errors.New("CANDIDATE_ARTIFACT_UNAVAILABLE_OR_CHANGED")
	}
	var data []byte
	if err := api.Call("GET", path+"/zip", nil, &data); err != nil {
		return nil, err
	}
	if "sha256:"+sha256Hash(data) != pins.Artifact.Digest {
		return nil, errors.New("CANDIDATE_ARTIFACT_HASH_MISMATCH")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) != 3 {
		return nil, errors.New("INVALID_CANDIDATE_ARTIFACT")
	}
	files := map[string][]byte{}
	for _, file := range reader.File {
		if file.Name != "nibfin-core.tar.gz" && file.Name != "source-pin.json" &&
			file.Name != "SHA256SUMS" || files[file.Name] != nil ||
			file.UncompressedSize64 > 32*1024*1024 {
			return nil, errors.New("UNSAFE_CANDIDATE_ARTIFACT")
		}
		stream, err := file.Open()
		if err != nil {
			return nil, errors.New("CANDIDATE_ARTIFACT_READ_FAILED")
		}
		content, readErr := io.ReadAll(io.LimitReader(stream, 32*1024*1024+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || len(content) > 32*1024*1024 {
			return nil, errors.New("CANDIDATE_ARTIFACT_READ_FAILED")
		}
		files[file.Name] = content
	}
	var pin SourcePin
	if json.Unmarshal(files["source-pin.json"], &pin) != nil || pin != pins.Core ||
		sha256Hash(files["nibfin-core.tar.gz"]) != pins.Core.ArchiveHash {
		return nil, errors.New("CORE_ARCHIVE_OR_PIN_MISMATCH")
	}
	return files["nibfin-core.tar.gz"], nil
}

func installCoreCache(api API, process Process) (directory string, err error) {
	return installPinnedCoreCache(api, process, Pins())
}

func installPinnedCoreCache(
	api API, process Process, pins Producers,
) (directory string, err error) {
	archive, err := candidateArchive(api, pins)
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", errors.New("USER_CACHE_UNAVAILABLE")
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		return "", errors.New("USER_CACHE_UNAVAILABLE")
	}
	// Resolve the OS-selected cache base (which can be a Windows volume alias)
	// before entering our private namespace. Reject aliases within that namespace.
	cache, err = filepath.EvalSymlinks(cache)
	if err != nil {
		return "", errors.New("USER_CACHE_UNAVAILABLE")
	}
	parent := filepath.Join(cache, "Nibfin", "candidates", "core")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", errors.New("CANDIDATE_DIRECTORY_UNAVAILABLE")
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent &&
		!(runtime.GOOS == "windows" && strings.EqualFold(resolved, parent)) {
		return "", errors.New("UNTRUSTED_CANDIDATE_DIRECTORY")
	}
	parent, err = coreVisibleDirectory(process, parent)
	if err != nil {
		return "", err
	}
	root, err := os.MkdirTemp(parent, ".verify-")
	if err != nil {
		return "", errors.New("CANDIDATE_DIRECTORY_UNAVAILABLE")
	}
	defer func() {
		if cleanupErr := os.RemoveAll(root); cleanupErr != nil {
			directory = ""
			err = errors.Join(err, errors.New("CANDIDATE_TEMPORARY_CLEANUP_INCOMPLETE"))
		}
	}()
	bootstrap := filepath.Join(root, "bootstrap")
	if err := unpackCore(archive, bootstrap); err != nil {
		return "", err
	}
	version, err := os.ReadFile(filepath.Join(bootstrap, ".node-version"))
	if err != nil {
		return "", errors.New("CORE_NODE_PIN_MISSING")
	}
	actual, err := process.Run("node", []string{"--version"}, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(actual)) != "v"+strings.TrimSpace(
		string(version)) {
		return "", errors.New("CORE_PINNED_NODE_REQUIRED")
	}
	pinPath := filepath.Join(root, "pin.json")
	pinJSON, _ := json.Marshal(pins.Core)
	if os.WriteFile(pinPath, pinJSON, 0600) != nil {
		return "", errors.New("CANDIDATE_FILE_UNAVAILABLE")
	}
	target := filepath.Join(parent, pins.Core.Commit)
	entry := filepath.Join(bootstrap, "bin", "nibfin-core.mjs")
	return verifyCache(process, archive, target, entry, pinPath, pins.Core)
}

func verifyCache(process Process, archive []byte, target, entry, pinPath string,
	pin SourcePin) (directory string, err error) {
	fresh := false
	defer func() {
		if err != nil && fresh {
			if cleanupErr := os.RemoveAll(target); cleanupErr != nil {
				err = errors.Join(err, errors.New("CANDIDATE_FAILED_CACHE_CLEANUP_INCOMPLETE"))
			}
		}
	}()
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		// This is a user-owned source cache, not Core's privileged host installer.
		// Reserve a new directory exclusively; never rename over existing data.
		if err := os.Mkdir(target, 0700); err != nil {
			return "", errors.New("CANDIDATE_CACHE_CHANGED_INSPECT_BEFORE_RETRY")
		}
		fresh = true
		if err := unpackCore(archive, target); err != nil {
			return "", err
		}
	}
	// The verifier itself comes from the hash-verified archive, never the cache.
	data, err := process.Run("node", []string{entry, "verify", target, pinPath}, nil)
	var result struct {
		Status string    `json:"status"`
		Source SourcePin `json:"source"`
	}
	if err != nil || json.Unmarshal(data, &result) != nil || result.Status != "verified" ||
		result.Source.Repository != pin.Repository || result.Source.Commit != pin.Commit {
		return "", errors.New("CORE_INSTALLATION_READBACK_FAILED")
	}
	return target, nil
}

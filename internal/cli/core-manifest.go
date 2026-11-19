package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const coreManifestName = "nibfin-manifest.json"

type coreManifestFile struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Size int64  `json:"size"`
	Hash string `json:"sha256"`
}

func parseCoreManifest(data []byte, pin SourcePin) ([]coreManifestFile, error) {
	invalid := errors.New("CORE_MANIFEST_MISMATCH")
	if pin.Repository != officialCore || !commitPattern.MatchString(pin.Commit) ||
		!digestPattern.MatchString(pin.ManifestHash) {
		return nil, invalid
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, invalid
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil ||
		sha256Hash(bytes.TrimSuffix(canonical.Bytes(), []byte("\n"))) != pin.ManifestHash {
		return nil, invalid
	}
	var manifest struct {
		Schema     int                `json:"schemaVersion"`
		Repository string             `json:"repository"`
		Commit     string             `json:"commit"`
		Files      []coreManifestFile `json:"files"`
	}
	if json.Unmarshal(data, &manifest) != nil || manifest.Schema != 1 ||
		manifest.Repository != pin.Repository || manifest.Commit != pin.Commit ||
		len(manifest.Files) == 0 || len(manifest.Files) > 1000 {
		return nil, invalid
	}
	seen := map[string]bool{coreManifestName: true}
	var total int64
	safe := regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	for _, source := range manifest.Files {
		path := source.Path
		if len(path) > 100 || !safe.MatchString(path) || strings.HasPrefix(path, "/") ||
			seen[path] || (source.Mode != "100644" && source.Mode != "100755") ||
			source.Size < 0 || source.Size > 32*1024*1024 ||
			!digestPattern.MatchString(source.Hash) {
			return nil, invalid
		}
		for _, part := range strings.Split(path, "/") {
			if part == "" || part == "." || part == ".." || part == ".git" {
				return nil, invalid
			}
		}
		seen[path] = true
		total += source.Size
		if total > 32*1024*1024 {
			return nil, invalid
		}
	}
	return manifest.Files, nil
}

func (source coreManifestFile) matches(mode string, size int64, hash string) bool {
	return source.Mode == mode && source.Size == size && source.Hash == hash
}

func verifyCoreArchiveManifest(files map[string]coreArchiveFile, pin SourcePin) error {
	declarations, err := parseCoreManifest(files[coreManifestName].data, pin)
	if err != nil {
		return err
	}
	if len(files) != len(declarations)+1 {
		return errors.New("CORE_MANIFEST_FILE_MISMATCH")
	}
	for _, source := range declarations {
		file, exists := files[source.Path]
		mode := "100644"
		if file.mode == 0755 {
			mode = "100755"
		}
		if !exists || !source.matches(mode, int64(len(file.data)), sha256Hash(file.data)) {
			return errors.New("CORE_MANIFEST_FILE_MISMATCH")
		}
	}
	return nil
}

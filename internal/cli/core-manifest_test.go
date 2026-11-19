package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Build producer-shaped data independently of the verifier's canonicalizer.
func fixtureCoreManifest(files map[string][]byte, pin *SourcePin) {
	entries := []map[string]any{}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		if name == "nibfin-manifest.json" {
			continue
		}
		entries = append(entries, map[string]any{"path": name,
			"mode": fmt.Sprintf("%o", 0100000|fixtureCoreMode(name)),
			"size": len(data), "sha256": sha256Hash(data)})
	}
	manifest := map[string]any{"schemaVersion": 1, "repository": pin.Repository,
		"commit": pin.Commit, "files": entries}
	canonical, _ := json.Marshal(manifest)
	pin.ManifestHash = sha256Hash(canonical)
	files["nibfin-manifest.json"] = append(canonical, '\n')
}

func fixtureCoreMode(name string) int64 {
	if name == "bin/nibfin-core.mjs" {
		return 0755
	}
	return 0644
}

func TestCoreArchiveManifest(t *testing.T) {
	kinds := []string{"valid", "format", "missing", "unrelated", "malformed",
		"trailing-json", "repository", "commit", "schema", "digest", "unsafe",
		"duplicate", "self", "missing-file", "extra", "altered", "mode", "size",
		"hash", "invalid-mode"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			pin := SourcePin{Repository: officialCore, Commit: strings.Repeat("a", 40)}
			raw := map[string][]byte{"bin/nibfin-core.mjs": []byte("never execute"),
				".node-version": []byte("24.22.1\n")}
			fixtureCoreManifest(raw, &pin)
			var manifest map[string]any
			json.Unmarshal(raw["nibfin-manifest.json"], &manifest)
			entries := manifest["files"].([]any)
			entry := entries[0].(map[string]any)
			switch kind {
			case "repository", "commit":
				manifest[kind] = "wrong"
			case "schema":
				manifest["schemaVersion"] = 2
			case "unsafe":
				entry["path"] = "../escape"
			case "duplicate":
				manifest["files"] = append(entries, entries[0])
			case "self":
				entry["path"] = "nibfin-manifest.json"
			case "size":
				entry["size"] = 999
			case "hash":
				entry["sha256"] = strings.Repeat("0", 64)
			case "invalid-mode":
				entry["mode"] = "100600"
			}
			canonical, _ := json.Marshal(manifest)
			pin.ManifestHash = sha256Hash(canonical)
			raw["nibfin-manifest.json"] = canonical
			switch kind {
			case "format":
				// Struct order differs from the sorted map used to compute the pin.
				reordered := struct {
					Schema     any `json:"schemaVersion"`
					Repository any `json:"repository"`
					Files      any `json:"files"`
					Commit     any `json:"commit"`
				}{manifest["schemaVersion"], manifest["repository"],
					manifest["files"], manifest["commit"]}
				raw["nibfin-manifest.json"], _ = json.MarshalIndent(reordered, "", "  ")
				raw["nibfin-manifest.json"] = append(raw["nibfin-manifest.json"], '\n')
			case "valid":
				raw["nibfin-manifest.json"] = append(canonical, '\n')
			case "missing":
				delete(raw, "nibfin-manifest.json")
			case "unrelated":
				raw["unrelated.json"] = canonical
				raw["nibfin-manifest.json"] = []byte("{}")
			case "malformed":
				raw["nibfin-manifest.json"] = []byte("{")
			case "trailing-json":
				raw["nibfin-manifest.json"] = append(canonical, []byte(" {}")...)
			case "digest":
				pin.ManifestHash = strings.Repeat("0", 64)
			case "missing-file":
				delete(raw, ".node-version")
			case "extra":
				raw["extra"] = []byte("extra")
			case "altered":
				raw[".node-version"] = []byte("substituted")
			}
			files := map[string]coreArchiveFile{}
			for name, data := range raw {
				mode := int64(0644)
				if name == "bin/nibfin-core.mjs" && kind != "mode" {
					mode = 0755
				}
				files[name] = coreArchiveFile{data: data, mode: mode}
			}
			err := verifyCoreArchiveManifest(files, pin)
			valid := kind == "valid" || kind == "format"
			if (err == nil) != valid {
				t.Fatalf("valid=%t: %v", valid, err)
			}
		})
	}
}

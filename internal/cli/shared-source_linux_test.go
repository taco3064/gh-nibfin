package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledCoreAuthenticatedBeforeExecution(t *testing.T) {
	root := t.TempDir()
	data := []byte("export const unexecuted = true;\n")
	file := filepath.Join(root, "entry.mjs")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	manifest := map[string]any{"schemaVersion": 1, "repository": "taco3064/nibfin-core",
		"commit": commit, "files": []map[string]any{{"path": "entry.mjs", "mode": "100644",
			"size": len(data), "sha256": sha256Hash(data)}}}
	canonical, _ := json.Marshal(manifest)
	pin := SourcePin{Repository: "taco3064/nibfin-core", Commit: commit,
		ManifestHash: sha256Hash(canonical)}
	if err := os.WriteFile(filepath.Join(root, "nibfin-manifest.json"), canonical,
		0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledCoreManifest(root, pin); err != nil {
		t.Fatal(err)
	}
	wrong := pin
	wrong.Commit = strings.Repeat("b", 40)
	if verifyInstalledCoreManifest(root, wrong) == nil {
		t.Fatal("wrong installation pin")
	}
	if err := os.WriteFile(file, []byte("tampered source"), 0600); err != nil {
		t.Fatal(err)
	}
	if verifyInstalledCoreManifest(root, pin) == nil {
		t.Fatal("source bytes not verified")
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(root, "unreviewed.mjs")
	if err := os.WriteFile(extra, data, 0600); err != nil {
		t.Fatal(err)
	}
	if verifyInstalledCoreManifest(root, pin) == nil {
		t.Fatal("unexpected module accepted")
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0700); err != nil {
		t.Fatal(err)
	}
	if verifyInstalledCoreManifest(root, pin) == nil {
		t.Fatal("changed source mode accepted")
	}
}

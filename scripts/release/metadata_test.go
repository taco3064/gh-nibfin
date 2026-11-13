package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompleteRelease(t *testing.T) {
	root := repository(t)
	source := []byte("package main\nfunc main() {}\n")
	sourcePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(sourcePath, source, 0644); err != nil {
		t.Fatal(err)
	}
	if err := produce(root, "v1.2.3-rc.1+test", build); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "dist")
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result manifest
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Version != "1.2.3-rc.1+test" || len(result.Targets) != 6 {
		t.Fatalf("unexpected manifest: %+v", result)
	}
	sums, err := os.ReadFile(filepath.Join(directory, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(sums)), "\n")
	if len(lines) != 7 {
		t.Fatal("incorrect checksum count")
	}
	for i, line := range lines {
		fields := strings.Split(line, "  ")
		if len(fields) != 2 {
			t.Fatal(line)
		}
		actual, err := describe(directory, fields[1])
		if err != nil {
			t.Fatal(err)
		}
		if actual.SHA256 != fields[0] {
			t.Fatal("checksum mismatch")
		}
		if i < 6 {
			item := result.Targets[i]
			target := targets()[i]
			if item.Filename != target.filename() || item.Size != actual.Size ||
				item.SHA256 != actual.SHA256 || item.OS != target.OS ||
				item.Arch != target.Arch {
				t.Fatalf("metadata mismatch: %+v", item)
			}
			info, err := os.Stat(filepath.Join(directory, item.Filename))
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && target.OS != "windows" &&
				info.Mode().Perm()&0111 != 0111 {
				t.Fatal("missing executable permissions")
			}
		}
	}
	digest := sha256.Sum256(data)
	if !strings.HasPrefix(lines[6], hex.EncodeToString(digest[:])+"  manifest.json") {
		t.Fatal("manifest checksum missing")
	}
	if err := produce(root, "2.0.0", build); err == nil {
		t.Fatal("repeated invocation accepted existing set")
	}
	after, err := os.ReadFile(sourcePath)
	if err != nil || string(after) != string(source) {
		t.Fatal("source changed")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 3 {
		t.Fatal("unexpected persistent output")
	}
	// A valid set with one missing executable must fail validation.
	if err := os.Remove(filepath.Join(directory, targets()[0].filename())); err != nil {
		t.Fatal(err)
	}
	if err := writeMetadata(directory, result.Version); err == nil {
		t.Fatal("missing artifact accepted")
	}
}

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersions(t *testing.T) {
	for _, value := range []string{"1.2.3", "v0.0.0", "1.0.0-rc.1+build.007"} {
		got, err := normalizeVersion(value)
		if err != nil || got != strings.TrimPrefix(value, "v") {
			t.Fatalf("%q: %q, %v", value, got, err)
		}
	}
	for _, value := range []string{"", "1.2", "01.2.3", "1.2.3-01", "1.2.3;id",
		"1.2.3-", "1.2.3+", "1.2.3-rc..1", "vv1.2.3", " 1.2.3"} {
		if _, err := normalizeVersion(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func repository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	data := []byte("module github.com/taco3064/gh-nibfin\n\ngo 1.27.1\n")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestFailedBuildsCleanWorkspace(t *testing.T) {
	for _, mode := range []string{"invalid", "failure", "missing", "wrong-format",
		"unexpected"} {
		t.Run(mode, func(t *testing.T) {
			root := repository(t)
			version := "1.2.3"
			if mode == "invalid" {
				version = "bad"
			}
			calls := 0
			builder := func(_, path, _ string, target target) error {
				calls++
				if mode == "failure" {
					return errors.New("build failed")
				}
				if mode == "missing" {
					return nil
				}
				if mode == "unexpected" {
					extra := filepath.Join(filepath.Dir(path), "extra")
					if err := os.WriteFile(extra, []byte("extra"), 0644); err != nil {
						return err
					}
				}
				return os.WriteFile(path, []byte("wrong format"), 0644)
			}
			if err := produce(root, version, builder); err == nil {
				t.Fatal("incomplete release succeeded")
			}
			if mode == "invalid" && calls != 0 {
				t.Fatal("built invalid version")
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "go.mod" {
				t.Fatalf("workspace changed: %v", entries)
			}
		})
	}
}

func TestExistingOutputPreserved(t *testing.T) {
	for _, kind := range []string{"directory", "file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := repository(t)
			path := filepath.Join(root, "dist")
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0755)
			case "file":
				err = os.WriteFile(path, []byte("unrelated"), 0644)
			case "symlink":
				err = os.Symlink("go.mod", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			builder := func(_, _, _ string, _ target) error {
				t.Fatal("existing destination reached builder")
				return nil
			}
			if err := produce(root, "1.2.3", builder); err == nil {
				t.Fatal("existing destination accepted")
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("destination replaced: %v", err)
			}
		})
	}
}

func TestTargetNames(t *testing.T) {
	expected := []string{"gh-nibfin_linux_amd64", "gh-nibfin_linux_arm64",
		"gh-nibfin_darwin_amd64", "gh-nibfin_darwin_arm64",
		"gh-nibfin_windows_amd64.exe", "gh-nibfin_windows_arm64.exe"}
	for i, target := range targets() {
		if target.filename() != expected[i] {
			t.Fatal(target.filename())
		}
	}
}

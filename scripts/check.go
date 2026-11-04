package main

import (
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

func check(path string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if entry.IsDir() {
		if entry.Name() == ".git" || entry.Name() == "dist" {
			return filepath.SkipDir
		}
		return nil
	}
	if !strings.HasSuffix(path, ".go") {
		return nil
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return fmt.Errorf("source symlink: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) > 1000 {
		return fmt.Errorf("source exceeds 1000 lines: %s", path)
	}
	for i, line := range lines {
		if utf8.RuneCountInString(strings.TrimSuffix(line, "\r")) > 90 {
			return fmt.Errorf("source exceeds 90 characters: %s:%d", path, i+1)
		}
	}
	formatted, err := format.Source(data)
	if err != nil {
		return err
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	if string(formatted) != normalized {
		return fmt.Errorf("run gofmt: %s", path)
	}
	return nil
}

func main() {
	if runtime.Version() != "go1.27.1" {
		fmt.Fprintln(os.Stderr, "use pinned Go 1.27.1")
		os.Exit(1)
	}
	if err := filepath.WalkDir(".", check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Source structure, formatting and pinned toolchain verified.")
}

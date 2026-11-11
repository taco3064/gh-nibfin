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
		if entry.Name() == ".git" || entry.Name() == "dist" || entry.Name() == "node_modules" {
			return filepath.SkipDir
		}
		return nil
	}
	extensions := ".go .js .mjs .cjs .ts .tsx .jsx .css .html .sh .py .ps1"
	if !strings.Contains(" "+extensions+" ", " "+filepath.Ext(path)+" ") {
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
	if !strings.HasSuffix(path, ".go") {
		return nil
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
	module, err := os.ReadFile("go.mod")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read go.mod")
		os.Exit(1)
	}
	version := ""
	for _, line := range strings.Split(string(module), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			version = fields[1]
		}
	}
	if version == "" || runtime.Version() != "go"+version {
		fmt.Fprintln(os.Stderr, "use the Go version pinned in go.mod")
		os.Exit(1)
	}
	if err := filepath.WalkDir(".", check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Source structure, formatting and pinned toolchain verified.")
}

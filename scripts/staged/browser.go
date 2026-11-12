package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Provision JavaScript dependencies only; CI uses its preinstalled Chrome.
func runBrowser() error {
	program := "npm"
	var prefix []string
	if runtime.GOOS == "windows" {
		program = "cmd.exe"
		prefix = []string{"/d", "/c", "npm"}
	}
	for _, args := range [][]string{
		{"ci", "--ignore-scripts", "--no-audit", "--no-fund"},
		{"run", "test:browser"},
	} {
		command := exec.Command(program, append(prefix, args...)...)
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			return err
		}
	}
	return nil
}

func browserAffected(paths []string) bool {
	for _, path := range paths {
		if strings.HasPrefix(path, "internal/dashboard/") ||
			strings.HasPrefix(path, "tests/browser/") ||
			strings.HasPrefix(path, "scripts/staged/") {
			return true
		}
		switch path {
		case "package.json", "package-lock.json", "scripts/check.go", "go.mod", "go.sum",
			".github/workflows/ci.yml":
			return true
		}
	}
	return false
}

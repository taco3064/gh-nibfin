package main

import "testing"

func TestBrowserSelection(t *testing.T) {
	for _, path := range []string{"internal/dashboard/assets/graph.js",
		"internal/dashboard/service.go", "tests/browser/fixtures.mjs", "package.json",
		"package-lock.json", "scripts/check.go", "scripts/staged/main.go",
		".github/workflows/ci.yml"} {
		if !browserAffected([]string{path}) {
			t.Errorf("browser changes not selected: %s", path)
		}
	}
	for _, paths := range [][]string{nil, {"README.md"}, {"internal/cli/app.go"},
		{"docs/snapshot.md", "main.go"}} {
		if browserAffected(paths) {
			t.Errorf("unrelated changes selected browser: %v", paths)
		}
	}
}

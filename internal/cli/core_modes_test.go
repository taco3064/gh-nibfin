package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCoreExtractionPreservesPrivateExecutableClass(t *testing.T) {
	for _, mode := range []int64{0644, 0755} {
		root := t.TempDir()
		names := []string{"bin/nibfin-core.mjs", ".node-version"}
		if err := unpackCore(testArchiveMode(t, names, mode), root); err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			stat, err := os.Stat(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" {
				want := os.FileMode(0600) | os.FileMode(mode&0100)
				if stat.Mode().Perm() != want {
					t.Fatalf("mode %o extracted as %o, want %o", mode,
						stat.Mode().Perm(), want)
				}
			}
		}
	}
}

func TestCoreExtractionRejectsUnsupportedModes(t *testing.T) {
	for _, mode := range []int64{0, 0600, 0666, 0777, 04755, 0100644} {
		names := []string{"bin/nibfin-core.mjs", ".node-version"}
		if unpackCore(testArchiveMode(t, names, mode), t.TempDir()) == nil {
			t.Fatalf("unsupported archive mode %o was accepted", mode)
		}
	}
}

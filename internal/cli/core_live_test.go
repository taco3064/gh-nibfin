package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLiveCandidateCache(t *testing.T) {
	if os.Getenv("NIBFIN_TEST_LIVE_READS") != "1" {
		t.Skip("explicit live public-source verification only")
	}
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("XDG_CACHE_HOME", root)
	process := NativeProcess{}
	path, err := installCoreCache(GitHub{Process: process}, process)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "Nibfin", "candidates", "core", Pins().Core.Commit)
	if path != want {
		t.Fatal("candidate must be installed inside the isolated test cache")
	}
	t.Log("actual Core archive/manifest/installation verified; no host adoption")
}

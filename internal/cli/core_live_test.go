package cli

import (
	"os"
	"os/exec"
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
	cacheBase, _ := os.UserCacheDir()
	canonicalBase, _ := filepath.EvalSymlinks(cacheBase)
	t.Logf("OS cache base=%q canonical=%q", cacheBase, canonicalBase)
	process := NativeProcess{}
	if node, err := exec.LookPath("node"); err == nil {
		resolved, _ := filepath.EvalSymlinks(node)
		if stat, err := os.Stat(resolved); err == nil {
			t.Logf("Node executable: %s mode=%v", resolved, stat.Mode())
		}
	}
	path, err := installCoreCache(GitHub{Process: process}, process)
	if err != nil {
		cache, _ := os.UserCacheDir()
		canonical, resolveErr := filepath.EvalSymlinks(cache)
		t.Logf("OS cache: %s canonical=%s error=%v", cache, canonical, resolveErr)
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonical, "Nibfin", "candidates", "core", Pins().Core.Commit)
	if path != want {
		t.Fatal("candidate must be installed inside the isolated test cache")
	}
	t.Log("actual Core archive/manifest/installation verified; no host adoption")
}

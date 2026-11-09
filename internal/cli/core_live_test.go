package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	api := GitHub{Process: process}
	if err := verifyProducers(api, Pins()); err != nil {
		t.Fatal(err)
	}
	recipes, err := callerRecipes(api, Pins().Core)
	if err != nil {
		t.Fatal(err)
	}
	files := templates(Pins(), 127829492, recipes)
	for name, kind := range map[string]string{"wake": "action-dispatch", "issue": "issue"} {
		path := ".github/workflows/nibfin-" + name + ".yml"
		want := strings.ReplaceAll(strings.Join(recipes[kind], "\n"),
			"CORE_COMMIT", Pins().Core.Commit)
		want = strings.ReplaceAll(want, "ACTION_COMMIT", Pins().Action.Commit)
		want = strings.ReplaceAll(want, "MEMBER_ID", "127829492")
		if files[path] != want {
			t.Fatal("caller must exactly match immutable Core recipe")
		}
	}
	wake := files[".github/workflows/nibfin-wake.yml"]
	if !strings.Contains(wake, "\n    timeout-minutes: 180\n") {
		t.Fatal("installed delivery job must have a three-hour timeout")
	}
	if node, err := exec.LookPath("node"); err == nil {
		resolved, _ := filepath.EvalSymlinks(node)
		if stat, err := os.Stat(resolved); err == nil {
			t.Logf("Node executable: %s mode=%v", resolved, stat.Mode())
		}
	}
	path, err := installCoreCache(api, process)
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

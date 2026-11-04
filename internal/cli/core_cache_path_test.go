package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type canonicalPathFixture struct{ path string }

func (f canonicalPathFixture) Run(string, []string, []byte) ([]byte, error) {
	return json.Marshal(f.path)
}

func TestCoreCanonicalPathRequiresSameDirectoryIdentity(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	other, _ = filepath.EvalSymlinks(other)
	actual, err := coreVisibleDirectory(canonicalPathFixture{root}, root)
	if err != nil || actual != root {
		t.Fatal("canonical spelling of the same directory must be accepted")
	}
	for _, path := range []string{other, ".", "", root + string(os.PathSeparator)} {
		if _, err := coreVisibleDirectory(canonicalPathFixture{path}, root); err == nil {
			t.Fatal("different or noncanonical directory must never redirect the cache")
		}
	}
}

func TestLiveCoreDefaultCacheDirectoryIdentity(t *testing.T) {
	if os.Getenv("NIBFIN_TEST_LIVE_READS") != "1" {
		t.Skip("explicit real default-cache path inspection only")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(cache, "Nibfin", "candidates", "core")
	if _, err := os.Stat(parent); err != nil {
		t.Skip("default candidate namespace does not already exist")
	}
	canonical, err := coreVisibleDirectory(NativeProcess{}, parent)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native directory identity verified; spelling changed=%t", canonical != parent)
}

package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type failingVerifier struct{}

func (failingVerifier) Run(string, []string, []byte) ([]byte, error) {
	return nil, errors.New("SYNTHETIC_VERIFICATION_FAILURE")
}

func testArchive(t *testing.T, names []string) []byte {
	return testArchiveMode(t, names, 0644)
}

func testArchiveMode(t *testing.T, names []string, mode int64) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for _, name := range names {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: mode,
			Typeflag: tar.TypeReg, Size: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if writer.Close() != nil || compressed.Close() != nil {
		t.Fatal("cannot create fixture archive")
	}
	return buffer.Bytes()
}

func TestFailedFreshCacheRemovedAndExistingDataPreserved(t *testing.T) {
	archive := testArchive(t, []string{"bin/nibfin-core.mjs", ".node-version"})
	for _, existing := range []bool{false, true} {
		target := filepath.Join(t.TempDir(), "candidate")
		if existing {
			if os.Mkdir(target, 0700) != nil ||
				os.WriteFile(filepath.Join(target, "user-data"), []byte("keep"), 0600) != nil {
				t.Fatal("cannot prepare existing cache")
			}
		}
		_, err := verifyCache(failingVerifier{}, archive, target, "trusted-verifier",
			"approved-pin", Pins().Core)
		if err == nil {
			t.Fatal("failed verification must not succeed")
		}
		_, statErr := os.Stat(target)
		if existing && statErr != nil || !existing && !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal("cleanup must remove only the newly created failed cache")
		}
	}
}

func TestCoreExtractionRejectsEscapesAndDuplicateEntries(t *testing.T) {
	for _, names := range [][]string{{"../escape"}, {"/absolute"}, {"x", "x"},
		{".git/config"}, {`bin\escape`}} {
		root := t.TempDir()
		if unpackCore(testArchive(t, names), filepath.Join(root, "cache")) == nil {
			t.Fatal("unsafe archive must fail")
		}
		if _, err := os.Stat(filepath.Join(root, "escape")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("archive escaped its destination")
		}
	}
}

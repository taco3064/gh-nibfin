package cli

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestRunnerArchiveNodeCommandLinks(t *testing.T) {
	const link = "externals/node24/bin/npm"
	const target = "../lib/node_modules/npm/bin/npm-cli.js"
	for _, test := range []struct {
		name, destination string
		ok                bool
	}{
		{"official", target, true},
		{"escaping", "../../../../outside", false},
		{"absolute", "/bin/sh", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "runner.tar.gz")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(file)
			writer := tar.NewWriter(gz)
			entry := &tar.Header{Name: link, Mode: 0777, Typeflag: tar.TypeSymlink,
				Linkname: test.destination}
			if err := writer.WriteHeader(entry); err != nil {
				t.Fatal(err)
			}
			for _, close := range []func() error{writer.Close, gz.Close, file.Close} {
				if err := close(); err != nil {
					t.Fatal(err)
				}
			}
			err = unpackRunner(archive, root, false)
			if (err == nil) != test.ok {
				t.Fatalf("unexpected extraction result: %v", err)
			}
			if test.ok {
				got, err := os.Readlink(filepath.Join(root, link))
				if err != nil || got != target {
					t.Fatalf("unexpected extracted link: %q, %v", got, err)
				}
			}
		})
	}
}

func TestLiveOfficialRunnerArchive(t *testing.T) {
	source, hash := os.Getenv("NIBFIN_TEST_RUNNER_ARCHIVE"),
		os.Getenv("NIBFIN_TEST_RUNNER_ARCHIVE_SHA256")
	if source == "" || !digestPattern.MatchString(hash) {
		t.Skip("verified official Runner archive is not supplied")
	}
	root := t.TempDir()
	if err := initializePrivateRunnerDirectory(root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, ".nibfin-runner-package")
	if err := os.WriteFile(archive, data, 0600); err != nil {
		t.Fatal(err)
	}
	app := RunnerApplication{Filename: "runner.tar.gz", Hash: hash}
	if err := unpackRunner(archive, root, false); err != nil {
		t.Fatal(err)
	}
	if err := verifyRunnerFiles(root, app); err != nil {
		t.Fatal(err)
	}
	if err := (NativeRunnerHost{}).Check(root); err != nil {
		t.Fatal(err)
	}
}

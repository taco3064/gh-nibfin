package cli

import (
	"archive/tar"
	"compress/gzip"
	"io"
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
	a, api, _, directory := runnerFixture(t)
	plan := InitPlan{
		Repository: api.data["repos/example/project"].(Repository),
		Member:     api.data["user"].(User),
	}
	app := RunnerApplication{Filename: "runner.tar.gz", Hash: hash}
	a.Download = func(_ RunnerApplication, destination string) error {
		file, err := os.Open(source)
		if err != nil {
			return err
		}
		defer file.Close()
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, file)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	paths, err := a.stageRunnerSlots(plan, filepath.Dir(directory), app)
	if err != nil || len(paths) != 2 {
		t.Fatalf("expected both official Runner packages, got %v: %v", paths, err)
	}
	for _, root := range paths {
		if err := (NativeRunnerHost{}).Check(root); err != nil {
			t.Fatal(err)
		}
	}
}

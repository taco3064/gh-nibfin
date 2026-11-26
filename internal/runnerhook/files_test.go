package runnerhook

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFiles(t *testing.T) {
	jsonl, err := os.ReadFile(filepath.Join("testdata", "trusted-root.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	old := execGH
	t.Cleanup(func() { execGH = old })
	var args []string
	execGH = func(_ context.Context, a ...string) (stdout, stderr bytes.Buffer, err error) {
		args = a
		stdout.Write(jsonl)
		return
	}
	files, err := Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "attestation trusted-root" {
		t.Errorf("ran gh %v", args)
	}
	for _, name := range []string{Script, Check, OfficialDir + "/nibfin-shared-runner.yml", OfficialDir + "/nibfin-job.yml", TrustedRoot} {
		if len(files[name]) == 0 {
			t.Errorf("no %s", name)
		}
	}
	if len(files) != 5 {
		t.Errorf("%d files", len(files))
	}

	execGH = func(context.Context, ...string) (stdout, stderr bytes.Buffer, err error) {
		stderr.WriteString("tuf: connection refused")
		return stdout, stderr, errors.New("exit status 1")
	}
	if _, err := Files(context.Background()); err == nil || !strings.Contains(err.Error(), "tuf: connection refused") {
		t.Errorf("err = %v", err)
	}
}

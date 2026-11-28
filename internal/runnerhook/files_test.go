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
	rendered := map[string][]byte{
		".github/workflows/nibfin-issue.yml":         []byte("issue\n"),
		".github/workflows/nibfin-shared-runner.yml": []byte("shared\n"),
		".github/workflows/nibfin-job.yml":           []byte("job\n"),
	}
	files, err := Files(context.Background(), rendered)
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
	if len(files) != 5 || string(files[OfficialDir+"/nibfin-job.yml"]) != "job\n" {
		t.Errorf("%d files", len(files))
	}
	if _, err := Files(context.Background(), map[string][]byte{}); err == nil || !strings.Contains(err.Error(), "lack") {
		t.Errorf("without official files: err = %v", err)
	}

	execGH = func(context.Context, ...string) (stdout, stderr bytes.Buffer, err error) {
		stderr.WriteString("tuf: connection refused")
		return stdout, stderr, errors.New("exit status 1")
	}
	if _, err := Files(context.Background(), rendered); err == nil || !strings.Contains(err.Error(), "tuf: connection refused") {
		t.Errorf("err = %v", err)
	}
}

package cli

import (
	"os/exec"
	"testing"
)

func TestSharedNodeXZExtraction(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("system Python required for Linux installer tests")
	}
	command := exec.Command(python, "-I", "shared-core-deploy_test.py")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("synthetic Node extraction: %v\n%s", err, output)
	}
}

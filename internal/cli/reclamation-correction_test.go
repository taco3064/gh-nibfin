package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSparseInteractiveResume(t *testing.T) {
	for _, answer := range []string{"yes", "unchanged"} {
		t.Run(answer, func(t *testing.T) {
			p := &sparseFake{observations: []sparseObservation{
				{"binding", "disabled", "supported"}}, result: "operation_failed"}
			var out bytes.Buffer
			err := (App{Process: p}).runDesktopInput([]string{
				"init", "--repo", "owner/repo", "--apply", "--consent-open-target",
			}, "windows", &out, strings.NewReader(answer+"\n"), true)
			if err == nil || !strings.Contains(err.Error(), "GUEST_OPEN_UNAVAILABLE") {
				t.Fatalf("expected fixture opening failure, got %v", err)
			}
			text := out.String()
			index := strings.LastIndex(text, "Desktop resume:")
			if index < 0 {
				t.Fatal(text)
			}
			resume := strings.SplitN(text[index:], "\n", 2)[0]
			choice := "enable"
			if answer == "unchanged" {
				choice = answer
			}
			if !strings.Contains(resume, "--reclamation "+choice) ||
				strings.Contains(resume, "--consent-experimental-sparse") != (answer == "yes") {
				t.Fatal(resume)
			}
			if answer == "unchanged" && p.mutations != 0 {
				t.Fatal("unchanged choice mutated sparse state")
			}
		})
	}
}

func TestSparseOperationProcessErrors(t *testing.T) {
	failure := errors.New("process failed")
	tests := []struct {
		name   string
		err    error
		output []byte
		want   string
	}{
		{"success", nil, nil, "succeeded"},
		{"launch_denied", &os.PathError{Op: "fork/exec", Path: "private",
			Err: os.ErrPermission}, nil, "permission_denied"},
		{"provider_denied", failure, []byte("E_ACCESSDENIED"), "permission_denied"},
		{"utf16_denied", failure, wslUTF16("0x80070005", false, true),
			"permission_denied"},
		{"unsupported", failure, []byte("0x80070032"), "unsupported"},
		{"unknown", failure, nil, "operation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sparseOperationResult(tt.err, tt.output); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

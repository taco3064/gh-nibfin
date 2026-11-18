package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardObservationNativeDenialIsSanitized(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"denied", "observed", "other-command"} {
		status := mode
		command := "observe-local-work"
		if mode == "other-command" {
			status, command = "denied", "other"
		}
		script := `process.stdout.write(JSON.stringify({schemaVersion:1,status:'` +
			status + `',secret:'fixture-private-output'}));
process.stderr.write('fixture-private-diagnostic'); process.exit(1);`
		path := filepath.Join(t.TempDir(), "observation.mjs")
		if err := os.WriteFile(path, []byte(script), 0600); err != nil {
			t.Fatal(err)
		}
		data, err := runResolvedNativeProcess(context.Background(), "node", node,
			[]string{path, command}, nil, true)
		if strings.Contains(string(data), "private") {
			t.Fatal("failed output escaped sanitization")
		}
		if mode == "denied" {
			if err != nil || string(data) != `{"schemaVersion":1,"status":"denied"}` {
				t.Fatal("supported denial not retained safely", err)
			}
		} else if err == nil || len(data) != 0 {
			t.Fatal("unrelated failure retained output", mode)
		}
	}
}

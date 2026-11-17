package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelayLocalAtomicConfigurationAndCredentialBoundary(t *testing.T) {
	root := t.TempDir()
	// Relocate fixed system paths into a disposable fixture and substitute the
	// current test UID. Production retains root ownership and ancestry checks.
	script := strings.ReplaceAll(managedRelayLocal, `ROOT = "/etc/nibfin"`,
		"ROOT = "+string(jsonRelay(root)))
	script = strings.ReplaceAll(script, `STATE = "/var/lib/nibfin/notification-relay"`,
		"STATE = "+string(jsonRelay(filepath.Join(root, "state"))))
	script = strings.ReplaceAll(script, `UNITS = "/etc/systemd/system"`,
		"UNITS = "+string(jsonRelay(filepath.Join(root, "units"))))
	script = strings.ReplaceAll(script, "info.st_uid != 0", "info.st_uid != os.getuid()")
	script = strings.ReplaceAll(script, `if current == "/":`, `if current == ROOT:`)
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("system Python is required for relay administration tests")
	}
	call := func(request relayLocalRequest, success bool) []byte {
		t.Helper()
		command := exec.Command(python, "-I", "-c", script)
		command.Stdin = bytes.NewReader(jsonRelay(request))
		out, err := command.CombinedOutput()
		if (err == nil) != success {
			t.Fatalf("local %s: %v %s", request.Operation, err, out)
		}
		return out
	}
	host := map[string]any{
		"memberId": 7, "tokenFile": "/must-never-be-read/controller-token",
		"installation": map[string]any{"pin": Pins().Core},
		"sharedRunner": map[string]any{"repositories": []int64{10}},
		"repository": map[string]any{
			"approval": map[string]any{"repository": "example/project", "repositoryId": 10},
			"eventRunner": map[string]any{
				"repository": "example/project", "repositoryId": 10, "memberId": 7,
				"workflowId": 40, "workflowPath": managedIssueWorkflow,
				"sourceCommit": strings.Repeat("a", 40),
				"workflowBlob": strings.Repeat("b", 40),
				"reusable": map[string]string{"repository": Pins().Core.Repository,
					"path": ".github/workflows/issue.yml", "commit": Pins().Core.Commit},
			},
		},
	}
	path := filepath.Join(root, "host.json")
	if err := os.WriteFile(path, jsonRelay(host), 0600); err != nil {
		t.Fatal(err)
	}
	call(relayLocalRequest{Operation: "lock"}, true)
	call(relayLocalRequest{Operation: "lock"}, false)
	statePath := filepath.Join(root, "state")
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	call(relayLocalRequest{Operation: "read"}, false)
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	var snapshot relayLocalSnapshot
	if err := json.Unmarshal(call(relayLocalRequest{Operation: "read"}, true),
		&snapshot); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(snapshot.Bindings, []byte("token")) {
		t.Fatal("credential location leaked into the projection")
	}
	config := relayConfiguration{Version: 1, Origins: []relayOrigin{}}
	call(relayLocalRequest{"write", &snapshot, &config}, true)
	call(relayLocalRequest{"write", &snapshot, &config}, false)
	info, err := os.Stat(filepath.Join(root, "notification-relay.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("configuration must be private: %v", err)
	}
	call(relayLocalRequest{Operation: "unlock"}, true)
	_, lockErr := os.Stat(filepath.Join(root, "notification-relay.cli-lock"))
	if !os.IsNotExist(lockErr) {
		t.Fatal("lock not removed")
	}
	// A hostile peer path never becomes a filesystem read, even for an adopted ID.
	host["wakePeerFiles"] = []string{"/must-never-be-read/peer.json"}
	if err := os.WriteFile(path, jsonRelay(host), 0600); err != nil {
		t.Fatal(err)
	}
	call(relayLocalRequest{Operation: "lock"}, true)
	call(relayLocalRequest{Operation: "read"}, false)
	call(relayLocalRequest{Operation: "unlock"}, true)
}

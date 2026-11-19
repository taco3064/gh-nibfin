package cli

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed core-maintenance-bootstrap-fixture.py
var controlledBootstrapFixture []byte

func requireBootstrapOriginalManifest(t *testing.T, root, retained string) {
	t.Helper()
	original, err := os.ReadFile(filepath.Join(root, "original-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(retained, "nibfin-manifest.json"))
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("original retained source bytes were lost", retained, err, string(data))
	}
}

func TestMaintenanceBootstrapUsesExistingGateAndActualVerifiedSource(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated eligible root-owned fixture ancestors")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("controlled bootstrap runtime prerequisite", err)
	}
	for _, scenario := range []string{"fresh", "newer-reuse", "lost-pin",
		"missing-probe", "foreign-pin", "tampered-source", "different-target",
		"foreign-archive", "bad-archive", "node-pending", "node-malformed",
		"checkpoint-retained", "checkpoint-pin-replaced", "checkpoint-pin-synced",
		"checkpoint-before-tree", "checkpoint-tree", "checkpoint-tree-synced",
		"checkpoint-retained-next-target", "checkpoint-pin-replaced-next-target",
		"checkpoint-pin-synced-next-target", "checkpoint-before-tree-next-target",
		"checkpoint-tree-next-target", "checkpoint-tree-synced-next-target",
		"record-null", "record-array", "record-string", "record-number", "record-bool",
		"record-null-probe", "record-array-probe", "record-string-probe",
		"record-number-probe", "record-bool-probe", "manifest-null", "manifest-array",
		"manifest-string", "manifest-number", "manifest-bool", "manifest-null-probe",
		"manifest-array-probe", "manifest-string-probe", "manifest-number-probe",
		"manifest-bool-probe", "invalid-capability-null", "invalid-capability-array",
		"invalid-platform-null", "invalid-platform-array", "invalid-targets-null",
		"invalid-targets-array", "invalid-request-null", "invalid-request-array"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			installer := filepath.Join(dir, "installer.py")
			if err := os.WriteFile(installer, []byte(sharedDeploymentSource(
				[]string{"bootstrap"})), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "controlled-bootstrap.py")
			if err := os.WriteFile(path, controlledBootstrapFixture, 0600); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(dir, "host")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(context.Background(), "/usr/bin/python3", "-I",
				path, installer, root, node, scenario)
			output, err := cmd.CombinedOutput()
			if scenario == "node-malformed" {
				if err == nil || strings.TrimSpace(string(output)) != "not a receipt" {
					t.Fatal("malformed child result changed", err, string(output))
				}
				return
			}
			var result struct {
				Status   string `json:"status"`
				Source   SourcePin
				Code     string
				Retained []string               `json:"recoveryRetained"`
				GitHub   nativeGitHubDiagnostic `json:"githubApi"`
			}
			if scenario == "node-pending" {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 ||
					json.Unmarshal(output, &result) != nil || result.Status != "pending" ||
					result.Code != "BOOTSTRAP_PENDING" || result.GitHub.HTTPStatus == nil ||
					*result.GitHub.HTTPStatus != 422 || len(result.Retained) != 2 {
					t.Fatal("child typed pending result changed", err, string(output))
				}
				return
			}
			if err != nil || json.Unmarshal(output, &result) != nil {
				t.Fatal("fixed bootstrap/gate failed", err, string(output))
			}
			if scenario == "missing-probe" || scenario == "tampered-source" ||
				strings.HasSuffix(scenario, "-probe") {
				if result.Status != "missing" {
					t.Fatal("missing metadata probe fabricated source", string(output))
				}
				if _, err := os.Lstat(filepath.Join(root, "executed-b")); !os.IsNotExist(err) {
					t.Fatal("unknown old bytes executed", err)
				}
				requireBootstrapOriginalManifest(t, root,
					filepath.Join(root, "maintenance", "bootstrap"))
				return
			}
			if scenario == "foreign-archive" || scenario == "bad-archive" ||
				strings.HasPrefix(scenario, "invalid-") {
				if result.Status != "rejected" {
					t.Fatal("invalid archive changed old source", string(output))
				}
				return
			}
			want := "b"
			if scenario == "different-target" || strings.HasPrefix(scenario, "checkpoint-") ||
				strings.HasPrefix(scenario, "record-") || strings.HasPrefix(scenario, "manifest-") {
				want = "c"
				if strings.HasSuffix(scenario, "-next-target") &&
					!strings.HasPrefix(scenario, "checkpoint-tree") {
					want = "d"
				}
			}
			if result.Status != "installed" || result.Source.Commit != strings.Repeat(want, 40) {
				t.Fatal("actual source was not reused", string(output))
			}
			kept, _ := filepath.Glob(filepath.Join(root, "maintenance", ".bootstrap-*",
				"retained-bootstrap"))
			retained := scenario != "fresh" && scenario != "newer-reuse"
			if retained && (len(kept) != 1 || len(result.Retained) != 2) ||
				!retained && (len(kept) != 0 || len(result.Retained) != 1) {
				t.Fatal("unknown old bytes or nonblocking diagnostics lost", kept, string(output))
			}
			if retained {
				requireBootstrapOriginalManifest(t, root, kept[0])
			}
			if want != "b" {
				if _, err := os.Lstat(filepath.Join(root, "executed-b")); !os.IsNotExist(err) {
					t.Fatal("unverified old B source executed", err)
				}
			}
		})
	}
}

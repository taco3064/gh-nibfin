package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRebuildProjectionWithControlledCoreModules(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "bounded", true: "changed"}[changed],
			func(t *testing.T) {
				d := newRebuildDriver(t)
				root := t.TempDir()
				if err := os.Mkdir(filepath.Join(root, "src"), 0700); err != nil {
					t.Fatal(err)
				}
				hostJSON := `{"private":"fixture-secret","credentialPath":"/private"}`
				pool, _ := json.Marshal(d.old)
				retirement := map[string]any{"id": fixtureRetirementID, "phase": "removal",
					"pending": "NATIVE_REMOVAL_REQUIRED", "binding": map[string]any{
						"pool": d.old, "repositories": d.repositories,
						"adoptions": []string{"fixture-private-history"}}}
				files := map[string]string{
					"host-adoption.mjs": `
export const hostFile = '/fixture-host';
let reads = 0;
export async function boundedHostFile() {
  reads++;
  return ` + string(jsonRelay(hostJSON)) + ` +
    (` + string(jsonRelay(changed)) + ` && reads > 1 ? 'changed' : '');
}`,
					"host-paths.mjs": `export async function rootOwnedPath() {}`,
					"shared-host-inspect.mjs": `export async function inspectSharedHost() {
  return ` + string(pool) + `;
}`,
					"provider-binding.mjs": `export function installedProvider() {
  return { provider: 'codex', modelProfile: { model: 'gpt-6.1' },
    components: { hostProfile: 'linux-rootful-docker-v1' } };
}`,
					"shared-runner.mjs": `export async function sharedRepository(host, id) {
  return { repository: { approval: {
    repository: id === 10 ? 'example/project' : 'example/peer' } } };
}`,
					"retirement-files.mjs": `export async function readRetirementFile(path) {
  return path === '/fixture-retirement' ? ` + string(jsonRelay(retirement)) + ` : {};
}`,
					"retirement-admission.mjs": `
export const retirementPath = '/fixture-retirement', drainPath = '/fixture-drain';`,
				}
				for name, source := range files {
					if err := os.WriteFile(filepath.Join(root, "src", name),
						[]byte(source), 0600); err != nil {
						t.Fatal(err)
					}
				}
				// The fixture uses the CI interpreter without the installed-tool
				// path policy. Production always verifies its fixed paths.
				script := strings.ReplaceAll(rebuildSnapshotScript, "/opt/nibfin/core", root)
				output, err := exec.Command("node", "--input-type=module", "--eval", script).
					Output()
				if changed {
					if err == nil || len(output) != 0 {
						t.Fatal("changed adoption was reported as a stable snapshot")
					}
					return
				}
				var snapshot rebuildSnapshot
				if err != nil || json.Unmarshal(output, &snapshot) != nil ||
					!validRebuildSnapshot(snapshot) || !snapshot.Draining {
					t.Fatal("projection failed", err, string(output))
				}
				for _, secret := range []string{"fixture-secret", "private-history",
					"credentialPath", "adoptions"} {
					if strings.Contains(string(output), secret) {
						t.Fatal("private Core document escaped projection", secret)
					}
				}
			})
	}
}

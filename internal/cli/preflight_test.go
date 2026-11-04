package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type inspectionProcess struct {
	dockerHost string
	missing    bool
	calls      []string
}

func (p *inspectionProcess) Run(name string, args []string, _ []byte) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	p.calls = append(p.calls, call)
	if p.missing {
		return nil, errors.New("synthetic-secret-in-untrusted-error")
	}
	switch call {
	case "codex login status":
		return []byte("chatgpt"), nil
	case "docker context show":
		return []byte("default"), nil
	case "docker context inspect default":
		return []byte(`[{"Endpoints":{"docker":{"Host":"` + p.dockerHost + `"}}}]`), nil
	case "docker --host unix:///var/run/docker.sock info --format {{json .}}":
		return []byte(`{"OSType":"linux","OperatingSystem":"Docker Desktop",` +
			`"SecurityOptions":["name=rootless","name=apparmor"]}`), nil
	}
	return []byte("synthetic-version-secret-not-for-output"), nil
}

func cleanDockerEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS",
		"DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_CONFIG"} {
		t.Setenv(key, "")
	}
}

func TestPreflightReportsFactsWithoutProtectedReadiness(t *testing.T) {
	cleanDockerEnvironment(t)
	process := &inspectionProcess{dockerHost: "unix:///var/run/docker.sock"}
	api := initFixture()
	var output bytes.Buffer
	err := (App{API: api, Process: process}).Run(
		[]string{"preflight", "--repo", "example/project"}, &output)
	var command *CommandError
	if !errors.As(err, &command) || command.Exit != 2 {
		t.Fatal("missing Core proof must remain unverified")
	}
	for _, expected := range []string{"CHATGPT_LOGIN_PRESENT",
		"DOCKER_DESKTOP_LINUX_ENGINE_REPORTED", "ENGINE_FLAG_REPORTED_ONLY",
		"PINNED_CORE_HAS_NO_READ_ONLY_ISOLATION_PROOF_ENTRY"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatal("missing observed capability or exact proof boundary: " + expected)
		}
	}
	if strings.Contains(output.String(), "secret") || len(api.writes) != 0 {
		t.Fatal("inspection must redact raw results and never write provider state")
	}
	last := process.calls[len(process.calls)-1]
	if last != "docker --host unix:///var/run/docker.sock info --format {{json .}}" {
		t.Fatal("engine query must pin the validated local endpoint, not mutable context")
	}
}

func TestPreflightMissingAndDeniedToolsNeverExposeDiagnostics(t *testing.T) {
	cleanDockerEnvironment(t)
	api := initFixture()
	repo := api.data["repos/example/project"].(Repository)
	repo.Permissions = nil
	api.data["repos/example/project"] = repo
	var output bytes.Buffer
	_ = (App{API: api, Process: &inspectionProcess{missing: true}}).Run(
		[]string{"preflight", "--repo", "example/project"}, &output)
	if strings.Contains(output.String(), "secret") ||
		!strings.Contains(output.String(), "REPOSITORY_PERMISSION_REQUIRED") ||
		!strings.Contains(output.String(), "TOOL_INSPECTION_UNAVAILABLE") {
		t.Fatal("denied or missing tools must have bounded redacted diagnostics")
	}
}

func TestDockerRemoteAndAmbientOverridesDoNotContactDaemon(t *testing.T) {
	cleanDockerEnvironment(t)
	process := &inspectionProcess{dockerHost: "tcp://unrelated.example:2375"}
	checks := inspectDocker(process)
	if len(process.calls) != 2 ||
		checks[0].Code != "REMOTE_OR_UNKNOWN_DOCKER_ENDPOINT_NOT_PROBED" {
		t.Fatal("remote endpoint must not be contacted")
	}
	t.Setenv("DOCKER_HOST", "tcp://unrelated.example:2375")
	process.calls = nil
	_ = inspectDocker(process)
	if len(process.calls) != 0 {
		t.Fatal("ambient daemon overrides must not execute Docker")
	}
}

func TestCodexLoginClassificationNeverReturnsRawSecrets(t *testing.T) {
	for _, test := range []struct{ text, mode string }{
		{"Logged in using ChatGPT", "chatgpt"},
		{"Logged in using an API key - synthetic-secret", "api_key"},
		{"synthetic-secret unknown output", ""},
	} {
		data, err := codexLoginMode(test.text)
		if string(data) != test.mode || (err == nil) != (test.mode != "") {
			t.Fatal("login classification must expose only a recognized enum")
		}
	}
}

func TestManagedRunnerInspectionPreservesRegistrationAndRejectsTampering(t *testing.T) {
	app, api, host, directory := runnerFixture(t)
	if _, err := runRunnerTest(app, "create", directory, true); err != nil {
		t.Fatal(err)
	}
	repo, user, err := repository(api, "example/project", true)
	if err != nil {
		t.Fatal(err)
	}
	check := app.inspectManagedRunner(directory, repo, user)
	if check.Status != "available" {
		t.Fatal("verified package/runtime inspection should succeed")
	}
	path := filepath.Join(directory, "bin", "Runner.Listener")
	if err := os.WriteFile(path, []byte("tampered fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	check = app.inspectManagedRunner(directory, repo, user)
	if check.Code != "OFFICIAL_PACKAGE_BYTES_OR_PERMISSIONS_UNVERIFIED" {
		t.Fatal("changed package must have a precise denial")
	}
	if api.tokens != 0 || host.calls != 0 {
		t.Fatal("read-only inspection must not request tokens or configure Runner")
	}
}

func TestToolPathRejectsProjectExecutableBeforeExecution(t *testing.T) {
	root := privateRunnerTestRoot(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	name := "nibfin-unsafe-tool"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	err := os.WriteFile(filepath.Join(root, name), []byte("not executable"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	if _, err := toolPath(name); err == nil || err.Error() != "UNTRUSTED_TOOL_PATH" {
		t.Fatal("project supplied executable must never run")
	}
}

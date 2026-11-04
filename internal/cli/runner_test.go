package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func privateRunnerTestRoot(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(home, ".nibfin-runner-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error("owned Runner test directory cleanup failed")
		}
	})
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

const runnerTokenSentinel = "synthetic-runner-secret"

type runnerAPI struct {
	*fakeAPI
	app        RunnerApplication
	runners    []RunnerInfo
	tokens     int
	deletes    int
	expired    bool
	deleteLost bool
	now        time.Time
}

func (f *runnerAPI) Call(method, path string, input, output any) error {
	base := "repos/example/project/actions/runners"
	if method == "GET" && path == base+"/downloads" {
		return assign(output, []RunnerApplication{f.app})
	}
	if method == "GET" && path == base+"?per_page=100&page=1" {
		return assign(output, map[string]any{"total_count": len(f.runners),
			"runners": f.runners})
	}
	if method == "POST" && (path == base+"/registration-token" ||
		path == base+"/remove-token") {
		f.tokens++
		expires := f.now.Add(time.Hour)
		if f.expired {
			expires = f.now.Add(-time.Second)
		}
		return assign(output, map[string]string{"token": runnerTokenSentinel,
			"expires_at": expires.Format(time.RFC3339)})
	}
	if method == "GET" && path == base+"/42" {
		if len(f.runners) == 0 {
			return errors.New("NOT_FOUND")
		}
		return assign(output, f.runners[0])
	}
	if method == "DELETE" && path == base+"/42" {
		f.deletes++
		f.runners = nil
		if f.deleteLost {
			return errors.New("PROCESS_RESULT_UNAVAILABLE")
		}
		return nil
	}
	return f.fakeAPI.Call(method, path, input, output)
}

type runnerHostFixture struct {
	api          *runnerAPI
	calls        int
	lost         bool
	keepLocal    bool
	tokenPresent bool
}

func (*runnerHostFixture) Check(string) error { return nil }

func (f *runnerHostFixture) Configure(root, command string,
	inputs map[string]string) error {
	f.calls++
	f.tokenPresent = inputs["TOKEN"] == runnerTokenSentinel
	if command == "configure" {
		runner := RunnerInfo{ID: 42, Name: inputs["NAME"], Status: "offline"}
		runner.Labels = append(runner.Labels, struct {
			Name string `json:"name"`
		}{
			Name: inputs["LABELS"]})
		f.api.runners = []RunnerInfo{runner}
		settings := map[string]any{"agentId": 42, "agentName": runner.Name,
			"gitHubUrl": inputs["URL"]}
		data, _ := json.Marshal(settings)
		if err := os.WriteFile(filepath.Join(root, ".runner"), data, 0600); err != nil {
			return err
		}
	} else {
		f.api.runners = nil
		if !f.keepLocal {
			if err := os.Remove(filepath.Join(root, ".runner")); err != nil {
				return err
			}
		}
	}
	if f.lost {
		return errors.New("RUNNER_TIMEOUT_RESULT_UNKNOWN")
	}
	return nil
}

func runnerFixture(t *testing.T) (App, *runnerAPI, *runnerHostFixture, string) {
	t.Helper()
	var archive bytes.Buffer
	files := map[string]string{"bin/Runner.Listener": "fixture executable",
		"bin/Runner.Listener.exe": "fixture executable"}
	zipped := runtime.GOOS == "windows"
	if zipped {
		writer := zip.NewWriter(&archive)
		for name, body := range files {
			file, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = file.Write([]byte(body))
		}
		_ = writer.Close()
	} else {
		compressed := gzip.NewWriter(&archive)
		writer := tar.NewWriter(compressed)
		for name, body := range files {
			_ = writer.WriteHeader(&tar.Header{Name: name, Mode: 0700,
				Typeflag: tar.TypeReg, Size: int64(len(body))})
			_, _ = writer.Write([]byte(body))
		}
		_ = writer.Close()
		_ = compressed.Close()
	}
	osName := map[string]string{"windows": "win", "darwin": "osx", "linux": "linux"}
	arch := map[string]string{"amd64": "x64", "arm64": "arm64", "arm": "arm"}
	extension := ".tar.gz"
	if zipped {
		extension = ".zip"
	}
	filename := fmt.Sprintf("actions-runner-%s-%s-1.0.0%s",
		osName[runtime.GOOS], arch[runtime.GOARCH], extension)
	api := &runnerAPI{fakeAPI: initFixture(), now: time.Now().UTC(),
		app: RunnerApplication{OS: osName[runtime.GOOS], Arch: arch[runtime.GOARCH],
			Filename: filename, Hash: sha256Hash(archive.Bytes()),
			URL: "https://github.com/actions/runner/releases/download/v1.0.0/" + filename}}
	host := &runnerHostFixture{api: api}
	app := App{API: api, Runner: host, Clock: func() time.Time { return api.now },
		Download: func(_ RunnerApplication, path string) error {
			return os.WriteFile(path, archive.Bytes(), 0600)
		}}
	return app, api, host, filepath.Join(privateRunnerTestRoot(t), "managed-runner")
}

func runRunnerTest(app App, command, directory string, apply bool) (string, error) {
	args := []string{"runner", command, "--repo", "example/project",
		"--directory", directory}
	if command == "create" {
		args = append(args, "--name", "candidate-runner")
	}
	if apply {
		args = append(args, "--apply")
	}
	var output bytes.Buffer
	err := app.Run(args, &output)
	return output.String(), err
}

func TestRunnerPreviewThenRegistrationAndUnregister(t *testing.T) {
	app, api, host, directory := runnerFixture(t)
	if _, err := runRunnerTest(app, "create", directory, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) || api.tokens != 0 {
		t.Fatal("preview must not create files or issue tokens")
	}
	for _, command := range []string{"create", "register", "unregister"} {
		output, err := runRunnerTest(app, command, directory, true)
		if err != nil || strings.Contains(output, runnerTokenSentinel) {
			t.Fatalf("%s failed or disclosed a token: %v; result=%s", command, err, output)
		}
		data, err := os.ReadFile(filepath.Join(directory, runnerRecordName))
		if err != nil || bytes.Contains(data, []byte(runnerTokenSentinel)) {
			t.Fatal("management metadata must contain no token")
		}
	}
	if host.calls != 2 || !host.tokenPresent || api.tokens != 2 || len(api.runners) != 0 {
		t.Fatal("official configuration and scoped read-back must agree")
	}
}

func TestRunnerExpiredTokenTamperingAndMissingAdminNeverConfigure(t *testing.T) {
	for _, kind := range []string{"expired", "tampered", "no-admin", "env-override",
		"state-write"} {
		app, api, host, directory := runnerFixture(t)
		if _, err := runRunnerTest(app, "create", directory, true); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "expired":
			api.expired = true
		case "tampered":
			if os.WriteFile(filepath.Join(directory, "bin/Runner.Listener"),
				[]byte("changed"), 0600) != nil {
				t.Fatal("cannot mutate fixture executable")
			}
		case "no-admin":
			repo := api.data["repos/example/project"].(Repository)
			repo.Permissions["admin"] = false
			api.data["repos/example/project"] = repo
		case "env-override":
			path := filepath.Join(directory, ".env")
			data := []byte("ACTIONS_RUNNER_INPUT_RUNASSERVICE=true\n")
			if os.WriteFile(path, data, 0600) != nil {
				t.Fatal("cannot prepare override fixture")
			}
		case "state-write":
			if runtime.GOOS != "windows" && os.Geteuid() == 0 {
				continue // A root test process bypasses ordinary file permissions.
			}
			path := filepath.Join(directory, runnerRecordName)
			if os.Chmod(path, 0400) != nil {
				t.Fatal("cannot prepare unwritable state fixture")
			}
			defer os.Chmod(path, 0600)
		}
		if _, err := runRunnerTest(app, "register", directory, true); err == nil {
			t.Fatal("invalid registration must reject")
		}
		if host.calls != 0 {
			t.Fatal("denied registration cannot touch official configuration")
		}
		if (kind == "env-override" || kind == "state-write") && api.tokens != 0 {
			t.Fatal("unsafe environment or unsaved intent must reject before token request")
		}
	}
}

func TestRunnerUnknownRegistrationCannotBeBlindlyRepeated(t *testing.T) {
	app, _, host, directory := runnerFixture(t)
	if _, err := runRunnerTest(app, "create", directory, true); err != nil {
		t.Fatal(err)
	}
	host.lost = true
	for i := 0; i < 2; i++ {
		if _, err := runRunnerTest(app, "register", directory, true); err == nil {
			t.Fatal("unknown registration must not succeed")
		}
	}
	if host.calls != 1 {
		t.Fatal("unknown official process must not be retried")
	}
}

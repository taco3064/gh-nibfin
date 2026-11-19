package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMaintenanceProcessPreservesOnlyFixedSafeExitMatrix(t *testing.T) {
	for _, scenario := range []string{"unsupported", "denied", "sudo", "http422",
		"unknown", "malformed", "non-bridge"} {
		t.Run(scenario, func(t *testing.T) {
			body := `{"status":"unsupported","code":"RUNNER_RUNTIME_REFRESH_REQUIRED",` +
				`"detail":"private","token":"private","body":"private",` +
				`"url":"private","githubApi":{"kind":"http","httpStatus":422,` +
				`"message":"private","resource":"private"}}`
			exit := 1
			switch scenario {
			case "denied":
				body = strings.Replace(body, "unsupported", "denied", 1)
			case "http422":
				body = strings.Replace(body, "unsupported", "pending", 1)
				exit = 2
			case "unknown":
				body = `{"status":"unknown","code":"PRIVATE","message":"private"}`
			case "malformed":
				body = "private malformed response"
			}
			path := filepath.Join(t.TempDir(), "fixed-bridge-fixture.py")
			program := "#!/usr/bin/python3 -I\nimport sys\nsys.stdout.write(" +
				strconv.Quote(body) + ")\nraise SystemExit(" + strconv.Itoa(exit) + ")\n"
			if err := os.WriteFile(path, []byte(program), 0700); err != nil {
				t.Fatal(err)
			}
			process := coreResultProcess{ctx: context.Background(),
				resolve: func(string) (string, error) { return path, nil }}
			name, args := maintenanceBridge, []string(nil)
			if scenario == "sudo" {
				name, args = "/usr/bin/sudo", []string{"-n", "--", maintenanceBridge}
			} else if scenario == "non-bridge" {
				name = "/usr/bin/unrelated-error"
			}
			var captured []byte
			_, _, err := nativeUpgradeLifecycle(nativeUpgradeOperations{
				prepare: func() error { return nil },
				lock:    func() (func(), error) { return func() {}, nil },
				host:    func() (sharedHost, error) { return sharedHost{}, nil },
				migrate: func(sharedHost) ([]nativeMigrationResult,
					*nativeContinuation, error) {
					return nil, nil, &nativeSourceRequired{Producers: Pins()}
				},
				maintain: func(Producers) error {
					output, callErr := process.Run(name, args, []byte(`{"kind":"inspect"}`))
					captured = output
					return decodeCoreSourceMaintenance(output, callErr)
				},
			})
			if err == nil {
				t.Fatal("failed root process became source success")
			}
			if scenario == "unknown" || scenario == "malformed" || scenario == "non-bridge" {
				if len(captured) != 0 || coreOperationCause(err) != nil {
					t.Fatal("unrecognized process acquired a trusted result", string(captured), err)
				}
				return
			}
			var command *CommandError
			cause := coreOperationCause(err)
			if !errors.As(err, &command) || command.Exit != exit || cause == nil ||
				cause.GitHubAPI == nil || cause.GitHubAPI.HTTPStatus == nil ||
				*cause.GitHubAPI.HTTPStatus != 422 {
				t.Fatal("process/source lifecycle lost safe root result", err, cause)
			}
			if scenario != "http422" && strings.Contains(string(captured), "private") {
				t.Fatal("exit-1 transport disclosed unapproved root output", string(captured))
			}
		})
	}
}

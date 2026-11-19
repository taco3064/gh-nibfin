package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNativeSecretPrivateProcessInput(t *testing.T) {
	token := []byte("synthetic-private-credential")
	command := nativeSecretCommand(context.Background(), "/usr/bin/gh",
		"example/project", token)
	if strings.Contains(strings.Join(command.Args, " "), string(token)) {
		t.Fatal("credential entered command arguments")
	}
	input, err := io.ReadAll(command.Stdin)
	if err != nil || !bytes.Equal(input, token) {
		t.Fatal("secret must be sent only over private standard input")
	}
	found := false
	for _, item := range command.Env {
		if strings.HasPrefix(item, "GH_TOKEN=") {
			found = item == "GH_TOKEN="+string(token)
		}
	}
	if !found || command.Stdout != nil || command.Stderr != nil {
		t.Fatal("selected-account authentication and discarded diagnostics required")
	}
}

func TestInstalledContinuationValidatesExitAndPayload(t *testing.T) {
	pending := errors.New("CORE_PENDING_RESULT")
	for _, test := range []struct {
		output string
		err    error
		status string
	}{
		{`{"status":"dispatched","results":[]}`, nil, "dispatched"},
		{`{"status":"paused","results":[]}`, nil, "paused"},
		{`{"status":"pending","results":[]}`, pending, "pending"},
		{`{"status":"dispatched","results":[]}`, pending, "pending"},
		{`{"status":"paused","results":[]}`, pending, "pending"},
		{`{"status":"pending","results":[]}`, nil, "pending"},
		{`{"status":"dispatched"}`, nil, "pending"},
		{`{"status":"dispatched","results":null}`, nil, "pending"},
		{`{"status":"completed","results":[]}`, nil, "pending"},
		{`{"status":"dispatched","results":[null]}`, nil, "pending"},
		{`{"status":"dispatched","results":["invalid"]}`, nil, "pending"},
		{`{"status":"dispatched","results":[{}]}`, nil, "pending"},
		{`{}`, nil, "pending"},
		{``, nil, "pending"},
		{`broken`, nil, "pending"},
	} {
		result := readInstalledContinuation([]byte(test.output), test.err)
		if result.Status != test.status {
			t.Fatalf("invalid outcome: %+v", result)
		}
		if test.status == "pending" && result.Code == "" && test.err != pending {
			t.Fatal("invalid response must explain missing evidence")
		}
	}
}

func nativeReadyHost(t *testing.T) (*sharedHost, []sharedRegistration) {
	t.Helper()
	host, err := decodeSharedHost([]byte(`{
  "schemaVersion":1,"repositoryId":10,"repository":"example/project","memberId":7,
  "adoptedRepositoryIds":[10],
  "runners":[{"id":25,"name":"slot-1"},{"id":26,"name":"slot-2"}]
 }`))
	if err != nil {
		t.Fatal(err)
	}
	return host, []sharedRegistration{
		{ID: 25, Name: "slot-1", Status: "online"},
		{ID: 26, Name: "slot-2", Status: "online"},
	}
}

func TestNativeReadinessAndContinuationAreIndependent(t *testing.T) {
	api, _, request := nativeSetupFixture()
	host, runners := nativeReadyHost(t)
	plan := InitPlan{NativeManaged: true, Repository: request.Target,
		Member: request.Account, Execution: &request.Destination}
	var output bytes.Buffer
	calls := 0
	ops := managedHostOperations{
		discover: func() (*sharedHost, error) { return host, nil },
		complete: func() ([]byte, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("fixture response lost")
			}
			return []byte(`{"status":"paused","results":[]}`), nil
		},
	}
	app := App{API: api}
	for attempt := range 2 {
		err := app.publishReadyManagedRoute(json.NewEncoder(&output), plan, runners, ops)
		if attempt == 0 {
			var pending *CommandError
			if !errors.As(err, &pending) || pending.Exit != 2 {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || !strings.Contains(output.String(), `"installation":"ready"`) ||
		!strings.Contains(output.String(), `"status":"paused"`) {
		t.Fatal("installation readiness and continuation must remain separate")
	}
	runners[0].Status = "offline"
	if app.publishReadyManagedRoute(json.NewEncoder(&output), plan, runners, ops) == nil ||
		calls != 2 {
		t.Fatal("continuation ran before readiness")
	}
}

func TestInstalledContinuationProcessPreservesPendingBoundary(t *testing.T) {
	process := coreResultProcess{ctx: context.Background()}
	for _, test := range []struct{ script, status string }{
		{`printf '%s' '{"status":"pending","results":[]}'; exit 2`, "pending"},
		{`printf '%s' '{"status":"paused","results":[]}'`, "paused"},
		{`printf '%s' '{"status":"dispatched","results":[]}'`, "dispatched"},
		{`printf '%s' '{"status":"dispatched","results":[]}'; exit 1`, "pending"},
	} {
		output, err := process.Run("/usr/bin/sh", []string{"-c", test.script}, nil)
		result := readInstalledContinuation(output, err)
		if result.Status != test.status {
			t.Fatalf("process outcome: %+v, %v", result, err)
		}
		if test.status == "pending" && strings.Contains(test.script, "exit 2") &&
			(len(output) == 0 || err == nil || err.Error() != "CORE_PENDING_RESULT") {
			t.Fatal("adapter discarded pending output or exit convention")
		}
	}
}

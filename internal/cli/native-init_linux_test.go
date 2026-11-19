package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestNativeSecretCommandKeepsSelectedCredentialPrivate(t *testing.T) {
	t.Setenv("GH_TOKEN", "unrelated-environment-login")
	t.Setenv("GH_DEBUG", "api")
	token := []byte("synthetic-selected-account")
	cmd := nativeSecretCommand(context.Background(), "/fixed/gh", "example/project", token)
	input, _ := io.ReadAll(cmd.Stdin)
	if !bytes.Equal(input, token) || strings.Contains(strings.Join(cmd.Args, " "),
		string(token)) || cmd.Stdout != nil || cmd.Stderr != nil {
		t.Fatal("plaintext escaped its private process inputs")
	}
	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "GH_TOKEN="+string(token)) ||
		strings.Contains(env, "unrelated-environment-login") ||
		strings.Contains(env, "GH_DEBUG") {
		t.Fatal("selected credential was overridden or diagnostic tracing inherited")
	}
	if !workflowScope("") || !workflowScope("repo, workflow") || workflowScope("repo") {
		t.Fatal("OAuth scope detection changed")
	}
}

func TestNativeContinuationValidatesResultAndExitConvention(t *testing.T) {
	for _, status := range []string{"dispatched", "paused", "pending", "denied"} {
		for index, failure := range []error{nil, errors.New("CORE_PENDING_RESULT"),
			errors.New("PROCESS_RESULT_UNAVAILABLE")} {
			body := []byte(`{"status":"` + status + `","results":[]}`)
			_, err := decodeNativeContinuation(body, failure)
			valid := status == "pending" && index == 1 ||
				(status == "dispatched" || status == "paused") && index == 0
			if (err == nil) != valid {
				t.Fatal("invalid status/exit matrix", status, index, err)
			}
		}
	}
	for _, status := range []string{"dispatched", "paused", "pending"} {
		var err error
		if status == "pending" {
			err = errors.New("CORE_PENDING_RESULT")
		}
		body := []byte(`{"status":"` + status + `","results":[]}`)
		result, failure := decodeNativeContinuation(body, err)
		if failure != nil || result.Status != status {
			t.Fatal(result, failure)
		}
	}
	for _, body := range []string{"", "{}", `{"status":"dispatched"}`,
		`{"status":"denied","results":[]}`, `{"status":"pending","results":null}`} {
		if _, err := decodeNativeContinuation([]byte(body), nil); err == nil {
			t.Fatal("malformed completion accepted", body)
		}
	}
	if _, err := decodeNativeContinuation([]byte(`{"status":"dispatched","results":[]}`),
		errors.New("CORE_PENDING_RESULT")); err == nil {
		t.Fatal("contradictory exit status accepted")
	}
}

func TestNativeResultSeparatesInstallationFromContinuation(t *testing.T) {
	for _, status := range []string{"dispatched", "paused", "pending"} {
		t.Run(status, func(t *testing.T) {
			var out bytes.Buffer
			ops := managedHostOperations{native: func(InitPlan, []sharedRegistration) (
				nativeContinuation, error) {
				return nativeContinuation{Status: status, Results: []json.RawMessage{}}, nil
			}}
			err := publishManagedRoute(json.NewEncoder(&out), InitPlan{NativeManaged: true},
				nil, ops)
			var result map[string]any
			if json.Unmarshal(out.Bytes(), &result) != nil || result["status"] != "completed" ||
				result["credential"] != "configured_unverified" {
				t.Fatal(out.String(), err)
			}
			if (err != nil) != (status == "pending") {
				t.Fatal("continuation result changed installation outcome", err)
			}
		})
	}
}

func TestNativeManagedInitResumesHostWithoutRepeatingCompletedWrites(t *testing.T) {
	base, plan, merge := managedResumeFixture(t)
	plan.NativeManaged = true
	api := &nativeWakeAPI{fakeAPI: base}
	app := App{API: api}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	api.writeLost = true
	err := app.resumeManagedInit(ctx, plan, nil, &out, managedHostOperations{})
	if err == nil || err.Error() != "NATIVE_WAKE_SECRET_WRITE_UNKNOWN" || out.Len() != 0 {
		t.Fatal("unknown credential setup must precede and hold caller publication", err)
	}
	api.writeLost = false
	err = app.resumeManagedInit(ctx, plan, nil, &out, managedHostOperations{})
	var pending *CommandError
	if !errors.As(err, &pending) || pending.Code != "MANAGED_INSTALL_AWAITING_PR_MERGE" ||
		api.secretWrites != 2 {
		t.Fatal("native setup must complete before the PR wait", err, api.secretWrites)
	}
	merge()
	host := nativeFixtureHost(t, api)
	continuations := 0
	ops := managedHostOperations{
		discover: func() (*sharedHost, error) { return host, nil },
		cleanup:  func(InitPlan, []sharedRegistration) error { return nil },
		native: func(plan InitPlan, runners []sharedRegistration) (nativeContinuation, error) {
			return activateNativeRoute(api, plan, runners, nativeActivationOperations{
				discover: func() (*sharedHost, error) { return host, nil },
				recheck:  func(sharedHost) error { return nil },
				complete: func() ([]byte, error) {
					continuations++
					if continuations == 1 {
						return nil, errors.New("PROCESS_RESULT_UNAVAILABLE")
					}
					return []byte(`{"status":"paused","results":[]}`), nil
				},
			})
		},
		prepare: func(InitPlan) error { t.Fatal("repeated host preparation"); return nil },
		credentials: func(int64, []byte) error {
			t.Fatal("repeated credential transfer")
			return nil
		},
	}
	ready := context.Background()
	if err := app.resumeManagedInit(ready, plan, nil, &out, ops); err == nil {
		t.Fatal("lost continuation response must remain pending")
	}
	if err := app.resumeManagedInit(ready, plan, nil, &out, ops); err != nil {
		t.Fatal(err)
	}
	if api.secretWrites != 2 || continuations != 2 || len(api.writes) != 2 {
		t.Fatal("confirmed writes were repeated", api.secretWrites, api.writes)
	}
	// Current installation state is sufficient even without local receipts.
	// Verify both a prepared disabled route and an already activated route.
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range []string{"false", "true"} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		api.data["repos/example/project/actions/variables/"+sharedPoolVariable] =
			actionsVariable{Name: sharedPoolVariable, Value: pool}
		if err := app.resumeManagedInit(ready, plan, nil, &out, ops); err != nil {
			t.Fatal("missing receipt must reuse the actual configured installation", err)
		}
	}
	if api.secretWrites != 2 || continuations != 4 || len(api.writes) != 3 {
		t.Fatal("missing local record restarted completed installation", api.writes)
	}
}

func TestNativeResumePreservesSuccessfulPublicationAcrossMetadataFailure(t *testing.T) {
	base, plan, _ := managedResumeFixture(t)
	plan.NativeManaged = true
	api := &nativeWakeAPI{fakeAPI: base, readAfterWriteFailure: true}
	app := App{API: api}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := app.resumeManagedInit(ctx, plan, nil, &out,
		managedHostOperations{}); err == nil || api.secretWrites != 1 {
		t.Fatal("expected failed metadata observation after successful publication", err)
	}
	api.readFailure, api.readAfterWriteFailure = "", false
	for range 2 {
		err := app.resumeManagedInit(ctx, plan, nil, &out, managedHostOperations{})
		var pending *CommandError
		if !errors.As(err, &pending) || pending.Code != "MANAGED_INSTALL_AWAITING_PR_MERGE" ||
			api.secretWrites != 1 {
			t.Fatal("confirmed publication was repeated during observation recovery", err)
		}
	}
}

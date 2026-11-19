package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type nativeResumeProvider struct {
	*nativeSetupAPI
	publisher *fixtureSecretPublisher
	t         *testing.T
}

func (p nativeResumeProvider) PublishWakeSecret(repo string) error {
	return p.publisher.PublishWakeSecret(repo)
}

func (p nativeResumeProvider) Call(method, path string, input, output any) error {
	if strings.Contains(path, "/pulls") && p.publisher.calls != 1 {
		p.t.Fatal("caller publication/readback preceded credential preparation")
	}
	return p.nativeSetupAPI.Call(method, path, input, output)
}

func TestNativeResumePreparesBeforePRAndReusesReadyInstallation(t *testing.T) {
	fixture, plan, merge := managedResumeFixture(t)
	plan.NativeManaged = true
	api := &nativeSetupAPI{fakeAPI: fixture}
	api.data["repos/example/project/actions/permissions"] = map[string]bool{
		"enabled": true,
	}
	publisher := &fixtureSecretPublisher{api: api}
	app := App{API: nativeResumeProvider{api, publisher, t}}
	var output bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := app.resumeManagedInit(ctx, plan, []byte("fixture"), &output,
		managedHostOperations{})
	var pending *CommandError
	if !errors.As(err, &pending) || pending.Code != "MANAGED_INSTALL_AWAITING_PR_MERGE" {
		t.Fatalf("expected pending PR after native preparation: %v", err)
	}
	merge()
	// Native repository admission does not invent branch protection as a
	// prerequisite. The exact caller files still have to match the selected source.
	branch := api.data["repos/example/project/branches/main"].(map[string]any)
	branch["protected"] = false
	host, runners := nativeReadyHost(t)
	labels := []struct {
		Name string `json:"name"`
	}{
		{"nibfin-user-7"}, {"nibfin-shared"},
	}
	updateRunners := func(first, second string) {
		api.data["repos/example/project/actions/runners?per_page=100&page=1"] =
			map[string]any{"total_count": 2, "runners": []RunnerInfo{
				{ID: 25, Name: "slot-1", OS: "Linux", Status: first, Labels: labels},
				{ID: 26, Name: "slot-2", OS: "Linux", Status: second, Labels: labels},
			}}
	}
	var installed *sharedHost
	preparations, registrations, credentials, starts := 0, 0, 0, 0
	complete := 0
	ops := managedHostOperations{
		discover:    func() (*sharedHost, error) { return installed, nil },
		prepare:     func(InitPlan) error { preparations++; return nil },
		credentials: func(int64, []byte) error { credentials++; return nil },
		stage:       func(InitPlan) ([]string, error) { return []string{"one", "two"}, nil },
		register: func(InitPlan, []string) ([]sharedRegistration, error) {
			registrations++
			return runners, nil
		},
		adopt: func(InitPlan, []sharedRegistration) (*sharedHost, error) {
			installed = host
			return host, nil
		},
		staged: func(_ InitPlan, live []sharedRegistration) ([]string, error) {
			if live[0].Status != "online" || live[1].Status != "offline" {
				t.Fatal("partial service readiness was not reobserved")
			}
			return nil, nil
		},
		start: func(paths []string) error {
			starts++
			if starts == 1 {
				updateRunners("online", "offline")
				return errors.New("fixture partial start")
			}
			if len(paths) != 0 {
				t.Fatal("must reuse installed slots")
			}
			updateRunners("online", "online")
			return nil
		},
		cleanup: func(InitPlan, []sharedRegistration) error { return nil },
		complete: func() ([]byte, error) {
			complete++
			if complete == 1 {
				return nil, errors.New("fixture lost completion response")
			}
			return []byte(`{"status":"dispatched","results":[]}`), nil
		},
	}
	for attempt := range 3 {
		err := app.resumeManagedInit(context.Background(), plan, []byte("fixture"),
			&output, ops)
		if attempt == 0 {
			if err == nil || err.Error() != "fixture partial start" || complete != 0 {
				t.Fatalf("partial start must precede continuation: %v", err)
			}
		} else if attempt == 1 {
			if !errors.As(err, &pending) || pending.Code != "INSTALLED_CONTINUATION_PENDING" {
				t.Fatalf("ready installation should retain pending continuation: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if publisher.calls != 1 || complete != 2 || starts != 2 || preparations != 1 ||
		registrations != 1 || credentials != 1 {
		t.Fatal("continuation retry must preserve configuration, PR and installed slots")
	}
	if !strings.Contains(output.String(), `"installation":"ready"`) {
		t.Fatal("installation readiness missing from continuation result")
	}
}

func TestNativeResumeMissingIntentObservesProviderWithoutReplacingSecret(t *testing.T) {
	fixture, plan, merge := managedResumeFixture(t)
	merge()
	plan.NativeManaged = true
	api := &nativeSetupAPI{fakeAPI: fixture}
	api.data["repos/example/project/actions/permissions"] = map[string]bool{
		"enabled": true,
	}
	api.data["repos/example/project/actions/secrets/"+nativeWakeSecret] =
		nativeSecretMetadata{Name: nativeWakeSecret, UpdatedAt: "unattributed"}
	host, _ := nativeReadyHost(t)
	observed, err := observedNativeManagedRoute(api, *host)
	if err != nil || !observed {
		t.Fatalf("provider route observation: %v", err)
	}
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixture must have no local intent")
	}
	publisher := &fixtureSecretPublisher{api: api}
	app := App{API: nativeResumeProvider{api, publisher, t}}
	var output bytes.Buffer
	err = app.resumeManagedInit(context.Background(), plan, nil, &output,
		managedHostOperations{})
	if err == nil || err.Error() != "NATIVE_SECRET_CONFLICT" || publisher.calls != 0 {
		t.Fatalf("provider conflict must preserve the unattributed secret: %v", err)
	}
}

func TestNativeResumeReadyWithoutIntentRetriesCompletion(t *testing.T) {
	fixture, plan, merge := managedResumeFixture(t)
	merge()
	plan.NativeManaged = true
	api := &nativeSetupAPI{fakeAPI: fixture}
	base := "repos/example/project/actions/"
	api.data[base+"permissions"] = map[string]bool{"enabled": true}
	api.data[base+"secrets/"+nativeWakeSecret] =
		nativeSecretMetadata{Name: nativeWakeSecret, UpdatedAt: "unattributed"}
	api.data[base+"variables/"+sharedPoolVariable] =
		actionsVariable{Name: sharedPoolVariable, Value: "true"}
	api.data[base+"variables/"+sharedDestinationVariable] = actionsVariable{
		Name:  sharedDestinationVariable,
		Value: `{"repository":"example/project","repositoryId":10}`,
	}
	host, _ := nativeReadyHost(t)
	labels := []struct {
		Name string `json:"name"`
	}{{"nibfin-user-7"}, {"nibfin-shared"}}
	api.data[base+"runners?per_page=100&page=1"] = map[string]any{
		"total_count": 2, "runners": []RunnerInfo{
			{ID: 25, Name: "slot-1", OS: "Linux", Status: "online", Labels: labels},
			{ID: 26, Name: "slot-2", OS: "Linux", Status: "online", Labels: labels},
		},
	}
	publisher := &fixtureSecretPublisher{api: api}
	app := App{API: nativeResumeProvider{api, publisher, t}}
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
	var output bytes.Buffer
	for attempt := range 2 {
		err := app.resumeManagedInit(context.Background(), plan, nil, &output, ops)
		var pending *CommandError
		if attempt == 0 {
			if !errors.As(err, &pending) || pending.Code != "INSTALLED_CONTINUATION_PENDING" {
				t.Fatalf("expected pending continuation: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range []func(){
		func() { host.MemberID++ },
		func() {
			host.MemberID--
			api.data[base+"variables/"+sharedDestinationVariable] = actionsVariable{
				Name: sharedDestinationVariable, Value: `{}`,
			}
		},
	} {
		change()
		err := app.resumeManagedInit(context.Background(), plan, nil, &output, ops)
		if err == nil || err.Error() != "NATIVE_SECRET_CONFLICT" || calls != 2 {
			t.Fatalf("unverified installation must preserve secret and refuse: %v", err)
		}
		// The ordinary unfinished setup may create an intent, never a new secret.
		path, err := managedIntentPath(plan.Repository)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	if calls != 2 || publisher.calls != 0 || managedIntentAvailable(plan.Repository) {
		t.Fatal("ready recovery must reuse provider effects without recreating intent")
	}
	for _, event := range api.events {
		if !strings.HasPrefix(event, "GET ") {
			t.Fatalf("ready recovery mutated provider configuration: %s", event)
		}
	}
	if !strings.Contains(output.String(), `"credential":"configured_unverified"`) ||
		!strings.Contains(output.String(), `"status":"paused"`) {
		t.Fatal("configuration observation and paused continuation must remain distinct")
	}
}

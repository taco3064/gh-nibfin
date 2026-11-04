package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prepareRegisteredRunner(t *testing.T) (App, *runnerAPI, *runnerHostFixture, string) {
	t.Helper()
	app, api, host, directory := runnerFixture(t)
	for _, command := range []string{"create", "register"} {
		if _, err := runRunnerTest(app, command, directory, true); err != nil {
			t.Fatal(err)
		}
	}
	return app, api, host, directory
}

func TestRemoteRemovalReadsBackLostWriteAndRetainsLocalConfiguration(t *testing.T) {
	app, api, host, directory := prepareRegisteredRunner(t)
	api.deleteLost = true
	for i := 0; i < 2; i++ {
		if _, err := runRunnerTest(app, "remove", directory, true); err != nil {
			t.Fatal(err)
		}
	}
	if api.deletes != 1 || host.calls != 1 {
		t.Fatal("remote removal must not repeat a confirmed write or invoke local config")
	}
	if _, err := os.Stat(filepath.Join(directory, ".runner")); err != nil {
		t.Fatal("remote removal must retain local configuration")
	}
	if _, err := runRunnerTest(app, "unregister", directory, true); err != nil {
		t.Fatal(err)
	}
	if api.tokens != 1 || host.calls != 2 {
		t.Fatal("local-only cleanup must not issue another registration/removal token")
	}
}

func TestWrongMemberBusyRunnerAndExternalServiceAreNotRemoved(t *testing.T) {
	for _, kind := range []string{"member", "busy", "online", "service"} {
		app, api, host, directory := prepareRegisteredRunner(t)
		command := "remove"
		switch kind {
		case "member":
			api.runners[0].Labels[0].Name = "nibfin-user-999"
		case "busy":
			api.runners[0].Busy = true
		case "online":
			api.runners[0].Status = "online"
		case "service":
			command = "unregister"
			path := filepath.Join(directory, ".service")
			if os.WriteFile(path, []byte("external"), 0600) != nil {
				t.Fatal("cannot prepare service fixture")
			}
		}
		if _, err := runRunnerTest(app, command, directory, true); err == nil {
			t.Fatal("unrelated or active Runner must reject")
		}
		if api.deletes != 0 || host.calls != 1 || api.tokens != 1 {
			t.Fatal("rejected management cannot mutate the Runner")
		}
	}
}

func TestPartialUnregisterIsPendingAndDoesNotRetry(t *testing.T) {
	app, api, host, directory := prepareRegisteredRunner(t)
	host.keepLocal = true
	for i := 0; i < 2; i++ {
		if _, err := runRunnerTest(app, "unregister", directory, true); err == nil {
			t.Fatal("partial local cleanup cannot succeed")
		}
	}
	if host.calls != 2 || api.tokens != 2 {
		t.Fatal("uncertain unregister must not be retried")
	}
	if _, err := os.Stat(filepath.Join(directory, ".runner")); err != nil {
		t.Fatal("unresolved local configuration must be retained")
	}
}

func TestRunnerChildEnvironmentContainsOnlyItsShortLivedToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "private-github")
	t.Setenv("OPENAI_API_KEY", "private-model")
	t.Setenv("ACTIONS_RUNNER_INPUT_PAT", "private-admin")
	t.Setenv("LD_PRELOAD", "untrusted-runtime")
	for _, item := range runnerEnvironment(map[string]string{
		"TOKEN": runnerTokenSentinel, "UNATTENDED": "true"}) {
		if strings.Contains(item, "private-") || strings.Contains(item, "untrusted-runtime") {
			t.Fatal("official configuration must not inherit admin/model/runtime credentials")
		}
	}
}

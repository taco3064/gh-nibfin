package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestInitRunnerPreviewRegistrationAndRepeat(t *testing.T) {
	app, api, host, directory := runnerFixture(t)
	plan, err := planInit(api, "example/project")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := app.planInitRunner(plan, directory, "first-runner")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	args := []string{"init", "--repo", "example/project", "--runner-directory",
		directory, "--runner-name", "first-runner"}
	if err := app.Run(args, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview must not create a Runner directory")
	}
	if api.tokens != 0 || host.calls != 0 {
		t.Fatal("preview must not register a Runner")
	}
	for i := 0; i < 2; i++ {
		if err := app.applyInitRunner(plan, *runner, &output); err != nil {
			t.Fatal(err)
		}
	}
	if api.tokens != 1 || host.calls != 1 {
		t.Fatal("repeat must read back the existing registration without a new token")
	}
	if strings.Contains(output.String(), runnerTokenSentinel) ||
		!strings.Contains(output.String(), `"host_adoption":"required"`) {
		t.Fatal("result must preserve host-adoption status without exposing credentials")
	}
}

func TestInitRunnerDoesNotRetryUnknownRegistration(t *testing.T) {
	app, api, host, directory := runnerFixture(t)
	plan, err := planInit(api, "example/project")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := app.planInitRunner(plan, directory, "first-runner")
	if err != nil {
		t.Fatal(err)
	}
	host.lost = true
	host.keepLocal = true
	var output bytes.Buffer
	for i := 0; i < 2; i++ {
		var pending *CommandError
		err := app.applyInitRunner(plan, *runner, &output)
		if !errors.As(err, &pending) || pending.Exit != 2 {
			t.Fatalf("setup attempt %d must stay Pending, got %v", i+1, err)
		}
	}
	if api.tokens != 1 || host.calls != 1 {
		t.Fatal("setup must not request a second registration token")
	}
}

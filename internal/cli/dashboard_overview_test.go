package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type observationProcess struct {
	t       *testing.T
	calls   int
	failure bool
	legacy  bool
}

func (p *observationProcess) Run(name string, args []string,
	input []byte) ([]byte, error) {
	p.calls++
	if name == "gh" {
		expected := []string{"auth", "token", "--hostname", "github.com"}
		if !reflect.DeepEqual(args, expected) || input != nil {
			p.t.Fatal("unexpected credential request")
		}
		return []byte("private-test-credential\n"), nil
	}
	if name != "/trusted/node" || len(args) != 2 || args[0] != "/trusted/core" {
		p.t.Fatal("unexpected process", name, args)
	}
	if args[1] == "help" {
		if p.legacy {
			return []byte(`{"commands":["contract"]}`), nil
		}
		return []byte(`{"commands":[
		"observe-local-work (private JSON stdin, no arguments)"]}`), nil
	}
	if args[1] != "observe-local-work" {
		p.t.Fatal("administrative command invoked", args)
	}
	var body map[string]string
	if json.Unmarshal(input, &body) != nil || len(body) != 1 ||
		body["token"] != "private-test-credential" {
		p.t.Fatal("wrong private input")
	}
	if p.failure {
		return []byte("private-test-credential /private/diagnostic"),
			errors.New("private-test-credential")
	}
	return []byte(`{"schemaVersion":1,"status":"observed",
	"subject":{"id":1,"login":"reader"},"completeness":"complete",
	"interval":{"startedAt":"2026-11-18T00:00:00Z",
	"finishedAt":"2026-11-18T00:00:01Z"},"runners":[],"work":[],"errors":[]}`), nil
}

func TestDashboardOverviewPrivateAdapter(t *testing.T) {
	for _, mode := range []string{"success", "failure", "legacy"} {
		p := &observationProcess{t: t, failure: mode == "failure", legacy: mode == "legacy"}
		o := dashboardObserver{process: p, subject: snapshot.Subject{ID: 1, Login: "reader"},
			resolve: func() (string, string, error) {
				return "/trusted/node", "/trusted/core", nil
			}}
		result := o.Observe()
		if (result.Status == "observed") != (mode == "success") {
			t.Fatal(result)
		}
		data, _ := json.Marshal(result)
		if strings.Contains(string(data), "private-test") {
			t.Fatal("credential/diagnostic leaked")
		}
		if mode == "legacy" && p.calls != 1 {
			t.Fatal("legacy Core received credential")
		}
	}
}

func TestDashboardOverviewPublicEntry(t *testing.T) {
	for _, args := range [][]string{{"--overview", "--repo", "example/project"},
		{"--overview", "extra"}, {"--overview=invalid"}} {
		if (App{}).Run(append([]string{"dashboard"}, args...), io.Discard) == nil {
			t.Fatal("conflicting arguments accepted", args)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := readableDashboardAPI()
	fake := &dashboardFake{done: make(chan struct{})}
	app := App{
		Context: ctx, API: api,
		StartDashboard: func(_ context.Context, config dashboard.Config) (
			DashboardService, error) {
			if config.Repository != "" || config.Overview == nil || config.Provider == nil {
				t.Fatal("wrong overview composition")
			}
			return fake, nil
		}, OpenDashboard: func(context.Context, string) error {
			cancel()
			return nil
		}}
	if err := app.Run([]string{"dashboard", "--overview"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(api.calls, []string{"user"}) || fake.closed != 1 {
		t.Fatal("overview inferred repository or did not close", api.calls)
	}
}

func TestDashboardOverviewBrowserHandoff(t *testing.T) {
	target := "http://127.0.0.1:1234/#" + strings.Repeat("a", 64) + ":overview"
	if _, _, err := dashboardBrowserCommand("linux", target); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dashboardBrowserCommand("linux", target+":other"); err == nil {
		t.Fatal("unbounded mode accepted")
	}
}

func TestDashboardOverviewDetailRejectsChangedSubject(t *testing.T) {
	api := readableDashboardAPI()
	provider := dashboardOverviewRepository{api: api,
		subject: snapshot.Subject{ID: 9, Login: "other"}}
	value := provider.Collect("example/project")
	if value.Repository.Status.Availability != snapshot.Unknown ||
		len(api.calls) != 1 || api.calls[0] != "user" {
		t.Fatal("changed subject reached repository reads", api.calls)
	}
}

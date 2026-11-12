package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type dashboardAPI struct {
	user, repo string
	fail       string
	message    string
	calls      []string
}

func (a *dashboardAPI) Call(method, path string, input, output any) error {
	a.calls = append(a.calls, method+" "+path)
	if method != "GET" || input != nil {
		panic("dashboard wrote provider data")
	}
	if path == a.fail {
		if a.message != "" {
			return errors.New(a.message)
		}
		return errors.New("secret /private/provider")
	}
	data := `[]`
	switch {
	case path == "user":
		data = a.user
	case path == "repos/owner/project":
		data = a.repo
	case strings.Contains(path, "/pulls"):
		data = `[]`
	case strings.Contains(path, "/actions/runs"):
		data = `{"total_count":0,"workflow_runs":[]}`
	}
	return json.Unmarshal([]byte(data), output)
}

func readableDashboardAPI() *dashboardAPI {
	return &dashboardAPI{user: `{"id":1,"login":"reader","type":"User"}`,
		repo: `{"id":2,"full_name":"owner/project","permissions":{"pull":true}}`}
}

type dashboardProcess struct {
	calls int
	data  []byte
	err   error
}

func (p *dashboardProcess) Run(name string, args []string, input []byte) ([]byte, error) {
	p.calls++
	if name != "gh" || !reflect.DeepEqual(args,
		[]string{"repo", "view", "--json", "nameWithOwner"}) || input != nil {
		panic("unexpected discovery process")
	}
	return p.data, p.err
}

type fakeDashboard struct {
	done   chan struct{}
	closed int
	err    error
}

func (s *fakeDashboard) URL() string           { return "http://127.0.0.1:1234" }
func (s *fakeDashboard) BootstrapURL() string  { return s.URL() + "/#secret" }
func (s *fakeDashboard) Done() <-chan struct{} { return s.done }
func (s *fakeDashboard) Close() error          { s.closed++; return s.err }

func TestDashboardArgumentsAndIdentity(t *testing.T) {
	for _, args := range [][]string{
		{"--help"}, {"-h"}, {"--unknown"}, {"extra"}, {"--repo", ""},
		{"--repo", "../bad"}, {"--repo", "owner/project", "extra"},
	} {
		a := App{}
		err := a.Run(append([]string{"dashboard"}, args...), io.Discard)
		if strings.Contains(args[0], "help") || args[0] == "-h" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
	for _, tc := range []struct{ user, repo, fail string }{
		{`{}`, "", ""},
		{`{"id":1,"login":"bad-","type":"User"}`, "", ""},
		{`{"id":1,"login":"bad/path","type":"User"}`, "", ""},
		{"", "", "user"}, {"", "", "repos/owner/project"},
		{"", `{}`, ""}, {"", `{"id":2,"full_name":"other/repo"}`, ""},
		{"", `{"id":2,"full_name":"owner/project"}`, ""},
		{"", `{"id":2,"full_name":"owner/project","permissions":{"pull":false}}`, ""},
	} {
		api := readableDashboardAPI()
		if tc.user != "" {
			api.user = tc.user
		}
		if tc.repo != "" {
			api.repo = tc.repo
		}
		api.fail = tc.fail
		err := (App{API: api}).Run([]string{"dashboard", "--repo", "owner/project"},
			io.Discard)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
	}
}

func TestDashboardSelectionAndLifecycle(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		process := &dashboardProcess{data: []byte(`{"nameWithOwner":"owner/project"}`)}
		service := &fakeDashboard{done: make(chan struct{})}
		starts, opens := 0, 0
		runtime := &DashboardRuntime{
			Start: func(_ context.Context, c dashboard.Config) (DashboardSession, error) {
				starts++
				if c.Repository != "owner/project" || c.Bind != "" || c.Provider == nil {
					t.Fatal("wrong composition", c)
				}
				return service, nil
			},
			Open: func(_ context.Context, target string) error {
				opens++
				if target != service.BootstrapURL() {
					t.Fatal("wrong bootstrap")
				}
				cancel()
				return nil
			},
		}
		args := []string{"dashboard"}
		if explicit {
			args = append(args, "--repo", "owner/project")
		}
		var out bytes.Buffer
		err := (App{API: readableDashboardAPI(), Process: process,
			Dashboard: runtime}).RunContext(ctx, args, &out)
		cancel()
		if err != nil || starts != 1 || opens != 1 || service.closed != 1 {
			t.Fatal(err, starts, opens, service.closed)
		}
		if explicit && process.calls != 0 || !explicit && process.calls != 1 {
			t.Fatal("discovery count", process.calls)
		}
		if strings.Contains(out.String(), "secret") ||
			!strings.Contains(out.String(), "Ctrl+C") {
			t.Fatal("unsafe or missing output")
		}
	}
	for _, data := range []string{`{}`, `{"nameWithOwner":"../bad"}`, `not json`} {
		p := &dashboardProcess{data: []byte(data)}
		err := (App{Process: p}).Run([]string{"dashboard"}, io.Discard)
		if err == nil || err.Error() != "REPOSITORY_UNRESOLVED_USE_--repo" {
			t.Fatal(err)
		}
	}
}

func TestDashboardFailuresAndCancellation(t *testing.T) {
	for _, mode := range []string{"start", "open", "close", "serve", "cancel-start"} {
		ctx, cancel := context.WithCancel(context.Background())
		service := &fakeDashboard{done: make(chan struct{})}
		opens := 0
		runtime := &DashboardRuntime{
			Start: func(context.Context, dashboard.Config) (DashboardSession, error) {
				if mode == "start" {
					return nil, errors.New("secret")
				}
				if mode == "cancel-start" {
					cancel()
				}
				if mode == "serve" {
					close(service.done)
				}
				return service, nil
			},
			Open: func(context.Context, string) error {
				opens++
				if mode == "open" {
					return errors.New("secret /private/path")
				}
				if mode == "close" {
					service.err = errors.New("secret")
					cancel()
				}
				return nil
			},
		}
		err := (App{API: readableDashboardAPI(), Dashboard: runtime}).RunContext(ctx,
			[]string{"dashboard", "--repo", "owner/project"}, io.Discard)
		cancel()
		if mode == "cancel-start" || mode == "serve" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
		if mode != "start" && service.closed != 1 {
			t.Fatal("service leaked")
		}
		if (mode == "start" || mode == "cancel-start") && opens != 0 {
			t.Fatal("opened after failed/canceled start")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (App{}).RunContext(ctx, []string{"dashboard"}, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardRealComposition(t *testing.T) {
	for _, failure := range []string{"", "PERMISSION_DENIED", "offline"} {
		ctx, cancel := context.WithCancel(context.Background())
		api := readableDashboardAPI()
		var service *dashboard.Service
		var bootstrap string
		runtime := &DashboardRuntime{
			Start: func(ctx context.Context, c dashboard.Config) (DashboardSession, error) {
				var err error
				service, err = dashboard.Start(ctx, c)
				return service, err
			},
			Open: func(_ context.Context, target string) error {
				bootstrap = target
				u, err := url.Parse(target)
				if err != nil || "http://"+u.Host != service.URL() {
					t.Fatal("authority")
				}
				if failure != "" {
					api.fail = "repos/owner/project"
					api.message = failure
				}
				for _, proof := range []string{"", strings.Repeat("0", 64), u.Fragment} {
					req, _ := http.NewRequest("GET", service.URL()+"/api/snapshot", nil)
					if proof != "" {
						req.Header.Set(dashboard.SessionHeader, proof)
					}
					response, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(response.Body)
					response.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if proof != u.Fragment {
						if response.StatusCode != 401 {
							t.Fatal(response.StatusCode)
						}
						continue
					}
					var result snapshot.Snapshot
					if response.StatusCode != 200 || json.Unmarshal(data, &result) != nil ||
						result.SchemaVersion != 1 {
						t.Fatal("missing snapshot", string(data))
					}
					if failure == "" && (len(result.Repository.Items) != 1 ||
						result.Repository.Items[0].Name != "owner/project") {
						t.Fatal(result)
					}
					if failure == "PERMISSION_DENIED" &&
						result.Repository.Status.Availability != snapshot.Denied {
						t.Fatal("denial lost its typed status")
					}
					if failure != "" && result.Repository.Status.Availability == snapshot.Known {
						t.Fatal("unavailable provider became success")
					}
				}
				cancel()
				return nil
			},
		}
		var out bytes.Buffer
		err := (App{API: api, Dashboard: runtime}).RunContext(ctx,
			[]string{"dashboard", "--repo", "owner/project"}, &out)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), strings.Split(bootstrap, "#")[1]) {
			t.Fatal("proof leaked")
		}
		if service.BootstrapURL() != "" {
			t.Fatal("proof remained live")
		}
		l, err := net.Listen("tcp", strings.TrimPrefix(service.URL(), "http://"))
		if err != nil {
			t.Fatal("listener not reusable", err)
		}
		l.Close()
	}
}

func TestDashboardBrowserArguments(t *testing.T) {
	target := "http://127.0.0.1:1234/#" + strings.Repeat("a", 64)
	for platform, executable := range map[string]string{
		"linux": "xdg-open", "darwin": "open", "windows": "explorer.exe",
	} {
		name, args, err := browserCommand(platform, target)
		if err != nil || name != executable || !reflect.DeepEqual(args, []string{target}) {
			t.Fatal(name, args, err)
		}
	}
	for _, target := range []string{"https://evil/", "http://localhost:1234/#secret",
		"http://127.0.0.1:1234/", "http://user@127.0.0.1:1234/#secret"} {
		if _, _, err := browserCommand("linux", target); err == nil {
			t.Fatal(target)
		}
	}
}

// Keep all command waits bounded even if a regression loses cancellation.
func TestDashboardServingCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	service := &fakeDashboard{done: make(chan struct{}), err: errors.New("private")}
	close(service.done)
	runtime := &DashboardRuntime{
		Start: func(context.Context, dashboard.Config) (DashboardSession, error) {
			return service, nil
		},
		Open: func(context.Context, string) error { return nil },
	}
	err := (App{API: readableDashboardAPI(), Dashboard: runtime}).RunContext(ctx,
		[]string{"dashboard", "--repo", "owner/project"}, io.Discard)
	if err == nil || err.Error() != "DASHBOARD_SHUTDOWN_FAILED" {
		t.Fatal(err)
	}
}

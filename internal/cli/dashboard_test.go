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

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type dashboardAPI struct {
	user, repo string
	failure    string
	calls      []string
}

func (f *dashboardAPI) Call(method, path string, input, output any) error {
	if method != "GET" || input != nil {
		panic("dashboard write")
	}
	f.calls = append(f.calls, path)
	if f.failure != "" {
		return errors.New(f.failure)
	}
	data := f.repo
	if path == "user" {
		data = f.user
	}
	if strings.Contains(path, "?") {
		return errors.New("PERMISSION_DENIED")
	}
	return json.Unmarshal([]byte(data), output)
}

func readableDashboardAPI() *dashboardAPI {
	return &dashboardAPI{user: `{"id":1,"login":"reader","type":"User"}`,
		repo: `{"id":2,"full_name":"example/project","permissions":{"pull":true}}`}
}

type dashboardProcess struct {
	t     *testing.T
	data  string
	err   error
	calls int
}

func (p *dashboardProcess) Run(name string, args []string, input []byte) ([]byte, error) {
	p.calls++
	if name != "gh" || !reflect.DeepEqual(args,
		[]string{"repo", "view", "--json", "nameWithOwner"}) || input != nil {
		p.t.Fatal("unexpected discovery command", name, args)
	}
	return []byte(p.data), p.err
}

type dashboardFake struct {
	done     chan struct{}
	closed   int
	closeErr error
}

func (s *dashboardFake) URL() string { return "http://127.0.0.1:1234" }
func (s *dashboardFake) BootstrapURL() string {
	return s.URL() + "/#" + strings.Repeat("a", 64)
}
func (s *dashboardFake) Done() <-chan struct{} { return s.done }
func (s *dashboardFake) Close() error          { s.closed++; return s.closeErr }

func TestDashboardParsingAndIdentityFailures(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--bad"}, {"extra"},
		{"--repo", ""}, {"--repo", "../bad"}, {"--repo"}} {
		a := App{}
		err := a.Run(append([]string{"dashboard"}, args...), io.Discard)
		if strings.Contains(strings.Join(args, " "), "help") || args[0] == "-h" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
	for _, change := range []func(*dashboardAPI){
		func(f *dashboardAPI) { f.failure = "secret /private/path" },
		func(f *dashboardAPI) { f.user = `{}` },
		func(f *dashboardAPI) { f.user = `{"id":1,"login":"bad/name","type":"User"}` },
		func(f *dashboardAPI) { f.repo = `{}` },
		func(f *dashboardAPI) { f.repo = `{"id":2,"full_name":"other/project"}` },
		func(f *dashboardAPI) {
			f.repo = `{"id":2,"full_name":"example/project","permissions":{"push":true}}`
		},
	} {
		f := readableDashboardAPI()
		change(f)
		a := App{API: f, StartDashboard: func(context.Context, dashboard.Config) (
			DashboardService, error) {
			t.Fatal("started without identity")
			return nil, nil
		}}
		err := a.Run([]string{"dashboard", "--repo", "example/project"}, io.Discard)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe identity result", err)
		}
	}
}

func TestDashboardDiscoveryAndLifecycleFailures(t *testing.T) {
	for _, mode := range []string{"discovery", "start", "open", "close", "serve",
		"cancel-before", "cancel-start", "success"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &dashboardProcess{t: t, data: `{"nameWithOwner":"example/project"}`}
			s := &dashboardFake{done: make(chan struct{})}
			starts, opens := 0, 0
			if mode == "discovery" {
				p.data = `{"nameWithOwner":"bad"}`
			}
			if mode == "cancel-before" {
				cancel()
			}
			a := App{Context: ctx, API: readableDashboardAPI(), Process: p}
			a.StartDashboard = func(_ context.Context, c dashboard.Config) (
				DashboardService, error) {
				starts++
				if c.Repository != "example/project" || c.Bind != "" {
					t.Fatal(c)
				}
				if _, ok := c.Provider.(snapshot.Provider); !ok {
					t.Fatal("wrong provider")
				}
				if mode == "start" {
					return nil, errors.New("secret")
				}
				if mode == "cancel-start" {
					cancel()
				}
				return s, nil
			}
			a.OpenDashboard = func(context.Context, string) error {
				opens++
				if mode == "open" {
					return errors.New("secret bootstrap /private")
				}
				if mode == "close" || mode == "serve" {
					s.closeErr = errors.New("secret")
				}
				if mode == "serve" {
					close(s.done)
				} else {
					cancel()
				}
				return nil
			}
			var out bytes.Buffer
			err := a.Run([]string{"dashboard"}, &out)
			wantError := mode == "discovery" || mode == "start" || mode == "open" ||
				mode == "close" || mode == "serve"
			if (err != nil) != wantError {
				t.Fatal(mode, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "#") {
				t.Fatal("proof leaked")
			}
			if mode == "cancel-before" && (starts != 0 || opens != 0 || p.calls != 0) {
				t.Fatal("canceled invocation performed work")
			}
			if starts == 1 && mode != "start" && s.closed != 1 {
				t.Fatal("not closed")
			}
			if (mode == "start" || mode == "cancel-start" || mode == "discovery") && opens != 0 {
				t.Fatal("opened before startup")
			}
		})
	}
}

func TestDashboardComposedLoopback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := readableDashboardAPI()
	var service *dashboard.Service
	var origin, proof string
	starts, opens := 0, 0
	a := App{Context: ctx, API: api,
		Process: &dashboardProcess{t: t, err: errors.New("must not discover")}}
	a.StartDashboard = func(ctx context.Context, c dashboard.Config) (
		DashboardService, error) {
		starts++
		var err error
		service, err = dashboard.Start(ctx, c)
		return service, err
	}
	a.OpenDashboard = func(_ context.Context, target string) error {
		opens++
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		origin, proof = service.URL(), u.Fragment
		if origin != "http://"+u.Host || proof == "" {
			t.Fatal("wrong handoff")
		}
		for _, test := range []struct {
			proof  string
			status int
		}{
			{"", 401}, {"wrong", 401}, {proof, 200},
		} {
			req, _ := http.NewRequest("GET", origin+"/api/snapshot", nil)
			req.Header.Set(dashboard.SessionHeader, test.proof)
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatal(response.StatusCode, string(data))
			}
			if test.status == 200 {
				var result snapshot.Snapshot
				if json.Unmarshal(data, &result) != nil || result.SchemaVersion != 1 ||
					len(result.Repository.Items) != 1 ||
					result.Repository.Items[0].Name != "example/project" ||
					result.Issues.Status.Availability != snapshot.Denied {
					t.Fatal("projection changed", string(data))
				}
			}
		}
		cancel()
		return nil
	}
	var out bytes.Buffer
	if err := a.Run([]string{"dashboard", "--repo", "example/project"}, &out); err != nil {
		t.Fatal(err)
	}
	if starts != 1 || opens != 1 || strings.Contains(out.String(), proof) {
		t.Fatal("handoff")
	}
	if service.BootstrapURL() != "" {
		t.Fatal("proof retained")
	}
	address := strings.TrimPrefix(origin, "http://")
	l, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal("listener not released", err)
	}
	l.Close()
}

func TestDashboardBrowserArguments(t *testing.T) {
	target := "http://127.0.0.1:1234/#" + strings.Repeat("a", 64)
	for _, test := range []struct {
		platform, name string
		args           []string
	}{
		{"linux", "xdg-open", []string{target}},
		{"darwin", "open", []string{target}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", target}},
	} {
		name, args, err := dashboardBrowserCommand(test.platform, target)
		if err != nil || name != test.name || !reflect.DeepEqual(args, test.args) {
			t.Fatal(name, args, err)
		}
	}
	for _, target := range []string{"https://evil.example/#secret", "file:///tmp/a",
		"http://user@127.0.0.1:1234/#" + strings.Repeat("a", 64)} {
		if _, _, err := dashboardBrowserCommand("linux", target); err == nil {
			t.Fatal(target)
		}
	}
}

func TestDashboardDiscoveryFailureIsBounded(t *testing.T) {
	p := &dashboardProcess{t: t, err: errors.New("secret /private")}
	_, err := dashboardRepository(p)
	if err == nil || !strings.Contains(err.Error(), "--repo") ||
		strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}

func TestDashboardHelpHasNoEffects(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"dashboard", "--help"}} {
		var out bytes.Buffer
		if err := (App{}).Run(args, &out); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"dashboard [--repo OWNER/REPO]", "foreground", "Ctrl+C"} {
			if !strings.Contains(out.String(), text) {
				t.Fatal("missing help", text)
			}
		}
	}
}

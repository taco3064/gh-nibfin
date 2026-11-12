package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
)

type drainingDashboardAPI struct {
	base             *dashboardAPI
	reads            atomic.Int32
	entered, release chan struct{}
}

func (a *drainingDashboardAPI) Call(method, path string, input, output any) error {
	if path == "repos/owner/project" && a.reads.Add(1) == 2 {
		close(a.entered)
		<-a.release
	}
	return a.base.Call(method, path, input, output)
}

func TestDashboardCommandDrainsCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := &drainingDashboardAPI{base: readableDashboardAPI(),
		entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(api.release) }) }
	defer release()
	var service *dashboard.Service
	opened := make(chan struct{})
	requestDone := make(chan error, 1)
	runtime := &DashboardRuntime{
		Start: func(ctx context.Context, c dashboard.Config) (DashboardSession, error) {
			var err error
			service, err = dashboard.Start(ctx, c)
			return service, err
		},
		Open: func(_ context.Context, target string) error {
			u, _ := url.Parse(target)
			go func() {
				request, _ := http.NewRequest("GET", service.URL()+"/api/snapshot", nil)
				request.Header.Set(dashboard.SessionHeader, u.Fragment)
				client := &http.Client{Timeout: 3 * time.Second}
				response, err := client.Do(request)
				if err == nil {
					io.Copy(io.Discard, response.Body)
					response.Body.Close()
				}
				requestDone <- err
			}()
			<-api.entered
			cancel()
			close(opened)
			return nil
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- (App{API: api, Dashboard: runtime}).RunContext(ctx,
			[]string{"dashboard", "--repo", "owner/project"}, io.Discard)
	}()
	select {
	case <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("command never started collection")
	}
	select {
	case err := <-done:
		t.Fatal("returned before collection drained", err)
	case <-time.After(30 * time.Millisecond):
	}
	if service.BootstrapURL() != "" {
		t.Fatal("cancellation kept proof live")
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("command did not drain")
	}
	select {
	case err := <-requestDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not finish")
	}
}

func TestDashboardBrowserFailureReleasesRealListener(t *testing.T) {
	var service *dashboard.Service
	runtime := &DashboardRuntime{
		Start: func(ctx context.Context, c dashboard.Config) (DashboardSession, error) {
			var err error
			service, err = dashboard.Start(ctx, c)
			return service, err
		},
		Open: func(_ context.Context, target string) error {
			return fmt.Errorf("private browser path and URL: %s", target)
		},
	}
	var out bytes.Buffer
	err := (App{API: readableDashboardAPI(), Dashboard: runtime}).Run(
		[]string{"dashboard", "--repo", "owner/project"}, &out)
	if err == nil || err.Error() != "DASHBOARD_BROWSER_OPEN_FAILED" || out.Len() != 0 {
		t.Fatal("missing or unsafe browser error", err)
	}
	if service.BootstrapURL() != "" {
		t.Fatal("browser failure kept proof")
	}
	listener, err := net.Listen("tcp", strings.TrimPrefix(service.URL(), "http://"))
	if err != nil {
		t.Fatal("browser failure leaked listener", err)
	}
	listener.Close()
}

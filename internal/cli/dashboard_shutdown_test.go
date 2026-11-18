package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type dashboardBlockingOverview struct {
	entered, release chan struct{}
}

func (f dashboardBlockingOverview) CollectOverview() snapshot.Overview {
	close(f.entered)
	<-f.release
	return snapshot.Overview{}
}

func TestDashboardWaitsForActiveCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector := dashboardBlockingOverview{make(chan struct{}), make(chan struct{})}
	canceled := make(chan struct{})
	result := make(chan error, 1)
	var service *dashboard.Service
	a := App{Context: ctx, API: readableDashboardAPI()}
	a.StartDashboard = func(ctx context.Context, c dashboard.Config) (
		DashboardService, error) {
		var err error
		c.Overview = collector
		service, err = dashboard.Start(ctx, c)
		return service, err
	}
	a.OpenDashboard = func(_ context.Context, target string) error {
		select {
		case <-collector.entered:
		case <-time.After(3 * time.Second):
			return errors.New("collection did not start")
		}
		cancel()
		close(canceled)
		return nil
	}
	go func() {
		result <- a.Run([]string{"dashboard"}, io.Discard)
	}()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("command did not open")
	}
	select {
	case err := <-result:
		t.Fatal("returned before active collection finished", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(collector.release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	if service.BootstrapURL() != "" {
		t.Fatal("proof retained")
	}
}

func TestDashboardRealBindAndOpenFailure(t *testing.T) {
	for _, bindFailure := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		var service *dashboard.Service
		var occupied net.Listener
		opened := false
		a := App{Context: ctx, API: readableDashboardAPI()}
		a.StartDashboard = func(ctx context.Context, c dashboard.Config) (
			DashboardService, error) {
			if bindFailure {
				var err error
				occupied, err = net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					return nil, err
				}
				c.Bind = occupied.Addr().String()
			}
			var err error
			service, err = dashboard.Start(ctx, c)
			return service, err
		}
		a.OpenDashboard = func(context.Context, string) error {
			opened = true
			return errors.New("secret /private diagnostic")
		}
		err := a.Run([]string{"dashboard"}, io.Discard)
		cancel()
		if occupied != nil {
			occupied.Close()
		}
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
		if bindFailure && opened {
			t.Fatal("opened after failed bind")
		}
		if !bindFailure {
			if !opened || service.BootstrapURL() != "" {
				t.Fatal("open cleanup failed")
			}
			listener, err := net.Listen("tcp", strings.TrimPrefix(service.URL(), "http://"))
			if err != nil {
				t.Fatal("listener leaked", err)
			}
			listener.Close()
		}
	}
}

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/cli"
	"github.com/taco3064/gh-nibfin/internal/dashboard"
)

type signalAPI struct{}

func (signalAPI) Call(method, path string, input, output any) error {
	data := `{"id":2,"full_name":"example/project","permissions":{"pull":true}}`
	if path == "user" {
		data = `{"id":1,"login":"reader","type":"User"}`
	}
	return json.Unmarshal([]byte(data), output)
}

func TestDashboardNativeInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-sent interrupt requires Unix signals")
	}
	if os.Getenv("NIBFIN_DASHBOARD_SIGNAL_HELPER") == "1" {
		var service *dashboard.Service
		a := cli.App{API: signalAPI{}}
		a.StartDashboard = func(ctx context.Context, c dashboard.Config) (
			cli.DashboardService, error) {
			var err error
			service, err = dashboard.Start(ctx, c)
			return service, err
		}
		a.OpenDashboard = func(context.Context, string) error {
			process, err := os.FindProcess(os.Getpid())
			if err != nil {
				return err
			}
			return process.Signal(os.Interrupt)
		}
		if err := runApp(a, []string{"dashboard", "--repo", "example/project"},
			io.Discard); err != nil {
			t.Fatal(err)
		}
		if service == nil || service.BootstrapURL() != "" {
			t.Fatal("interrupt did not close the composed service")
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable,
		"-test.run=^TestDashboardNativeInterrupt$")
	command.Env = append(os.Environ(), "NIBFIN_DASHBOARD_SIGNAL_HELPER=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native signal path: %v\n%s", err, output)
	}
}

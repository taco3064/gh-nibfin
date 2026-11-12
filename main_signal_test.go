//go:build !windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/cli"
	"github.com/taco3064/gh-nibfin/internal/dashboard"
)

type signalAPI struct{}

func (signalAPI) Call(_ string, path string, _ any, output any) error {
	data := `{"id":2,"full_name":"owner/project","permissions":{"pull":true}}`
	if path == "user" {
		data = `{"id":1,"login":"reader","type":"User"}`
	}
	return json.Unmarshal([]byte(data), output)
}

func TestDashboardNativeInterrupt(t *testing.T) {
	if os.Getenv("NIBFIN_SIGNAL_FIXTURE") == "1" {
		ctx, stop := interruptionContext()
		defer stop()
		var service *dashboard.Service
		runtime := &cli.DashboardRuntime{
			Start: func(ctx context.Context, c dashboard.Config) (cli.DashboardSession, error) {
				var err error
				service, err = dashboard.Start(ctx, c)
				return service, err
			},
			Open: func(context.Context, string) error { fmt.Println("READY"); return nil },
		}
		app := cli.App{API: signalAPI{}, Dashboard: runtime}
		if err := app.RunContext(ctx, []string{"dashboard", "--repo", "owner/project"},
			io.Discard); err != nil {
			t.Fatal(err)
		}
		if service.BootstrapURL() != "" {
			t.Fatal("proof survived interrupt")
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
	command.Env = append(os.Environ(), "NIBFIN_SIGNAL_FIXTURE=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "READY" {
		t.Fatal("fixture never ready")
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal("interrupt failed to shut down", err)
	}
}

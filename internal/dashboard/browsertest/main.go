// Command browsertest hosts only synthetic snapshots using default packaged assets.
// Its bootstrap URL is sent through the test-owned pipe, never ordinary logs.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/pause"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type fixture struct{ value snapshot.Snapshot }

func (f fixture) Collect(string) snapshot.Snapshot { return f.value }

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("fixture required")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		return fmt.Errorf("fixture unavailable")
	}
	var value snapshot.Snapshot
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("invalid fixture")
	}
	var expired atomic.Bool
	now := func() time.Time {
		instant := time.Now()
		if expired.Load() {
			return instant.Add(dashboard.SessionLifetime + time.Minute)
		}
		return instant
	}
	var provider dashboard.Collector = fixture{value}
	var controller pause.Controller
	if len(os.Args) == 3 && os.Args[2] == "labels" {
		native := labelFixture(value)
		provider, controller = native, pause.Controller{Provider: native}
	}
	service, err := dashboard.Start(context.Background(), dashboard.Config{
		Repository: "example/project", Provider: provider, Controller: controller, Now: now,
	})
	if err != nil {
		return err
	}
	url := service.BootstrapURL()
	if len(os.Args) == 3 && os.Args[2] == "expired" {
		expired.Store(true)
	}
	if _, err := fmt.Fprintln(os.Stdout, url); err != nil {
		_ = service.Close()
		return fmt.Errorf("test pipe unavailable")
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	return service.Close()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "browser fixture service failed")
		os.Exit(1)
	}
}

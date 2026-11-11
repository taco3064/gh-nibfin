// Synthetic collector host for rendered tests. No GitHub transport is configured.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type collector struct{ value snapshot.Snapshot }

func (c collector) Collect(string) snapshot.Snapshot { return c.value }

func main() {
	var value snapshot.Snapshot
	if err := json.NewDecoder(os.Stdin).Decode(&value); err != nil {
		panic("invalid test snapshot")
	}
	service, err := dashboard.Start(context.Background(), dashboard.Config{
		Repository: "example/project", Provider: collector{value},
		Now: func() time.Time {
			return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		panic("test service unavailable")
	}
	defer service.Close()
	// A private pipe to the test process, never an ordinary application log.
	fmt.Println(service.BootstrapURL())
	var stop string
	_, _ = fmt.Fscanln(os.Stdin, &stop)
}

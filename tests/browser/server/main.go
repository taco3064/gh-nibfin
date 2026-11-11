// Browser fixture host: the production service and default packaged assets.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type collector struct{ value snapshot.Snapshot }

func (c collector) Collect(string) snapshot.Snapshot { return c.value }

func run() error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	if !scanner.Scan() {
		return fmt.Errorf("fixture missing")
	}
	var value snapshot.Snapshot
	if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
		return fmt.Errorf("invalid fixture")
	}
	service, err := dashboard.Start(context.Background(), dashboard.Config{
		Repository: "example/project", Provider: collector{value},
	})
	if err != nil {
		return err
	}
	// Private test pipe only; never forwarded to test output or files.
	fmt.Fprintln(os.Stdout, service.BootstrapURL())
	for scanner.Scan() {
	}
	return service.Close()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

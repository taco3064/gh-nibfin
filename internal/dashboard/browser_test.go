package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

// This opt-in fixture serves default packaged assets with a synthetic collector.
// Its secret handoff is a private subprocess pipe, never a maintained artifact.
func TestBrowserService(t *testing.T) {
	path := os.Getenv("NIBFIN_BROWSER_FIXTURE")
	if path == "" {
		t.Skip("launched by the rendered-browser suite")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value snapshot.Snapshot
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	s, err := Start(context.Background(), Config{Repository: "example/project",
		Provider: retainedSnapshot{value}})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("BROWSER_HANDOFF " + s.BootstrapURL())
	bufio.NewReader(os.Stdin).ReadString('\n')
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

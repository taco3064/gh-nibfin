package cli

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestOverviewNativeTransportBoundsAndDenial(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"denied", "failed", "large", "inventory-bound",
		"observation-bound", "cancel", "environment"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			limit := overviewResultLimit
			input := []byte(`{"token":"fixture-only"}`)
			script := "process.stdout.write('private diagnostic'); process.exit(1)"
			switch mode {
			case "denied":
				script = `process.stdout.write(JSON.stringify({schemaVersion:1,
status:'denied',extra:'private diagnostic'})); process.exit(1)`
			case "large":
				script = `process.stdout.write('x'.repeat(100000))`
			case "inventory-bound":
				limit = desktopOutputLimit
				script = `process.stdout.write('x'.repeat(100000))`
			case "observation-bound":
				script = `process.stdout.write('x'.repeat(1200000))`
			case "cancel":
				cancel()
				script = `setTimeout(() => {}, 10000)`
			case "environment":
				t.Setenv("GH_TOKEN", "fixture-credential")
				t.Setenv("WSLENV", "GH_TOKEN")
				script = `if (process.env.GH_TOKEN || process.env.WSLENV) process.exit(1);
let data = ''; process.stdin.on('data', chunk => data += chunk);
process.stdin.on('end', () => {
 if (Object.keys(JSON.parse(data)).join() !== 'token') process.exit(1);
 process.stdout.write('ok');
});`
			}
			data, err := runResolvedDesktopRead(ctx, node, []string{"-e", script}, input, limit)
			if strings.Contains(string(data), "private") {
				t.Fatal("diagnostics leaked")
			}
			switch mode {
			case "denied":
				if err != nil || string(data) != `{"schemaVersion":1,"status":"denied"}` {
					t.Fatal("denial lost", err)
				}
			case "large":
				if err != nil || len(data) != 100000 {
					t.Fatal("observation truncated", err)
				}
			case "environment":
				if err != nil || string(data) != "ok" {
					t.Fatal("environment or stdin", err)
				}
			default:
				if err == nil || len(data) != 0 {
					t.Fatal("unbounded or failed output retained")
				}
			}
		})
	}
}

func TestOverviewTransportRefusesArbitraryPrivateInput(t *testing.T) {
	b := fixtureOverviewBinding()
	_, valid := guestCommand(desktopTarget{Provider: "wsl", Name: "A"}, b.Entry,
		overviewBridgeCommand, "observe", "1", "reader", b.Identity, b.Architecture, b.Stamp)
	if !overviewPrivateArgs(valid) {
		t.Fatal("fixed operation rejected")
	}
	for _, invalid := range [][]string{{"--exec", "arbitrary"},
		{"--list", "--quiet"}, append(append([]string{}, valid...), "other")} {
		if _, err := (NativeProcess{}).DesktopReadContext(context.Background(), invalid,
			[]byte(`{"token":"fixture"}`), overviewResultLimit); err == nil {
			t.Fatal("native boundary accepted arbitrary private operation")
		}
	}
}

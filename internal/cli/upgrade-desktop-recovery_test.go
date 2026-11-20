package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestUpgradeOptionsShareValidationAndPreserveFalse(t *testing.T) {
	for _, desktop := range []bool{false, true} {
		o, err := parseUpgradeOptions([]string{"--repo=o/r", "--apply=false",
			"--managed-issue=false", "--version=v1.2.3"}, desktop)
		if err != nil || o.Apply || o.Version != "v1.2.3" {
			t.Fatal(o, err)
		}
		for _, flag := range []string{"--agent=codex", "--start-shared-services",
			"--consent-managed-login", "--managed-issue", "--version=main", "--apply=bad"} {
			if _, err := parseUpgradeOptions([]string{"--repo=o/r", flag}, desktop); err == nil {
				t.Fatal("accepted init or invalid option", desktop, flag)
			}
		}
	}
	if _, err := parseUpgradeOptions([]string{"--repo=o/r", "--target=wsl:Ubuntu"},
		false); err == nil {
		t.Fatal("Linux accepted desktop selection")
	}
}

func TestUpgradeSelectionPreferencesAndAmbiguity(t *testing.T) {
	base := upgradeDesktopTarget{target: desktopTarget{Name: "Ubuntu", Running: true},
		observed: upgradeTestObservation()}
	second := base
	second.target.Name = "Other"
	preference := upgradePreference{Target: "wsl:Ubuntu", Observation: base.observed}
	for _, scenario := range []string{"missing", "stale", "remembered", "replaced",
		"explicit", "ambiguous", "stopped"} {
		candidates := []upgradeDesktopTarget{base}
		pref := preference
		explicit := ""
		switch scenario {
		case "missing":
			pref = upgradePreference{}
		case "stale":
			pref.Target = "wsl:Removed"
		case "remembered":
			candidates = append(candidates, second)
		case "replaced", "explicit":
			candidates[0].observed.Identity = strings.Repeat("b", 32)
			if scenario == "explicit" {
				explicit = "wsl:Ubuntu"
			}
		case "ambiguous":
			pref = upgradePreference{}
			candidates = append(candidates, second)
		case "stopped":
			candidates[0].target.Running = false
		}
		chosen, err := selectUpgradeTarget(candidates, explicit, pref)
		blocked := scenario == "replaced" || scenario == "ambiguous"
		if blocked && err == nil || !blocked && err != nil {
			t.Fatal(scenario, chosen, err)
		}
	}
}

func TestUpgradeGuestOutputAndExitAreRelayedWithoutCompletionClaims(t *testing.T) {
	// These are controlled guest results, not evidence of live Core or VM behavior.
	// The existing native CLI envelope grounds migration and continuation output;
	// readiness/handoff payloads remain opaque to transport as their owner evolves.
	outputs := []string{
		`{"status":"pending","migration":[{"repository":"o/r","status":"pending",` +
			`"proposal":{"pull_request":"https://github.com/o/r/pull/9"}},` +
			`{"repository":"o/peer","status":"pending",` +
			`"proposal":{"pull_request":"https://github.com/o/peer/pull/10"}}],` +
			`"cause":"review/merge pending"}`,
		`{"status":"pending","vm_readiness":"pending",` +
			`"actions":"https://github.com/o/r/actions/runs/42"}`,
		`{"status":"pending","vm_readiness":"failed","cause":"KVM_UNAVAILABLE"}`,
		`{"status":"pending","installation":"maintenance-required"}`,
		`{"status":"pending","continuation":{"status":"paused","results":[]}}`,
		`{"status":"pending","continuation":{"status":"pending","results":[]}}`,
		`{"status":"completed","continuation":{"status":"dispatched","results":[]}}`,
		`{"handoff":"confirmed","vm_readiness":"confirmed",` +
			`"actions":"https://github.com/o/r/actions/runs/42"}`,
	}
	for _, output := range outputs {
		for _, exit := range []int{0, 1, 2, 7} {
			p := &upgradeDesktopFake{running: true, output: output, exit: guestUpgradeExit(exit)}
			app, _ := upgradeDesktopApp(t, p)
			var out bytes.Buffer
			err := app.runDesktopUpgrade([]string{"--repo=o/r"}, &out)
			if exit == 0 && err != nil || exit != 0 && err == nil {
				t.Fatal(exit, err)
			}
			var command *CommandError
			if exit != 0 && (!errors.As(err, &command) || command.Exit != exit) {
				t.Fatal("guest exit lost", exit, err)
			}
			lines := strings.SplitN(out.String(), "\n", 2)
			if len(lines) != 2 || lines[1] != output {
				t.Fatal("guest result changed or completion manufactured", out.String())
			}
		}
	}
}

func TestUpgradeResponseLossReentersSameGuestWithoutLedger(t *testing.T) {
	p := &upgradeDesktopFake{running: true,
		exit: errors.New("GUEST_TRANSPORT_LOST_READ_BACK_REQUIRED")}
	app, path := upgradeDesktopApp(t, p)
	args := []string{"--repo=o/r", "--apply"}
	if app.runDesktopUpgrade(args, io.Discard) == nil {
		t.Fatal("lost response passed")
	}
	original := append([]string(nil), p.entered...)
	// Missing preference is no evidence that provider-confirmed work is absent.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	p.exit = &CommandError{Code: "GUEST_EXIT_2", Exit: 2}
	p.output = `{"status":"pending","proposal":"https://github.com/o/r/pull/9"}`
	var out bytes.Buffer
	if app.runDesktopUpgrade(args, &out) == nil {
		t.Fatal("pending exit discarded")
	}
	if strings.Join(original, "\x00") != strings.Join(p.entered, "\x00") ||
		!strings.HasSuffix(out.String(), p.output) {
		t.Fatal("recovery redirected or lost guest result")
	}
}

func TestUpgradeControlReportSeparatesExitLaunchAndTransport(t *testing.T) {
	for _, code := range []int{0, 1, 2, 7} {
		data, _ := json.Marshal(map[string]int{"guest_exit": code})
		err := decodeUpgradeExit(data)
		if code == 0 && err != nil || code != 0 && err == nil {
			t.Fatal(code, err)
		}
	}
	launch := decodeUpgradeExit([]byte(`{"launch_failed":true}`))
	lost := decodeUpgradeExit(nil)
	if launch == nil || lost == nil || launch.Error() == lost.Error() {
		t.Fatal("launch and transport loss collapsed", launch, lost)
	}
}

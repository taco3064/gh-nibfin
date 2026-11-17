package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLinuxHandoffRejectsReplacementBeforePlanning(t *testing.T) {
	expected := strings.Repeat("f", 32)
	if linuxIdentity() == expected {
		expected = strings.Repeat("a", 32)
	}
	var out bytes.Buffer
	err := (App{}).Run([]string{"init", "--repo", "o/r", "--apply",
		"--linux-target-identity", expected, "--linux-target-arch", "arm64"}, &out)
	if err == nil || err.Error() != "GUEST_TARGET_CHANGED" || out.Len() != 0 {
		t.Fatal("handoff reached repository API or setup", err, out.String())
	}
}

func TestLinuxHandoffRequiresCompleteUnambiguousObservation(t *testing.T) {
	if requireLinuxObservation(linuxTargetObservation{}) == nil {
		t.Fatal("empty observation accepted")
	}
	files := eligibleLinuxFiles()
	read := func(path string) ([]byte, error) { return []byte(files[path]), nil }
	o := observeLinuxTarget(platformFixture(), read, "aarch64", "arm64", 183)
	if requireLinuxObservation(o) != nil {
		t.Fatal("valid native observation rejected")
	}
	o.Checks = append(o.Checks, o.Checks[0])
	if requireLinuxObservation(o) == nil {
		t.Fatal("duplicate evidence accepted")
	}
	o = observeLinuxTarget(platformFixture(), read, "x86_64", "arm64", 183)
	if requireLinuxObservation(o) == nil {
		t.Fatal("translated architecture accepted")
	}
}

func TestManagedHandoffRecheckedAfterConsentAndBeforeSetup(t *testing.T) {
	changed := errors.New("GUEST_TARGET_CHANGED")
	app := App{handoffCheck: func() error { return changed }}
	// Nil APIs and host operations would fail if any planning or mutation preceded
	// this recheck. These paths also cover resumed work after the PR wait.
	ctx := context.Background()
	if err := app.resumeManagedInit(ctx, InitPlan{}, nil, &bytes.Buffer{},
		managedHostOperations{}); err != changed {
		t.Fatal(err)
	}
	if err := app.finishVerifiedManagedInit(ctx, InitPlan{}, nil, nil,
		managedHostOperations{}); err != changed {
		t.Fatal(err)
	}
	if err := app.finishManagedInit(ctx, InitPlan{}, nil, nil,
		managedHostOperations{}); err != changed {
		t.Fatal(err)
	}
}

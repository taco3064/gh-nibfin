package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type upgradeFailingOutput struct{}

func (upgradeFailingOutput) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}

func TestUpgradeStartedGuestOutputLoss(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, failOutput := range []bool{false, true} {
		effect := filepath.Join(t.TempDir(), "guest-effect")
		args := []string{"-test.run=^TestUpgradeOutputChild$", "--",
			"upgrade-output-child", effect}
		var out io.Writer = io.Discard
		if failOutput {
			out = upgradeFailingOutput{}
		}
		err = runResolvedUpgradeGuest(context.Background(), executable, args, out)
		if _, statErr := os.Stat(effect); statErr != nil {
			t.Fatal("guest effect was not confirmed", statErr)
		}
		if failOutput {
			if err == nil || err.Error() != "GUEST_TRANSPORT_LOST_READ_BACK_REQUIRED" {
				t.Fatal("started guest output loss misclassified", err)
			}
		} else if err != nil {
			t.Fatal("working output failed", err)
		}
	}
	err = runResolvedUpgradeGuest(context.Background(),
		filepath.Join(t.TempDir(), "missing-guest"), nil, io.Discard)
	if err == nil || err.Error() != "GUEST_LAUNCH_FAILED" {
		t.Fatal("genuine start failure misclassified", err)
	}
}

func TestUpgradeOutputChild(t *testing.T) {
	args := os.Args
	if len(args) < 3 || args[len(args)-2] != "upgrade-output-child" {
		return
	}
	if err := os.WriteFile(args[len(args)-1], []byte("executed"), 0600); err != nil {
		os.Exit(1)
	}
	_, _ = os.Stderr.WriteString("guest proposal result\n")
	_, _ = os.Stdout.WriteString("{\"guest_exit\":0}\n")
	os.Exit(0)
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func assertNativeInstallLockHeld(t *testing.T, plan InitPlan) {
	t.Helper()
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := installIntentLock(path)
	if err == nil {
		unlock()
		t.Fatal("an independent acquisition entered the in-progress installation")
	}
	if err.Error() != "MANAGED_INSTALL_IN_PROGRESS" {
		t.Fatal(err)
	}
}

func assertNativeInstallLockReleased(t *testing.T, plan InitPlan) {
	t.Helper()
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := installIntentLock(path)
	if err != nil {
		t.Fatal("outer invocation left its installation lock held", err)
	}
	unlock()
}

func TestNativeCompletedPRContinuesInsideTheOriginalLock(t *testing.T) {
	for _, fail := range []bool{false, true} {
		base, plan, merge := managedResumeFixture(t)
		plan.NativeManaged = true
		merge()
		api := &nativeWakeAPI{fakeAPI: base}
		host := nativeFixtureHost(t, api)
		state := configureNativeFixture(api, plan)
		old := Pins()
		old.Artifact.ID++
		saveNativeFixtureIntent(t, managedInstallIntent{
			Plan: historicalNativePlan(plan, old), Pins: old, Native: state,
			PR: 7, Head: strings.Repeat("b", 40)})
		discoveries, continuations := 0, 0
		ops := managedHostOperations{
			discover: func() (*sharedHost, error) {
				discoveries++
				if discoveries == 1 {
					return nil, nil
				}
				return host, nil
			},
			cleanup: func(InitPlan, []sharedRegistration) error { return nil },
			native: func(current InitPlan, _ []sharedRegistration) (nativeContinuation, error) {
				assertNativeInstallLockHeld(t, current)
				continuations++
				if fail {
					return nativeContinuation{}, errors.New("FIXTURE_CONTINUATION_FAILURE")
				}
				return nativeContinuation{Status: "paused", Results: []json.RawMessage{}}, nil
			},
			prepare: func(InitPlan) error { t.Fatal("repeated host preparation"); return nil },
		}
		err := (App{API: api}).resumeManagedInit(context.Background(), plan, nil,
			new(bytes.Buffer), ops)
		if (err != nil) != fail || continuations != 1 || discoveries < 3 ||
			api.secretWrites != 0 || len(api.writes) != 0 {
			t.Fatal("completed PR did not continue inside the admitted operation", err,
				discoveries, continuations)
		}
		assertNativeInstallLockReleased(t, plan)
	}
}

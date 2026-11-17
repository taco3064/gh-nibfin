package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestManagedResultRequiresActivationAndSeparatesDispatch(t *testing.T) {
	plan := InitPlan{IssueRecipe: "issue-managed",
		Repository: Repository{ID: 10, FullName: "example/project"},
		Execution:  &SharedExecution{"example/project", 10}, Member: User{ID: 7}}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	ops := managedHostOperations{activate: func(InitPlan,
		[]sharedRegistration) (managedRelayResult, error) {
		return managedRelayResult{}, errors.New("MANAGED_RELAY_CONTROL_UNVERIFIED")
	}}
	if err := publishManagedRoute(encoder, plan, nil, ops); err == nil || out.Len() != 0 {
		t.Fatal("failed activation must not publish a configured result")
	}
	ops.activate = func(InitPlan, []sharedRegistration) (managedRelayResult, error) {
		return managedRelayResult{Mode: "managed", Service: "observed",
			Control: "resumed", Github: "verified", Recovery: "observed"}, nil
	}
	if err := publishManagedRoute(encoder, plan, nil, ops); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"MANAGED_ROUTING_CONFIGURED", "example/project",
		`"mode":"managed"`, `"dispatch_observed":false`} {
		if !strings.Contains(out.String(), expected) {
			t.Fatal(out.String())
		}
	}
}

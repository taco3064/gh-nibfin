package cli

import (
	"errors"
)

func releasedRebuild(i rebuildIntent, snapshot rebuildSnapshot) bool {
	r := snapshot.Retirement
	return r != nil && r.Release == "released" && r.ID == i.RetirementID &&
		r.Settled && r.Phase == "retired" && !snapshot.Draining &&
		rebuildFreshMatches(i, snapshot.Host) && sameAgent(&i.Agent, snapshot.Host.Agent) &&
		sameRebuildPool(snapshot.Host, r.Replacement) &&
		sameRebuildRepositories(i.Repositories, snapshot.Repositories)
}

func (a App) completeReleasedRebuild(i rebuildIntent, ops rebuildOperations,
	encoder installResultEncoder) error {
	if err := verifyFinalRebuildSnapshot(i, ops); err != nil {
		return err
	}
	snapshot, err := ops.snapshot()
	if err != nil || !releasedRebuild(i, snapshot) {
		return errors.New("REBUILD_RELEASE_REQUIRES_READBACK")
	}
	runners, err := readStartedSharedRunners(a.Context, a.API, *snapshot.Host)
	if err != nil {
		return err
	}
	output, callErr := ops.core("complete-installed-lifecycle")
	continuation, err := decodeNativeContinuation(output, callErr)
	if err != nil {
		return err
	}
	status := "completed"
	if continuation.Status == "pending" {
		status = "pending"
	}
	if err := encoder.Encode(map[string]any{"status": status,
		"code": "MANAGED_REPLACEMENT_ACTIVE", "activation": "core_verified",
		"retirementId": i.RetirementID, "agent": i.Agent,
		"execution": i.Old.SharedExecution, "runners": runners,
		"repositories": i.Repositories, "continuation": continuation}); err != nil {
		return err
	}
	if continuation.Status == "pending" {
		return &CommandError{Code: "NATIVE_CONTINUATION_PENDING", Exit: 2}
	}
	return nil
}

func (a App) activateReplacementNative(plan InitPlan,
	runners []sharedRegistration) (managedRelayResult, error) {
	result := managedRelayResult{Mode: "native", Service: "github",
		Control: "configured", Github: "verified", Recovery: "observed"}
	if !sharedRunnersOnline(runners) || initIssueRecipe(plan) != "issue" {
		return result, errors.New("REBUILD_NATIVE_ROUTE_UNVERIFIED")
	}
	pins, err := currentRepositoryProducers(a.API, plan.Repository.FullName)
	if err != nil {
		return result, err
	}
	host, observed, err := readNativeInstallationWithPins(a.API, plan,
		func() (*sharedHost, error) { return discoverSharedHost(a.Process) }, pins)
	if err != nil || host == nil || len(observed) != len(runners) ||
		!sharedRunnersOnline(observed) {
		return result, errors.New("REBUILD_NATIVE_ROUTE_UNVERIFIED")
	}
	for n, runner := range runners {
		if runner.ID != observed[n].ID || runner.Name != observed[n].Name {
			return result, errors.New("REBUILD_NATIVE_ROUTE_UNVERIFIED")
		}
	}
	_, exists, err := nativeWakeSecretMetadata(a.API, plan.Repository.FullName)
	if err != nil || !exists {
		return result, errors.New("NATIVE_WAKE_SECRET_REQUIRES_READBACK")
	}
	if err := setManagedPool(a.API, plan.Repository.FullName); err != nil {
		return result, err
	}
	return result, nil
}

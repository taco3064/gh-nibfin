package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
)

type managedRelayOperations struct {
	local   func(relayLocalRequest) ([]byte, error)
	core    func(string) ([]byte, error)
	system  func(...string) error
	refresh func() error
	recheck func() error
}

func (a App) activateManagedRelay(plan InitPlan,
	runners []sharedRegistration) (managedRelayResult, error) {
	if !sharedRunnersOnline(runners) {
		return managedRelayResult{}, errors.New("MANAGED_RELAY_RUNNERS_UNVERIFIED")
	}
	host, err := discoverSharedHost(a.Process)
	if err != nil || host == nil {
		return managedRelayResult{}, errors.New("MANAGED_RELAY_HOST_UNVERIFIED")
	}
	if plan.Execution == nil || host.SharedExecution != *plan.Execution ||
		host.MemberID != plan.Member.ID || len(host.Runners) != len(runners) {
		return managedRelayResult{}, errors.New("MANAGED_RELAY_HOST_BINDING_CHANGED")
	}
	for index, runner := range runners {
		if runner.ID != host.Runners[index].ID || runner.Name != host.Runners[index].Name {
			return managedRelayResult{}, errors.New("MANAGED_RELAY_RUNNERS_CHANGED")
		}
	}
	ops := managedRelayOperations{
		local: func(request relayLocalRequest) ([]byte, error) {
			return runRelayLocal(a.Process, request)
		},
		core: func(operation string) ([]byte, error) {
			return runInstalledRelay(a.Process, operation)
		},
		system: func(args ...string) error { return runRelaySystemd(a.Process, args...) },
		refresh: func() error {
			output, err := runInstalledSharedAdmin(a.Process, nil,
				"refresh-managed-issue-caller", plan.Repository.FullName,
				strconv.FormatInt(plan.Repository.ID, 10))
			var receipt struct {
				Status       string          `json:"status"`
				RepositoryID int64           `json:"repositoryId"`
				Runners      json.RawMessage `json:"runners"`
			}
			pool, _ := json.Marshal(host.Runners)
			if err != nil || json.Unmarshal(output, &receipt) != nil ||
				(receipt.Status != "adopted" && receipt.Status != "unchanged") ||
				receipt.RepositoryID != plan.Repository.ID ||
				!equalRelayJSON(pool, receipt.Runners) {
				return errors.New("MANAGED_CALLER_REFRESH_REQUIRES_READBACK")
			}
			return recheckSharedHost(a.Process, a.API, *host)
		},
		recheck: func() error {
			current, err := planInitRecipe(a.API, plan.Repository.FullName,
				plan.Execution.Repository, "issue-managed")
			if err != nil || len(current.Files) != 0 ||
				current.Repository.ID != plan.Repository.ID ||
				current.Repository.DefaultBranch != plan.Repository.DefaultBranch ||
				current.Member.ID != plan.Member.ID ||
				!sameSharedExecution(current.Execution, plan.Execution) {
				return errors.New("MANAGED_RELAY_INSTALLATION_CHANGED")
			}
			pin := Pins().Core
			body, _, err := content(a.API, pin.Repository, sharedRunnerWorkflow, pin.Commit)
			if err != nil {
				return errors.New("MANAGED_RELAY_DESTINATION_UNVERIFIED")
			}
			destination, _, err := repository(a.API, plan.Execution.Repository, false)
			if err != nil || destination.ID != plan.Execution.RepositoryID {
				return errors.New("MANAGED_RELAY_DESTINATION_CHANGED")
			}
			if err := verifySharedExecutionWorkflow(a.API, destination, body); err != nil {
				return errors.New("MANAGED_RELAY_DESTINATION_UNVERIFIED")
			}
			return recheckSharedHost(a.Process, a.API, *host)
		},
	}
	return configureManagedRelay(a.API, plan, ops)
}

func equalRelayJSON(a, b []byte) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil &&
		reflect.DeepEqual(left, right)
}

func relayLocalStatus(ops managedRelayOperations, operation, expected string) error {
	out, err := ops.local(relayLocalRequest{Operation: operation})
	var receipt struct{ Status string }
	if err != nil || json.Unmarshal(out, &receipt) != nil || receipt.Status != expected {
		return errors.New("MANAGED_RELAY_LOCAL_READBACK_REQUIRED")
	}
	return nil
}

func relaySnapshot(ops managedRelayOperations) (relayLocalSnapshot, error) {
	var snapshot relayLocalSnapshot
	out, err := ops.local(relayLocalRequest{Operation: "read"})
	if err != nil || json.Unmarshal(out, &snapshot) != nil ||
		len(snapshot.Bindings) == 0 || len(snapshot.Config) == 0 {
		return snapshot, errors.New("MANAGED_RELAY_LOCAL_READBACK_REQUIRED")
	}
	return snapshot, nil
}

func relayControl(ops managedRelayOperations, operation, expected string) error {
	out, err := ops.core(operation)
	var result struct{ Status string }
	if err != nil || json.Unmarshal(out, &result) != nil || result.Status != expected {
		return errors.New("MANAGED_RELAY_CONTROL_REQUIRES_INSPECTION")
	}
	return nil
}

func inspectManagedRelay(ops managedRelayOperations, config relayConfiguration,
	plan InitPlan) (relayObservation, error) {
	data, err := ops.core("inspect")
	if err != nil {
		return relayObservation{}, err
	}
	observed, err := decodeRelayObservation(data)
	if err == nil {
		err = observed.verify(config, plan)
	}
	return observed, err
}

func configureManagedRelay(api API, plan InitPlan,
	ops managedRelayOperations) (result managedRelayResult, err error) {
	if err := requireManagedRouteEligible(api, plan.Repository.FullName); err != nil {
		return result, err
	}
	if err := ops.recheck(); err != nil {
		return result, err
	}
	if err := relayLocalStatus(ops, "lock", "locked"); err != nil {
		return result, err
	}
	defer func() {
		if unlock := relayLocalStatus(ops, "unlock", "unlocked"); unlock != nil {
			err = unlock
		}
	}()
	// Refresh changes only the adopted Issue binding; Core preserves Action and
	// Runner identities. Read back the pool independently before configuring.
	if _, err := prepareSharedWakeDestination(api, plan.Repository.FullName,
		*plan.Execution); err != nil {
		return result, err
	}
	if err := ops.refresh(); err != nil {
		return result, err
	}
	snapshot, err := relaySnapshot(ops)
	if err != nil {
		return result, err
	}
	current := relayConfiguration{Version: 1, Origins: []relayOrigin{}}
	exists := !bytes.Equal(bytes.TrimSpace(snapshot.Config), []byte("null"))
	if exists {
		current, err = strictRelayConfig(snapshot.Config)
		if err != nil {
			return result, err
		}
	}
	adopted, err := relayBindings(api, snapshot.Bindings, plan, current)
	if err != nil {
		return result, err
	}
	next, err := reconcileRelayOrigins(current, adopted, plan.Repository.ID)
	if err != nil {
		return result, err
	}
	if err := requireManagedPoolEligibility(api, plan.Repository.FullName, current,
		plan.Repository.ID); err != nil {
		return result, err
	}
	changed := !reflect.DeepEqual(current, next)
	if exists {
		observed, err := inspectManagedRelay(ops, current, plan)
		if err != nil {
			return result, err
		}
		paused, err := observed.paused()
		if err != nil || paused {
			return result, errors.New("MANAGED_RELAY_PAUSED_REQUIRES_INSPECTION")
		}
		if !changed {
			// A fully configured route needs only inspection. An administrator
			// may explicitly resume an interrupted preparation after inspection;
			// quiesce that known route again before finishing suppression/startup.
			if err := verifyManagedPools(api, next, *plan.Execution); err == nil {
				if ready, err := observed.configured(plan.Repository.ID); err == nil {
					return ready, nil
				}
			}
		}
		prior := relayConfiguration{Version: 1, Origins: []relayOrigin{}}
		for _, origin := range current.Origins {
			if origin.RepositoryID != plan.Repository.ID {
				prior.Origins = append(prior.Origins, origin)
			}
		}
		if err := verifyManagedPools(api, prior, *plan.Execution); err != nil {
			return result, err
		}
		if err := relayControl(ops, "pause", "paused"); err != nil {
			return result, err
		}
	}
	if changed {
		out, err := ops.local(relayLocalRequest{"write", &snapshot, &next})
		var written struct{ Status string }
		if err != nil || json.Unmarshal(out, &written) != nil || written.Status != "written" {
			return result, errors.New("MANAGED_RELAY_CONFIGURATION_EFFECT_UNVERIFIED")
		}
	}
	readback, err := relaySnapshot(ops)
	configJSON, _ := json.Marshal(next)
	if err != nil || !equalRelayJSON(readback.Config, configJSON) ||
		!equalRelayJSON(readback.Bindings, snapshot.Bindings) {
		return result, errors.New("MANAGED_RELAY_CONFIGURATION_EFFECT_UNVERIFIED")
	}
	prepared, err := ops.core("prepare")
	if err != nil {
		return result, err
	}
	observed, err := decodeRelayObservation(prepared)
	if err != nil || observed.Status != "prepared" || observed.ServicesStarted == nil ||
		*observed.ServicesStarted || observed.Github.Status != "verified" {
		return result, errors.New("MANAGED_RELAY_PREPARATION_UNVERIFIED")
	}
	paused, err := observed.paused()
	if err != nil || !paused {
		return result, errors.New("MANAGED_RELAY_CONTROL_UNVERIFIED")
	}
	if err := ops.system("daemon-reload"); err != nil {
		return result, err
	}
	if _, err := inspectManagedRelay(ops, next, plan); err != nil {
		return result, err
	}
	if err := ops.recheck(); err != nil {
		return result, err
	}
	if _, err := prepareSharedWakeDestination(api, plan.Repository.FullName,
		*plan.Execution); err != nil {
		return result, err
	}
	if err := setManagedPool(api, plan.Repository.FullName); err != nil {
		return result, err
	}
	if err := verifyManagedPools(api, next, *plan.Execution); err != nil {
		return result, err
	}
	final, err := relaySnapshot(ops)
	if err != nil || !equalRelayJSON(final.Config, configJSON) ||
		!equalRelayJSON(final.Bindings, snapshot.Bindings) {
		return result, errors.New("MANAGED_RELAY_BINDINGS_CHANGED")
	}
	if err := ops.recheck(); err != nil {
		return result, err
	}
	if err := ops.system("enable", "--now", "nibfin-notification-relay.timer"); err != nil {
		return result, err
	}
	if err := relayControl(ops, "resume", "resumed"); err != nil {
		return result, err
	}
	observed, err = inspectManagedRelay(ops, next, plan)
	if err != nil {
		return result, err
	}
	if err := verifyManagedPools(api, next, *plan.Execution); err != nil {
		return result, err
	}
	return observed.configured(plan.Repository.ID)
}

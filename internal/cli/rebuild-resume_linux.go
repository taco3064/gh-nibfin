package cli

import (
	"encoding/json"
	"errors"
	"slices"
)

func (a App) resumeRebuild(i *rebuildIntent, token []byte, ops rebuildOperations,
	save func() error, encoder installResultEncoder) error {
	snapshot, err := ops.snapshot()
	if err != nil || reconcileRebuildSnapshot(*i, snapshot) != nil {
		return errors.New("REBUILD_SNAPSHOT_UNVERIFIED")
	}
	if releasedRebuild(*i, snapshot) {
		return a.completeReleasedRebuild(*i, ops, encoder)
	}
	if sameRebuildPool(&i.Old, snapshot.Host) {
		if ops.migrate != nil {
			if err := ops.migrate(*snapshot.Host); err != nil {
				return err
			}
		}
		if err := retireRebuild(a.API, i, ops, save); err != nil {
			return err
		}
	}
	primary := preparationRepository{i.Old.Repository, i.Old.RepositoryID}
	plan, err := ops.plan(primary, i)
	if err != nil {
		return err
	}
	snapshot, err = ops.snapshot()
	if err != nil || reconcileRebuildSnapshot(*i, snapshot) != nil {
		return errors.New("REBUILD_SNAPSHOT_UNVERIFIED")
	}
	host := snapshot.Host
	if sameRebuildPool(&i.Old, host) {
		if err := ops.prepare(plan); err != nil {
			return err
		}
		if err := setupManagedAgent(ops.host, plan, token); err != nil {
			return err
		}
		paths, err := ops.stage(plan, *i)
		if err != nil {
			return err
		}
		if len(paths) != 2 || paths[0] == paths[1] {
			return errors.New("REBUILD_TWO_FRESH_PACKAGES_REQUIRED")
		}
		i.Paths = paths
		if err := save(); err != nil {
			return err
		}
		if err := ops.register(plan, *i, func(fresh []sharedRegistration) error {
			// A later read may extend but may never replace a recorded identity.
			if len(fresh) < len(i.Fresh) {
				for n := range fresh {
					if fresh[n].ID != i.Fresh[n].ID || fresh[n].Name != i.Fresh[n].Name {
						return errors.New("REBUILD_FRESH_REGISTRATION_CHANGED")
					}
				}
				return nil
			}
			for n, old := range i.Fresh {
				if old.ID != fresh[n].ID || old.Name != fresh[n].Name {
					return errors.New("REBUILD_FRESH_REGISTRATION_CHANGED")
				}
			}
			i.Fresh = slices.Clone(fresh)
			return save()
		}); err != nil {
			return err
		}
		if !validRebuildIntent(*i) || len(i.Fresh) != 2 {
			return errors.New("REBUILD_FRESH_IDS_UNVERIFIED")
		}
		host, err = ops.host.adopt(plan, i.Fresh)
		if err != nil {
			// A lost adoption response remains pending. The next invocation uses
			// the fixed projection and exact saved fresh IDs to resume.
			return errors.New("REBUILD_ADOPTION_READBACK_REQUIRED")
		}
	}
	if snapshot.Retirement != nil && snapshot.Retirement.Release == "publishing" &&
		!sameRebuildPool(&i.Old, snapshot.Host) {
		// Core may have published the successor before recording preparation.
		// Repeat adoption with the same IDs to reconcile its publication intent.
		host, err = ops.host.adopt(plan, i.Fresh)
		if err != nil {
			return errors.New("REBUILD_ADOPTION_READBACK_REQUIRED")
		}
	}
	if !rebuildFreshMatches(*i, host) || checkManagedAgent(ops.host, plan, host) != nil {
		return errors.New("REBUILD_FRESH_BINDING_UNVERIFIED")
	}
	for _, target := range i.Repositories {
		peer, err := ops.plan(target, i)
		if err != nil {
			return err
		}
		if target.ID != primary.ID && host.adoptionStatus(target.ID) != "observed" {
			if err := ops.host.adoptPeer(*host, peer); err != nil {
				return errors.New("REBUILD_PEER_ADOPTION_READBACK_REQUIRED")
			}
		}
	}
	snapshot, err = ops.snapshot()
	if err != nil || reconcileRebuildSnapshot(*i, snapshot) != nil ||
		!sameRebuildRepositories(i.Repositories, snapshot.Repositories) {
		return errors.New("REBUILD_HISTORICAL_ADOPTIONS_UNVERIFIED")
	}
	if err := ops.packages(plan, *i); err != nil {
		return err
	}
	if err := ops.host.start(i.Paths); err != nil {
		return errors.New("REBUILD_SERVICES_READBACK_REQUIRED")
	}
	runners, err := readStartedSharedRunners(a.Context, a.API, *snapshot.Host)
	if err != nil {
		return err
	}
	var routes []managedRelayResult
	for _, target := range i.Repositories {
		peer, err := ops.plan(target, i)
		if err != nil {
			return err
		}
		route, err := ops.host.activate(peer, runners)
		if err != nil {
			return errors.New("REBUILD_WAKE_PREPARATION_INCOMPLETE")
		}
		routes = append(routes, route)
	}
	if err := retireStagedRegistration(i.Paths); err != nil {
		return err
	}
	if err := verifyFinalRebuildSnapshot(*i, ops); err != nil {
		return err
	}
	output, callErr := ops.core("activate-shared-replacement")
	if callErr != nil || verifyRebuildActivation(output, *i) != nil {
		return errors.New("REBUILD_ACTIVATION_UNCERTAIN_REPEAT_SAME_COMMAND")
	}
	var activation struct {
		Continuation json.RawMessage `json:"continuation"`
	}
	if json.Unmarshal(output, &activation) != nil {
		return errors.New("REBUILD_ACTIVATION_UNCERTAIN_REPEAT_SAME_COMMAND")
	}
	continuation, err := nativeContinuationPayload(activation.Continuation)
	if err != nil {
		// Older Core may omit the additive continuation field. The same fixed
		// lifecycle operation observes and resumes work without rebuilding.
		return a.completeReleasedRebuild(*i, ops, encoder)
	}
	if continuation.Status == "pending" {
		_ = encoder.Encode(map[string]any{"status": "pending",
			"continuation": continuation, "retirementId": i.RetirementID})
		return &CommandError{Code: "NATIVE_CONTINUATION_PENDING", Exit: 2}
	}
	if err := verifyFinalRebuildSnapshot(*i, ops); err != nil {
		return err
	}
	return encoder.Encode(map[string]any{"status": "completed",
		"code": "MANAGED_REPLACEMENT_ACTIVE", "activation": "core_verified",
		"retirementId": i.RetirementID, "agent": i.Agent,
		"execution": i.Old.SharedExecution, "runners": runners,
		"repositories": i.Repositories, "routing": routes,
		"continuation": continuation})
}

func verifyRebuildActivation(output []byte, i rebuildIntent) error {
	var receipt struct {
		Status string `json:"status"`
		ID     string `json:"retirementId"`
	}
	host, err := decodeSharedHost(output)
	if len(output) > 16384 || json.Unmarshal(output, &receipt) != nil ||
		receipt.Status != "active" || receipt.ID != i.RetirementID || err != nil ||
		!rebuildFreshMatches(i, host) ||
		len(host.AdoptedRepositoryIDs) != len(i.Repositories) {
		return errors.New("REBUILD_ACTIVATION_UNVERIFIED")
	}
	for _, target := range i.Repositories {
		if !slices.Contains(host.AdoptedRepositoryIDs, target.ID) {
			return errors.New("REBUILD_ACTIVATION_UNVERIFIED")
		}
	}
	return nil
}

func verifyFinalRebuildSnapshot(i rebuildIntent, ops rebuildOperations) error {
	snapshot, err := ops.snapshot()
	if err != nil || reconcileRebuildSnapshot(i, snapshot) != nil ||
		!rebuildFreshMatches(i, snapshot.Host) ||
		!sameRebuildRepositories(i.Repositories, snapshot.Repositories) {
		return errors.New("REBUILD_FINAL_BINDING_UNVERIFIED")
	}
	return nil
}

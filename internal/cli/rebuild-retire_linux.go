package cli

import (
	"encoding/json"
	"errors"
	"slices"
)

type rebuildRetirementResult struct {
	Version int    `json:"schemaVersion"`
	Status  string `json:"status"`
	ID      string `json:"retirementId"`
	Phase   string `json:"phase"`
	Removal struct {
		SharedExecution
		MemberID  int64                `json:"memberId"`
		Permitted bool                 `json:"permitted"`
		Runners   []sharedRegistration `json:"runners"`
	} `json:"removal"`
}

func decodeRebuildRetirement(output []byte, i rebuildIntent,
) (rebuildRetirementResult, error) {
	var r rebuildRetirementResult
	if len(output) > 16384 || json.Unmarshal(output, &r) != nil || r.Version != 1 ||
		!rebuildID.MatchString(r.ID) || i.RetirementID != "" && i.RetirementID != r.ID ||
		r.Removal.SharedExecution != i.Old.SharedExecution ||
		r.Removal.MemberID != i.Old.MemberID || len(r.Removal.Runners) != 2 ||
		!slices.Contains([]string{"pending", "retired"}, r.Status) ||
		!slices.Contains([]string{"draining", "settling", "removal", "cleanup",
			"retired"}, r.Phase) || r.Status == "retired" && r.Phase != "retired" {
		return r, errors.New("REBUILD_RETIREMENT_RESPONSE_UNVERIFIED")
	}
	for n, runner := range r.Removal.Runners {
		if runner.ID != i.Old.Runners[n].ID || runner.Name != i.Old.Runners[n].Name {
			return r, errors.New("REBUILD_RETIREMENT_IDENTITY_CHANGED")
		}
	}
	return r, nil
}

func reconcileRebuildSnapshot(i rebuildIntent, s rebuildSnapshot) error {
	if !validRebuildSnapshot(s) {
		return errors.New("REBUILD_SNAPSHOT_UNVERIFIED")
	}
	r := s.Retirement
	if r != nil && i.RetirementID == r.ID && r.Phase == "retired" &&
		r.Release != "released" && !s.Draining {
		return errors.New("REBUILD_CORE_DRAIN_UNVERIFIED")
	}
	if i.RetirementID != "" {
		if r == nil || r.ID != i.RetirementID || !sameRebuildPool(&i.Old, r.Pool) ||
			!sameRebuildRepositories(i.Repositories, r.Repositories) {
			return errors.New("REBUILD_RETIREMENT_IDENTITY_CHANGED")
		}
	}
	if sameRebuildPool(&i.Old, s.Host) {
		if !sameAgent(i.Old.Agent, s.Host.Agent) ||
			!sameRebuildRepositories(i.Repositories, s.Repositories) {
			return errors.New("REBUILD_ADOPTION_CHANGED")
		}
		return nil
	}
	if r == nil || r.ID != i.RetirementID || r.Phase != "retired" ||
		!r.Settled || !sameRebuildPool(r.Replacement, s.Host) ||
		!sameAgent(&i.Agent, s.Host.Agent) || !rebuildFreshMatches(i, s.Host) {
		return errors.New("REBUILD_REPLACEMENT_IDENTITY_CHANGED")
	}
	for _, repo := range s.Repositories {
		if !slices.Contains(i.Repositories, repo) {
			return errors.New("REBUILD_ADOPTION_CHANGED")
		}
	}
	return nil
}

func rebuildFreshMatches(i rebuildIntent, host *sharedHost) bool {
	if host == nil || len(host.Runners) != 2 || len(i.Fresh) != 2 ||
		host.SharedExecution != i.Old.SharedExecution || host.MemberID != i.Old.MemberID {
		return false
	}
	for n, runner := range i.Fresh {
		if runner.ID != host.Runners[n].ID || runner.Name != host.Runners[n].Name {
			return false
		}
	}
	return true
}

// Every remaining removal obtains a new Core response. Saved phases and native
// absence never grant removal authority or permission to create a replacement.
func retireRebuild(api API, i *rebuildIntent, ops rebuildOperations,
	save func() error) error {
	for n := 0; n <= len(i.Old.Runners); n++ {
		output, callErr := ops.core("retire-shared-host")
		r, err := decodeRebuildRetirement(output, *i)
		// Core returns exit 2 for a well-formed pending result; it is usable only
		// when its matching response explicitly grants current removal permission.
		if err != nil || callErr != nil &&
			(callErr.Error() != "CORE_PENDING_RESULT" || r.Status != "pending") {
			return errors.New("REBUILD_RETIREMENT_RESPONSE_UNVERIFIED")
		}
		i.RetirementID = r.ID
		if err := save(); err != nil {
			return err
		}
		snapshot, err := ops.snapshot()
		if err != nil || reconcileRebuildSnapshot(*i, snapshot) != nil ||
			snapshot.Retirement == nil || snapshot.Retirement.Phase != r.Phase ||
			r.Status == "retired" && !snapshot.Retirement.Settled {
			return errors.New("REBUILD_RETIREMENT_SNAPSHOT_REQUIRED")
		}
		if r.Status == "retired" {
			for _, runner := range i.Old.Runners {
				present, err := rebuildRegistration(api, i.Old,
					sharedRegistration{ID: runner.ID, Name: runner.Name})
				if err != nil || present {
					return errors.New("REBUILD_OLD_REGISTRATIONS_UNVERIFIED")
				}
			}
			return nil
		}
		if n == len(i.Old.Runners) || r.Phase != "removal" || !r.Removal.Permitted {
			return errors.New("REBUILD_CORE_SETTLEMENT_PENDING")
		}
		// Remove at most one present ID per permission response, then settle again.
		for _, runner := range r.Removal.Runners {
			present, err := rebuildRegistration(api, i.Old, runner)
			if err != nil {
				return err
			}
			if present {
				if err := removeRebuildRegistration(api, i.Old, runner); err != nil {
					return err
				}
				break
			}
		}
	}
	return errors.New("REBUILD_CORE_SETTLEMENT_PENDING")
}

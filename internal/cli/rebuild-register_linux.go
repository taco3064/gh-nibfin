package cli

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func (a App) registerReplacementSlots(plan InitPlan, i rebuildIntent,
	save func([]sharedRegistration) error) error {
	var observed []sharedRegistration
	for _, path := range i.Paths {
		runner, err := a.registerReplacementSlot(plan, i, path)
		if err != nil {
			return err
		}
		observed = append(observed, runner)
		if err := save(observed); err != nil {
			return err
		}
	}
	if len(observed) != 2 || observed[0].ID == observed[1].ID {
		return errors.New("REBUILD_FRESH_IDS_UNVERIFIED")
	}
	return nil
}

func (a App) registerReplacementSlot(plan InitPlan, i rebuildIntent,
	path string) (result sharedRegistration, err error) {
	lockPath := filepath.Join(path, runnerManagementLock)
	lock, err := privateInstallFile(lockPath, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return result, errors.New("RUNNER_MANAGEMENT_IN_PROGRESS_INSPECT_BEFORE_RETRY")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return result, errors.New("RUNNER_MANAGEMENT_IN_PROGRESS_INSPECT_BEFORE_RETRY")
	}
	defer lock.Close()
	defer func() {
		if os.Remove(lockPath) != nil {
			err = errors.New("REBUILD_REGISTRATION_LOCK_CLEANUP_REQUIRED")
		}
	}()
	record, err := readRunnerRecord(path, plan.Repository, plan.Member)
	if err != nil {
		return result, err
	}
	authorize := func(host *sharedHost) error {
		snapshot, err := readRebuildSnapshot(a.Process)
		if err != nil || reconcileRebuildSnapshot(i, snapshot) != nil ||
			!sameRebuildPool(&i.Old, host) || snapshot.Retirement == nil ||
			snapshot.Retirement.Phase != "retired" || !snapshot.Retirement.Settled {
			return errors.New("REBUILD_CORE_RETIREMENT_REQUIRED")
		}
		for _, old := range i.Old.Runners {
			present, err := rebuildRegistration(a.API, i.Old,
				sharedRegistration{ID: old.ID, Name: old.Name})
			if err != nil || present {
				return errors.New("REBUILD_OLD_REGISTRATIONS_UNVERIFIED")
			}
		}
		return nil
	}
	clock := a.Clock
	if clock == nil {
		clock = time.Now
	}
	record, err = a.registerSharedSlotChecked(plan.Repository, plan.Member, path,
		record, clock(), authorize)
	if err != nil {
		return result, err
	}
	for _, old := range i.Old.Runners {
		if record.RunnerID == old.ID {
			return result, errors.New("REBUILD_FRESH_ID_REUSES_RETIRED_RUNNER")
		}
	}
	return sharedRegistration{ID: record.RunnerID, Name: record.Name,
		Status: "offline"}, nil
}

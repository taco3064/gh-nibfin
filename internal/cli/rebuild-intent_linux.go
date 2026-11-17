package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

var rebuildID = regexp.MustCompile(`^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)

func sharedMutationPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil || !filepath.IsAbs(root) {
		return "", errors.New("REBUILD_PRIVATE_STATE_UNAVAILABLE")
	}
	return filepath.Join(root, "nibfin", "install", "shared-pool.json"), nil
}

// One installing user's pool lock covers every adopted repository, installation
// and replacement. Core independently serializes privileged pool transitions.
func sharedMutationLock() (func(), error) {
	path, err := sharedMutationPath()
	if err != nil {
		return nil, err
	}
	unlock, err := installIntentLock(path)
	if err != nil {
		return nil, err
	}
	closeHost, err := sharedHostMutationLock()
	if err != nil {
		unlock()
		return nil, err
	}
	return func() { closeHost(); unlock() }, nil
}

func sharedHostMutationLock() (func(), error) {
	const directory = "/etc/nibfin"
	if _, err := os.Lstat(directory); errors.Is(err, os.ErrNotExist) {
		return func() {}, nil
	}
	if err := requireRootOwnedHostPath(directory); err != nil {
		return nil, err
	}
	// The directory inode is shared across installing users. This CLI-only
	// lease does not overlap Core's resource, retirement or adoption gates.
	file, err := os.OpenFile(directory, os.O_RDONLY|syscall.O_DIRECTORY|
		syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("SHARED_POOL_LOCK_UNAVAILABLE")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("SHARED_POOL_MUTATION_IN_PROGRESS")
	}
	return func() { _ = file.Close() }, nil
}

func readRebuildIntent(path string) (*rebuildIntent, error) {
	file, err := privateInstallFile(path, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("REBUILD_PRIVATE_STATE_UNAVAILABLE")
	}
	defer file.Close()
	var intent rebuildIntent
	decoder := json.NewDecoder(io.LimitReader(file, 65537))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&intent) != nil || decoder.Decode(new(any)) != io.EOF ||
		!validRebuildIntent(intent) {
		return nil, errors.New("REBUILD_INTENT_CHANGED")
	}
	return &intent, nil
}

func validRebuildIntent(i rebuildIntent) bool {
	if i.Version != 1 || !validRebuildPool(&i.Old) || i.Old.Agent == nil ||
		!i.Old.Agent.valid() || !i.Agent.valid() ||
		!validRebuildRepositories(i.Repositories, i.Old.RepositoryID) ||
		i.RetirementID != "" && !rebuildID.MatchString(i.RetirementID) ||
		len(i.Fresh) > 2 || len(i.Paths) != 0 && len(i.Paths) != 2 {
		return false
	}
	ids := map[int64]bool{}
	for _, runner := range i.Old.Runners {
		ids[runner.ID] = true
	}
	for _, runner := range i.Fresh {
		if runner.ID <= 0 || ids[runner.ID] || !runnerNamePattern.MatchString(runner.Name) {
			return false
		}
		ids[runner.ID] = true
	}
	return true
}

func saveRebuildIntent(path string, intent rebuildIntent) error {
	if !validRebuildIntent(intent) {
		return errors.New("REBUILD_INTENT_CHANGED")
	}
	return writePrivateIntent(path, intent)
}

func requireNoReplacement() error {
	path, err := sharedMutationPath()
	if err != nil {
		return err
	}
	intent, err := readRebuildIntent(path)
	if err != nil {
		return err
	}
	if intent != nil {
		return errors.New("MANAGED_REPLACEMENT_PENDING_USE_RUNNER_REBUILD")
	}
	return nil
}

func lockSharedInit(plan InitPlan) (func(), error) {
	if plan.Execution == nil {
		return func() {}, nil
	}
	unlock, err := sharedMutationLock()
	if err != nil {
		return nil, err
	}
	if err := requireNoReplacement(); err != nil {
		unlock()
		return nil, err
	}
	_, markerErr := os.Lstat("/etc/nibfin/managed-pool-drain.json")
	if !errors.Is(markerErr, os.ErrNotExist) {
		unlock()
		return nil, errors.New("MANAGED_REPLACEMENT_PENDING_USE_RUNNER_REBUILD")
	}
	return unlock, nil
}

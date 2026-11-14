package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Cache official packages under the installing user. The protected service
// installer must verify them again before copying them to the Runner identities.
func (a App) stageSharedRunnerPackages(plan InitPlan) ([]string, error) {
	if plan.Execution == nil || plan.Execution.RepositoryID != plan.Repository.ID {
		return nil, errors.New("SHARED_RUNNER_PACKAGE_REQUIRES_EXECUTION_REPOSITORY")
	}
	app, err := runnerApplication(a.API, plan.Repository.FullName)
	if err != nil {
		return nil, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, errors.New("USER_CACHE_UNAVAILABLE")
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		return nil, errors.New("USER_CACHE_UNAVAILABLE")
	}
	cache, err = filepath.EvalSymlinks(cache)
	if err != nil {
		return nil, errors.New("USER_CACHE_UNAVAILABLE")
	}
	parent, err := sharedRunnerPackageDirectory(cache, plan, app.Hash)
	if err != nil {
		return nil, err
	}
	return a.stageRunnerSlots(plan, parent, app)
}

func sharedRunnerPackageDirectory(cache string, plan InitPlan,
	hash string) (string, error) {
	if !digestPattern.MatchString(hash) {
		return "", errors.New("SHARED_RUNNER_PACKAGE_DIRECTORY_UNAVAILABLE")
	}
	member := fmt.Sprintf("%d-%d", plan.Repository.ID, plan.Member.ID)
	parent := cache
	for _, segment := range []string{"Nibfin", "shared-runners", member, hash} {
		parent = filepath.Join(parent, segment)
		if err := os.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", errors.New("SHARED_RUNNER_PACKAGE_DIRECTORY_UNAVAILABLE")
		}
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() {
			return "", errors.New("UNTRUSTED_SHARED_RUNNER_PACKAGE_DIRECTORY")
		}
		if err := requirePrivateRunnerPaths([]string{parent}); err != nil {
			return "", err
		}
	}
	return parent, nil
}

func (a App) stageRunnerSlots(plan InitPlan, parent string,
	app RunnerApplication) ([]string, error) {
	var paths []string
	for slot := 1; slot <= 2; slot++ {
		name := sharedRunnerSlotName(plan.Repository.ID, plan.Member.ID, slot)
		path, err := runnerDirectory(filepath.Join(parent, name))
		if err != nil {
			return nil, err
		}
		_, err = os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			download := a.Download
			if download == nil {
				download = OfficialRunnerDownload
			}
			_, err = createRunnerPackage(download, plan.Repository, plan.Member,
				name, path, app)
		}
		if err != nil {
			return nil, err
		}
		record, err := readRunnerRecord(path, plan.Repository, plan.Member)
		if err != nil {
			return nil, err
		}
		if record.Name != name || record.Application != app {
			return nil, errors.New("SHARED_RUNNER_PACKAGE_CONFLICT_INSPECT_BEFORE_RETRY")
		}
		switch {
		case record.State == "created" && record.RunnerID == 0:
			if err := verifyRunnerPackage(path, app, true); err != nil {
				return nil, err
			}
		case record.State == "registered" && record.RunnerID > 0:
			if _, err := readRegisteredSharedSlot(a.API, plan.Repository,
				plan.Member, path, record); err != nil {
				return nil, err
			}
			if err := verifyRunnerFiles(path, app); err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("SHARED_RUNNER_PACKAGE_CONFLICT_INSPECT_BEFORE_RETRY")
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func sharedRunnerSlotName(repositoryID, memberID int64, slot int) string {
	return fmt.Sprintf("nibfin-%d-%d-%d", repositoryID, memberID, slot)
}

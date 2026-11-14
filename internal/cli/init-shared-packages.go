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
	parent := filepath.Join(cache, "Nibfin", "shared-runners",
		fmt.Sprintf("%d-%d", plan.Repository.ID, plan.Member.ID), app.Hash)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, errors.New("SHARED_RUNNER_PACKAGE_DIRECTORY_UNAVAILABLE")
	}
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil || canonical != parent {
		return nil, errors.New("UNTRUSTED_SHARED_RUNNER_PACKAGE_DIRECTORY")
	}
	if err := requirePrivateRunnerPaths([]string{parent}); err != nil {
		return nil, err
	}
	return a.stageRunnerSlots(plan, parent, app)
}

func (a App) stageRunnerSlots(plan InitPlan, parent string,
	app RunnerApplication) ([]string, error) {
	var paths []string
	for slot := 1; slot <= 2; slot++ {
		name := fmt.Sprintf("nibfin-%d-%d-%d", plan.Repository.ID, plan.Member.ID, slot)
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
		if record.State != "created" || record.RunnerID != 0 || record.Name != name ||
			record.Application != app {
			return nil, errors.New("SHARED_RUNNER_PACKAGE_CONFLICT_INSPECT_BEFORE_RETRY")
		}
		if err := verifyRunnerFiles(path, app); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

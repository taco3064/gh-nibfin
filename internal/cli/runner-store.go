package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const runnerRecordName = ".nibfin-runner.json"

type RunnerInstall struct {
	Repository   string            `json:"repository"`
	RepositoryID int64             `json:"repository_id"`
	MemberID     int64             `json:"member_id"`
	Name         string            `json:"name"`
	RunnerID     int64             `json:"runner_id"`
	State        string            `json:"state"`
	Application  RunnerApplication `json:"application"`
}

func runnerDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("ABSOLUTE_RUNNER_DIRECTORY_REQUIRED")
	}
	path = filepath.Clean(path)
	if path == filepath.Dir(path) {
		return "", errors.New("UNSAFE_RUNNER_DIRECTORY")
	}
	if hasGitAncestor(filepath.Dir(path)) {
		return "", errors.New("RUNNER_DIRECTORY_INSIDE_WORKTREE")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", errors.New("RUNNER_PARENT_DIRECTORY_UNAVAILABLE")
	}
	canonical := filepath.Join(parent, filepath.Base(path))
	if hasGitAncestor(canonical) {
		return "", errors.New("RUNNER_DIRECTORY_INSIDE_WORKTREE")
	}
	return canonical, nil
}

func readJSONFile(path string, output any) error {
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 64*1024 {
		return errors.New("RUNNER_LOCAL_RECORD_UNAVAILABLE")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("RUNNER_LOCAL_RECORD_UNAVAILABLE")
	}
	defer file.Close()
	if json.NewDecoder(io.LimitReader(file, 64*1024+1)).Decode(output) != nil {
		return errors.New("INVALID_RUNNER_LOCAL_RECORD")
	}
	return nil
}

func writeRunnerRecord(directory string, record RunnerInstall) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return errors.New("INVALID_RUNNER_LOCAL_RECORD")
	}
	path := filepath.Join(directory, runnerRecordName)
	if existing, err := os.Lstat(path); err == nil && !existing.Mode().IsRegular() {
		return errors.New("UNTRUSTED_RUNNER_LOCAL_RECORD")
	}
	if err := writeDurableState(path, append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func readRunnerRecord(directory string, repo Repository,
	user User) (RunnerInstall, error) {
	var record RunnerInstall
	stat, err := os.Lstat(directory)
	resolved, resolveErr := filepath.EvalSymlinks(directory)
	if err != nil || resolveErr != nil || !stat.IsDir() || resolved != directory ||
		runtime.GOOS != "windows" && stat.Mode().Perm()&0077 != 0 {
		return record, errors.New("UNTRUSTED_RUNNER_DIRECTORY")
	}
	path := filepath.Join(directory, runnerRecordName)
	if err := requirePrivateRunnerPaths([]string{directory, path}); err != nil {
		return record, err
	}
	err = readJSONFile(path, &record)
	if err != nil {
		return record, err
	}
	if record.Repository != repo.FullName || record.RepositoryID != repo.ID ||
		record.MemberID != user.ID || record.Name == "" {
		return record, errors.New("RUNNER_MANAGEMENT_IDENTITY_MISMATCH")
	}
	return record, nil
}

func rejectRunnerOverrides(directory string) error {
	path := filepath.Join(directory, ".env")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("UNTRUSTED_RUNNER_ENVIRONMENT_FILE")
	}
	return nil
}

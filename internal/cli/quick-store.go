package cli

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Quick records are recovery observations, never Core execution grants.
type quickRequest struct {
	Version      int    `json:"version"`
	Attempt      string `json:"attempt"`
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repository_id"`
	MemberID     int64  `json:"member_id"`
	Login        string `json:"login"`
	Name         string `json:"name"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
}

type quickState struct {
	Request   quickRequest      `json:"request"`
	Stage     string            `json:"stage"`
	Directory string            `json:"directory,omitempty"`
	App       RunnerApplication `json:"application"`
	RunnerID  int64             `json:"runner_id,omitempty"`
	Expires   string            `json:"expires_at,omitempty"`
	Origin    string            `json:"origin_digest,omitempty"`
}

type quickCapability struct {
	Kind    string            `json:"kind"`
	Request quickRequest      `json:"request"`
	App     RunnerApplication `json:"application"`
	Token   runnerToken       `json:"registration"`
}

type quickResult struct {
	Kind     string       `json:"kind"`
	Request  quickRequest `json:"request"`
	RunnerID int64        `json:"runner_id"`
}

var quickLogin = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

func newQuickAttempt() (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", errors.New("HANDOFF_RANDOMNESS_UNAVAILABLE")
	}
	return hex.EncodeToString(nonce[:]), nil
}

func quickDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func decodeQuick(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > 64*1024 || decoder.Decode(value) != nil ||
		decoder.Decode(new(any)) != io.EOF {
		return errors.New("INVALID_HANDOFF_INPUT")
	}
	return nil
}

// The private parent excludes other users and symlink substitution. Exchange
// material must live outside a checkout and must never be a workflow artifact.
func quickFilePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("ABSOLUTE_PRIVATE_HANDOFF_PATH_REQUIRED")
	}
	path = filepath.Clean(path)
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || parent != filepath.Dir(path) || hasGitAncestor(parent) {
		return "", errors.New("HANDOFF_OUTSIDE_WORKTREE_REQUIRED")
	}
	if err := requirePrivateRunnerParent(path); err != nil {
		return "", err
	}
	if err := requirePrivateHandoffPaths([]string{parent}); err != nil {
		return "", err
	}
	return path, nil
}

func readQuick(path string, consume bool) (data []byte, err error) {
	path, err = quickFilePath(path)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateHandoffPaths([]string{path}); err != nil {
		return nil, err
	}
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() {
		return nil, errors.New("HANDOFF_REGULAR_FILE_REQUIRED")
	}
	if consume {
		defer func() {
			if os.Remove(path) != nil {
				data = nil
				err = errors.New("HANDOFF_SECRET_CLEANUP_INCOMPLETE")
			}
		}()
	}
	if stat.Size() > 64*1024 {
		return nil, errors.New("HANDOFF_INPUT_TOO_LARGE")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("HANDOFF_INPUT_UNAVAILABLE")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 64*1024+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 64*1024 {
		return nil, errors.New("HANDOFF_INPUT_UNAVAILABLE")
	}
	return data, nil
}

func writeQuick(path string, value any, exclusive bool) (string, error) {
	path, err := quickFilePath(path)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > 64*1024 {
		return "", errors.New("INVALID_HANDOFF_RECORD")
	}
	if exclusive {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", errors.New("NEW_HANDOFF_FILE_REQUIRED")
		}
		if file.Close() != nil {
			return "", errors.New("HANDOFF_FILE_CLOSE_FAILED")
		}
	}
	if err := requirePrivateHandoffPaths([]string{path}); err != nil {
		return "", err
	}
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() {
		return "", errors.New("HANDOFF_REGULAR_FILE_REQUIRED")
	}
	data = append(data, '\n')
	if err := writeDurableState(path, data); err != nil {
		// Never leave a partially disclosed registration capability behind.
		if _, secret := value.(quickCapability); secret {
			if os.Remove(path) != nil {
				return "", errors.New("HANDOFF_SECRET_CLEANUP_INCOMPLETE")
			}
		}
		return "", err
	}
	return quickDigest(data), nil
}

func createQuickDirectory(path string) error {
	if err := requirePrivateRunnerParent(path); err != nil {
		return err
	}
	if os.Mkdir(path, 0700) != nil {
		return errors.New("NEW_HANDOFF_DIRECTORY_REQUIRED_NO_AUTOMATIC_RETRY")
	}
	if err := initializePrivateRunnerDirectory(path); err != nil {
		if os.Remove(path) != nil {
			return errors.Join(err, errors.New("HANDOFF_DIRECTORY_CLEANUP_INCOMPLETE"))
		}
		return err
	}
	return nil
}

func saveQuick(directory string, state quickState, initial bool) error {
	name := "state.json"
	if !initial {
		if _, err := loadQuick(directory); err != nil {
			return errors.New("HANDOFF_STATE_JOURNAL_UNRESOLVED")
		}
		_, number, err := latestQuickState(directory)
		if err != nil || number >= 100 {
			return errors.New("HANDOFF_STATE_JOURNAL_UNAVAILABLE")
		}
		name = fmt.Sprintf("state-%04d.json", number+1)
	}
	_, err := writeQuick(filepath.Join(directory, name), state, true)
	return err
}

// Immutable records retain the original request if a later write is interrupted.
// A malformed latest record never falls back to an earlier retryable state.
func latestQuickState(directory string) (string, int, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", 0, errors.New("HANDOFF_STATE_UNAVAILABLE")
	}
	number := 0
	for _, entry := range entries {
		var candidate int
		_, err := fmt.Sscanf(entry.Name(), "state-%04d.json", &candidate)
		if err == nil && entry.Name() == fmt.Sprintf("state-%04d.json", candidate) {
			if candidate > number {
				number = candidate
			}
		}
	}
	name := "state.json"
	if number > 0 {
		name = fmt.Sprintf("state-%04d.json", number)
	}
	return filepath.Join(directory, name), number, nil
}

func loadQuick(directory string) (quickState, error) {
	var state quickState
	path, _, err := latestQuickState(directory)
	if err != nil {
		return state, err
	}
	data, err := readQuick(path, false)
	if err != nil {
		return state, err
	}
	err = decodeQuick(data, &state)
	return state, err
}

func validQuickRequest(request quickRequest) bool {
	return request.Version == 1 && digestPattern.MatchString(request.Attempt) &&
		repoPattern.MatchString(request.Repository) && request.RepositoryID > 0 &&
		request.MemberID > 0 && quickLogin.MatchString(request.Login) &&
		runnerNamePattern.MatchString(request.Name) &&
		strings.HasSuffix(request.Name, "-"+request.Attempt[:32])
}

func quickLocked(directory string, out io.Writer, action func() error) (resultErr error) {
	lockPath, err := quickFilePath(filepath.Join(directory, "operation.lock"))
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return quickPending(out, "HANDOFF_LOCKED_INSPECT_BEFORE_RETRY")
	}
	if lock.Close() != nil {
		return quickPending(out, "HANDOFF_LOCK_UNRESOLVED")
	}
	defer func() {
		if os.Remove(lockPath) != nil {
			resultErr = errors.Join(resultErr, &CommandError{
				Code: "HANDOFF_LOCK_CLEANUP_INCOMPLETE", Exit: 2})
		}
	}()
	return action()
}

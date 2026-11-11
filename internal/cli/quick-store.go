package cli

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Quick requests are observations, never authority to issue a registration token.
type quickRequest struct {
	Attempt      string `json:"attempt"`
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repository_id"`
	Member       User   `json:"member"`
	Name         string `json:"name"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
}

type quickState struct {
	Request     quickRequest      `json:"request"`
	Role        string            `json:"role"`
	Phase       string            `json:"phase"`
	Directory   string            `json:"directory,omitempty"`
	Application RunnerApplication `json:"application"`
	RunnerID    int64             `json:"runner_id,omitempty"`
}

type quickHandoff struct {
	Request     quickRequest      `json:"request"`
	Application RunnerApplication `json:"application"`
	Token       string            `json:"token"`
	Expires     string            `json:"expires_at"`
}

type quickConfirmation struct {
	ObservedAt string       `json:"observed_at"`
	Expires    string       `json:"expires_at"`
	Request    quickRequest `json:"request"`
	RunnerID   int64        `json:"runner_id"`
}

const (
	quickInputLimit       = 32 * 1024
	quickStateFile        = "attempt.json"
	quickHandoffFile      = "handoff.json"
	quickConfirmationFile = "confirmation.json"
	quickRequestFile      = "request.json"
)

func quickNonce() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", errors.New("QUICK_RANDOM_UNAVAILABLE")
	}
	return hex.EncodeToString(data), nil
}

func quickDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func quickRead(path string, output any) ([]byte, error) {
	if !filepath.IsAbs(path) || hasGitAncestor(filepath.Dir(path)) {
		return nil, errors.New("PRIVATE_HANDOFF_OUTSIDE_WORKTREE_REQUIRED")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != filepath.Clean(path) {
		return nil, errors.New("UNTRUSTED_QUICK_FILE")
	}
	if err := requirePrivateQuickPaths([]string{filepath.Dir(path), path}); err != nil {
		return nil, err
	}
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > quickInputLimit {
		return nil, errors.New("INVALID_QUICK_FILE")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("QUICK_FILE_UNAVAILABLE")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, quickInputLimit+1))
	if err != nil || len(data) > quickInputLimit {
		return nil, errors.New("INVALID_QUICK_FILE")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(output) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("INVALID_QUICK_FILE")
	}
	return data, nil
}

func quickWrite(path string, value any) error {
	stat, statErr := os.Lstat(path)
	if statErr == nil && !stat.Mode().IsRegular() ||
		statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return errors.New("UNTRUSTED_QUICK_STATE_DESTINATION")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("INVALID_QUICK_STATE")
	}
	return writeDurableState(path, append(data, '\n'))
}

func quickNewDirectory(path string) error {
	if err := requirePrivateRunnerParent(path); err != nil {
		return err
	}
	if os.Mkdir(path, 0700) != nil {
		return errors.New("NEW_QUICK_DIRECTORY_REQUIRED")
	}
	if err := initializePrivateRunnerDirectory(path); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func quickValidateRequest(request quickRequest, repo Repository) error {
	if !digestPattern.MatchString(request.Attempt) ||
		request.Repository != repo.FullName || request.RepositoryID != repo.ID ||
		request.Member.ID <= 0 || request.Member.Type != "User" ||
		!runnerNamePattern.MatchString(request.Name) ||
		!quickLoginPattern.MatchString(request.Member.Login) ||
		request.OS != runtime.GOOS || request.Arch != runtime.GOARCH {
		return errors.New("QUICK_ATTEMPT_CONTEXT_MISMATCH_OR_UNSUPPORTED_HOST")
	}
	return nil
}

func quickLifetime(expires string, now time.Time) error {
	end, err := time.Parse(time.RFC3339, expires)
	// Leave the official two-minute configuration timeout inside provider validity.
	if err != nil || !end.After(now.Add(3*time.Minute)) {
		return errors.New("QUICK_TOKEN_MISSING_OR_INSUFFICIENT_LIFETIME")
	}
	return nil
}

func (proof quickConfirmation) fresh(now time.Time) bool {
	observed, observedErr := time.Parse(time.RFC3339, proof.ObservedAt)
	expires, expiresErr := time.Parse(time.RFC3339, proof.Expires)
	return observedErr == nil && expiresErr == nil && expires.After(now) &&
		!observed.After(now.Add(30*time.Second)) && expires.After(observed) &&
		!expires.After(observed.Add(5*time.Minute))
}

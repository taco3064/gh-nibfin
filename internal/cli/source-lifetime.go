package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const sourceRecordName = "operation.json"
const sourceRecordKind = "nibfin-core-source-operation"

type sourceOwner struct {
	PID   int
	Start string
	Boot  string
}

type sourceStamp struct {
	Identity string
	Size     int64
	Modified int64
	Mode     os.FileMode
}

type sourceRecord struct {
	Kind     string
	Owner    sourceOwner
	Group    int
	Phase    string
	Retained string
	Entries  map[string]sourceStamp
}

// A source is private to one operation. No selector reuses these directories.
// Release is the final consumer boundary, including pending/error outcomes.
// Installation intents and privileged deployments do not consume this path.
type coreSource struct {
	Directory  string
	parent     string
	name       string
	root       *os.Root
	record     sourceRecord
	recordInfo os.FileInfo
	removed    *sourceCleanupResult
}

func reserveCoreSource(parent string) (*coreSource, error) {
	path, err := os.MkdirTemp(parent, ".source-")
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	s := &coreSource{Directory: filepath.Join(path, "source"), parent: parent,
		name: filepath.Base(path), root: root,
		record: sourceRecord{Kind: sourceRecordKind, Owner: currentSourceOwner(),
			Phase: "extracting", Entries: map[string]sourceStamp{}}}
	if err := s.save(); err != nil {
		root.Close()
		return nil, err
	}
	return s, nil
}

func (s *coreSource) save() error {
	previous, statErr := s.root.Lstat(sourceRecordName)
	if s.recordInfo == nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			return errors.New("SOURCE_OWNERSHIP_RECORD_CHANGED")
		}
	} else if statErr != nil || !previous.Mode().IsRegular() ||
		!os.SameFile(previous, s.recordInfo) ||
		previous.Size() != s.recordInfo.Size() ||
		!previous.ModTime().Equal(s.recordInfo.ModTime()) {
		return errors.New("SOURCE_OWNERSHIP_RECORD_CHANGED")
	}
	data, err := json.Marshal(s.record)
	if err != nil {
		return err
	}
	// Pin the owned inode before writing. A pathname replacement after validation
	// must never be overwritten by a replacing rename or followed for writing.
	flags := os.O_WRONLY
	if s.recordInfo == nil {
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := s.root.OpenFile(sourceRecordName, flags, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := writeOwnedSourceRecord(file, s.recordInfo, data)
	if err != nil {
		return err
	}
	current, err := s.root.Lstat(sourceRecordName)
	if err != nil || !os.SameFile(current, info) {
		return errors.New("SOURCE_OWNERSHIP_RECORD_CHANGED")
	}
	s.recordInfo = info
	return nil
}

// The descriptor, rather than a replaceable pathname, owns the write target.
func writeOwnedSourceRecord(file *os.File, expected os.FileInfo,
	data []byte) (os.FileInfo, error) {
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if expected != nil && (!opened.Mode().IsRegular() ||
		!os.SameFile(opened, expected) || opened.Mode() != expected.Mode() ||
		opened.Size() != expected.Size() ||
		!opened.ModTime().Equal(expected.ModTime())) {
		return nil, errors.New("SOURCE_OWNERSHIP_RECORD_CHANGED")
	}
	// An interrupted write is unclassified and retained by reconciliation.
	// Never truncate before checking the opened inode against the owned record.
	_, writeErr := file.WriteAt(data, 0)
	if writeErr == nil {
		writeErr = file.Truncate(int64(len(data)))
	}
	syncErr := file.Sync()
	info, infoErr := file.Stat()
	if err := errors.Join(writeErr, syncErr, infoErr); err != nil {
		return nil, err
	}
	return info, nil
}

func (s *coreSource) extract(archive []byte) error {
	files, err := readCoreArchive(archive)
	if err != nil {
		return err
	}
	if err := s.root.Mkdir("source", 0700); err != nil {
		return err
	}
	root, err := s.root.OpenRoot("source")
	if err != nil {
		return err
	}
	defer root.Close()
	entries, extractErr := extractCoreFiles(files, root)
	for path, info := range entries {
		s.record.Entries[filepath.Join("source", path)] = stampSourceEntry(info)
	}
	info, err := s.root.Stat(".")
	if err != nil {
		return err
	}
	s.record.Entries["."] = stampSourceEntry(info)
	s.record.Phase = "prepared"
	return errors.Join(extractErr, s.save())
}

// A crash between starting a process and recording its group is deliberately
// distinguishable from a process known to have finished: it remains deferred.
func (s *coreSource) verifying() error {
	s.record.Phase = "starting"
	s.record.Group = -1
	return s.save()
}

func (s *coreSource) started(group int) error {
	s.record.Group = group
	s.record.Phase = "verifying"
	return s.save()
}

func (s *coreSource) finished() error {
	s.record.Phase = "verified"
	return s.save()
}

func (s *coreSource) Release() sourceCleanupResult {
	if s.removed != nil {
		return *s.removed
	}
	defer s.root.Close()
	// Never convert an interrupted start or incomplete extraction into a claim
	// that consumers are gone. The current operation can still report the cause.
	if s.record.Phase != "verified" && s.record.Phase != "prepared" {
		return sourceCleanupResult{s.name, "deferred", "incomplete source ownership"}
	}
	s.record.Phase = "released"
	if err := s.save(); err != nil {
		return sourceCleanupResult{s.name, "deferred", "release record unavailable"}
	}
	result := retireCoreSource(s.parent, s.name, s.root, s.record, observeSourceUse)
	if result.Status == "removed" {
		s.removed = &result
	}
	return result
}

type sourceCleanupResult struct {
	Entry  string `json:"entry"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func reportSourceCleanup(out interface{ Write([]byte) (int, error) },
	results []sourceCleanupResult) {
	if len(results) != 0 {
		// Housekeeping cannot replace an installation/provider failure, including
		// when the diagnostic writer itself is no longer available.
		_ = json.NewEncoder(out).Encode(map[string]any{"source_cleanup": results})
	}
}

func sourceCacheParent() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	cache, err = filepath.EvalSymlinks(cache)
	if err != nil {
		return "", err
	}
	parent := filepath.Join(cache, "Nibfin", "candidates", "core")
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("source cache namespace unavailable: %w", err)
	}
	if !sameSourcePath(resolved, parent) {
		return "", errors.New("source cache namespace is aliased")
	}
	return resolved, nil
}

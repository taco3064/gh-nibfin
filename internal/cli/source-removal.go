package cli

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

func captureSourceEntry(root *os.Root, path string, record sourceRecord) error {
	info, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !matchesSourceStamp(info, record.Entries[path]) {
		return errors.New("entry identity changed")
	}
	recordInfo, err := matchingSourceRecordInfo(root, record)
	if err != nil {
		return err
	}
	name, holding, err := reserveSourceRemoval(root)
	if err != nil {
		return err
	}
	defer holding.Close()
	info, err = holding.Stat(".")
	if err != nil {
		return err
	}
	held := filepath.Join(name, "entry")
	// Journal the staging directory and expected entry in the existing operation
	// record. A crash after rename is recovered by normal classified removal.
	record.Entries[name] = stampSourceEntry(info)
	record.Entries[held] = record.Entries[path]
	source := coreSource{root: root, record: record, recordInfo: recordInfo}
	err = source.save()
	if err != nil {
		_ = root.Remove(name) // Only an empty directory can be removed here.
		return err
	}
	if err := root.Rename(path, held); err != nil {
		_ = root.Remove(name)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := removeCapturedSourceEntry(holding, record.Entries[path]); err != nil {
		// Link restores a captured non-directory without replacing a new arrival.
		// If restoration cannot be done, retain it at the reported staging path.
		if root.Link(held, path) == nil {
			_ = holding.Remove("entry")
			_ = root.Remove(name)
			return fmt.Errorf("changed entry preserved at %s: %w", path, err)
		}
		return fmt.Errorf("entry preserved at %s: %w", held, err)
	}
	return root.Remove(name)
}

// Only the private captured name is unlinked. Concurrent arrivals at the public
// source pathname are never candidates for this removal.
func removeCapturedSourceEntry(holding *os.Root, expected sourceStamp) error {
	info, err := holding.Lstat("entry")
	if err != nil || !matchesSourceStamp(info, expected) {
		return errors.New("captured entry identity changed")
	}
	return holding.Remove("entry")
}

func reserveSourceRemoval(root *os.Root) (string, *os.Root, error) {
	name := ".retiring-" + rand.Text()
	if err := root.Mkdir(name, 0700); err != nil {
		return "", nil, err
	}
	holding, err := root.OpenRoot(name)
	if err != nil {
		return "", nil, err
	}
	return name, holding, nil
}

func removeSourceOwnershipRecord(root *os.Root, record sourceRecord) error {
	info, err := matchingSourceRecordInfo(root, record)
	if err != nil {
		return err
	}
	name, holding, err := reserveSourceRemoval(root)
	if err != nil {
		return err
	}
	defer holding.Close()
	if err := root.Rename(sourceRecordName, filepath.Join(name, "entry")); err != nil {
		_ = root.Remove(name)
		return err
	}
	if err := removeCapturedSourceEntry(holding, stampSourceEntry(info)); err != nil {
		return fmt.Errorf("ownership record preserved at %s/entry: %w", name, err)
	}
	return root.Remove(name)
}

func matchingSourceRecordInfo(root *os.Root,
	record sourceRecord) (os.FileInfo, error) {
	// Read and validate through one descriptor; never adopt the identity of a
	// record substituted after classification as permission to journal over it.
	file, err := root.Open(sourceRecordName)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(file)
	recordInfo, statErr := file.Stat()
	closeErr := file.Close()
	var current sourceRecord
	if errors.Join(readErr, statErr, closeErr) != nil ||
		json.Unmarshal(data, &current) != nil || !reflect.DeepEqual(current, record) {
		return nil, errors.New("ownership record changed")
	}
	return recordInfo, nil
}

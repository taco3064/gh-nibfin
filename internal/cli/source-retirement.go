package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
)

func sameSourcePath(a, b string) bool {
	return a == b || runtime.GOOS == "windows" && strings.EqualFold(a, b)
}

func reconcileCoreSources() []sourceCleanupResult {
	parent, err := sourceCacheParent()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []sourceCleanupResult{{"core", "deferred", err.Error()}}
	}
	return sweepCoreSources(parent, observeSourceUse)
}

func sweepCoreSources(parent string,
	observe func(sourceRecord) string) []sourceCleanupResult {
	root, err := os.OpenRoot(parent)
	if err != nil {
		return []sourceCleanupResult{{"core", "deferred", "namespace unreadable"}}
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return []sourceCleanupResult{{"core", "deferred", "namespace unreadable"}}
	}
	var results []sourceCleanupResult
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".verify-") {
			continue // Existing temporary-verifier cleanup retains its own policy.
		}
		result := sourceCleanupResult{name, "retained", "unclassified or shared source"}
		if !strings.HasPrefix(name, ".source-") || !entry.IsDir() {
			results = append(results, result)
			continue
		}
		operation, err := root.OpenRoot(name)
		if err != nil {
			result.Reason = "operation directory unavailable"
			results = append(results, result)
			continue
		}
		record, err := readSourceRecord(operation)
		if err != nil {
			result.Reason = "ownership record missing, stale or unclassified"
		} else {
			result = retireCoreSource(parent, name, operation, record, observe)
		}
		operation.Close()
		results = append(results, result)
	}
	return results
}

func readSourceRecord(root *os.Root) (sourceRecord, error) {
	var record sourceRecord
	info, err := root.Lstat(sourceRecordName)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return record, errors.New("unclassified ownership record")
	}
	data, err := root.ReadFile(sourceRecordName)
	if err != nil || json.Unmarshal(data, &record) != nil ||
		record.Kind != sourceRecordKind || len(record.Entries) == 0 {
		return record, errors.New("unclassified ownership record")
	}
	return record, nil
}

func retireCoreSource(parent, name string, root *os.Root, record sourceRecord,
	observe func(sourceRecord) string) sourceCleanupResult {
	result := sourceCleanupResult{name, "deferred", ""}
	if record.Retained != "" {
		result.Status, result.Reason = "retained", record.Retained
		return result
	}
	if reason := observe(record); reason != "" {
		result.Reason = reason
		return result
	}
	// Keep the namespace pinned throughout removal, including final unlink.
	parentRoot, err := os.OpenRoot(parent)
	if errors.Is(err, os.ErrNotExist) {
		result.Status, result.Reason = "removed", "already absent"
		return result
	}
	if err != nil {
		result.Reason = "source namespace observation unavailable"
		return result
	}
	defer parentRoot.Close()
	// Validate the opened operation and its namespace entry before touching data.
	info, err := parentRoot.Lstat(name)
	opened, openErr := root.Stat(".")
	if errors.Is(err, os.ErrNotExist) {
		result.Status, result.Reason = "removed", "already absent"
		return result
	}
	if err != nil || openErr != nil || !os.SameFile(info, opened) {
		result.Reason = "operation directory identity changed"
		return result
	}
	currentRecord, err := readSourceRecord(root)
	if err != nil || !reflect.DeepEqual(currentRecord, record) {
		result.Reason = "ownership record changed or unavailable"
		return result
	}
	paths, err := classifiedSourcePaths(root, record)
	if err != nil {
		result.Status, result.Reason = "retained", err.Error()
		return result
	}
	// Remove only individually classified entries through a pinned root. No
	// recursive pathname deletion can follow a substituted ancestor outside it.
	slices.SortFunc(paths, func(a, b string) int {
		return strings.Compare(b, a)
	})
	for _, path := range paths {
		if err := removeClassifiedSourceEntry(root, path, record); err != nil {
			result.Reason = "partial removal or changed entry; retry normal apply: " +
				err.Error()
			return result
		}
	}
	remaining, err := fs.ReadDir(root.FS(), ".")
	if err != nil || len(remaining) != 1 || remaining[0].Name() != sourceRecordName {
		result.Reason = "foreign additions preserved; operation record retained"
		return result
	}
	if err := removeSourceOwnershipRecord(root, record); err != nil {
		result.Reason = "source removed; ownership record removal pending: " + err.Error()
		return result
	}
	// Empty-directory removal is atomic and preserves concurrent additions.
	current, statErr := parentRoot.Lstat(name)
	if statErr == nil && os.SameFile(current, opened) {
		err = parentRoot.Remove(name)
	} else {
		err = statErr
		if err == nil {
			err = errors.New("operation identity changed")
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		result.Reason = "source removed; empty operation directory retained"
		return result
	}
	result.Status, result.Reason = "removed", "operation and consumers finished"
	return result
}

func classifiedSourcePaths(root *os.Root, record sourceRecord) ([]string, error) {
	var paths []string
	err := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry,
		walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceRecordName {
			return nil
		}
		info, err := root.Lstat(filepath.FromSlash(path))
		stamp, known := record.Entries[filepath.FromSlash(path)]
		if err != nil || !known || !matchesSourceStamp(info, stamp) {
			return errors.New("foreign, changed or symlinked entry preserved")
		}
		if path != "." {
			paths = append(paths, filepath.FromSlash(path))
		}
		return nil
	})
	return paths, err
}

func stampSourceEntry(info os.FileInfo) sourceStamp {
	return sourceStamp{sourceFileIdentity(info), info.Size(),
		info.ModTime().UnixNano(), info.Mode()}
}

func matchesSourceStamp(info os.FileInfo, stamp sourceStamp) bool {
	if info.Mode()&os.ModeSymlink != 0 || stamp.Identity == "" ||
		sourceFileIdentity(info) != stamp.Identity || info.Mode() != stamp.Mode {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular() &&
		info.Size() == stamp.Size && info.ModTime().UnixNano() == stamp.Modified
}

// Reserve and journal a private retirement destination before moving an entry.
// Validation after rename applies to the captured entry, rather than to a name
// which another writer can replace between validation and unlink.
func removeClassifiedSourceEntry(root *os.Root, path string,
	record sourceRecord) error {
	parent, err := root.OpenRoot(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	info, err := parent.Stat(".")
	parent.Close()
	if err != nil || !matchesSourceStamp(info, record.Entries[filepath.Dir(path)]) {
		return errors.New("parent identity changed")
	}
	return captureSourceEntry(root, path, record)
}

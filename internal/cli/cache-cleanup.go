package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

type cacheCleanupEntry struct {
	Entry  string `json:"entry"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type cacheCleanupReport struct {
	Entries []cacheCleanupEntry `json:"entries"`
}

type cacheUseObserver func(string) string

type cacheRetirement struct {
	selected string
	observe  cacheUseObserver
	retained func(string) string
	remove   func(*os.Root, string) error
}

func (r *cacheCleanupReport) add(name, status, reason string) {
	// Never disclose arbitrary filenames, process arguments or filesystem errors.
	if !commitPattern.MatchString(name) {
		name = "unclassified"
	}
	r.Entries = append(r.Entries, cacheCleanupEntry{name, status, reason})
}

func reconcileCoreCache(parent string, policy cacheRetirement) cacheCleanupReport {
	report := cacheCleanupReport{}
	root, err := os.OpenRoot(parent)
	if os.IsNotExist(err) {
		return report
	}
	if err != nil {
		report.add("", "deferred", "unknown_observation")
		return report
	}
	defer root.Close()
	if runtime.GOOS != "linux" {
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) != 0 {
			report.add("", "deferred", "unknown_observation")
		}
		return report
	}
	original, err := os.Lstat(parent)
	canonical, canonicalErr := filepath.EvalSymlinks(parent)
	if err != nil || canonicalErr != nil || canonical != parent ||
		!privateCacheInfo(original, true) {
		report.add("", "retained", "foreign_entry")
		return report
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		report.add("", "deferred", "unknown_observation")
		return report
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(parent, name)
		reason := ""
		switch {
		case path == policy.selected:
			reason = "selected_source"
		case !commitPattern.MatchString(name):
			reason = "foreign_entry"
		}
		if reason == "" && policy.retained != nil {
			reason = policy.retained(name)
		}
		var tree *cacheTree
		if reason == "" {
			tree, reason = classifyCache(root, name)
		}
		if reason == "" {
			reason = observeCacheUse(policy.observe, path)
		}
		if reason != "" {
			report.add(name, "retained", reason)
			continue
		}
		// Repeat namespace, ownership, content and consumer observations at removal.
		current, statErr := os.Lstat(parent)
		resolved, resolveErr := filepath.EvalSymlinks(parent)
		if statErr != nil || resolveErr != nil || resolved != parent ||
			!os.SameFile(original, current) {
			report.add(name, "deferred", "changed_identity")
			continue
		}
		reason = retireCacheTree(root, name, tree, func() string {
			if policy.retained != nil {
				if reason := policy.retained(name); reason != "" {
					return reason
				}
			}
			reason := observeCacheUse(policy.observe, path)
			if !cacheDirectoryUnchanged(parent, original) {
				return "changed_identity"
			}
			return reason
		}, policy.remove)
		if reason == "" {
			report.add(name, "removed", "unused_owned_source")
		} else {
			report.add(name, "deferred", reason)
		}
	}
	return report
}

func observeCacheUse(observe cacheUseObserver, path string) string {
	if observe == nil {
		return "unknown_observation"
	}
	return observe(path)
}

func reportCacheCleanup(out io.Writer, report cacheCleanupReport) {
	if len(report.Entries) == 0 {
		return
	}
	// Reporting has no bearing on an already observed installation or deletion.
	_ = json.NewEncoder(out).Encode(map[string]any{
		"status": "source_cache_housekeeping", "cleanup": report,
	})
}

// The returned boundary releases the selected source on every owning exit,
// including a failed operation or failed output delivery.
func beginCacheCleanup(out io.Writer, selected string,
	reconcile func(string) cacheCleanupReport) func() {
	reportCacheCleanup(out, reconcile(selected))
	return func() { reportCacheCleanup(out, reconcile("")) }
}

func reconcileCLIInvocation(selectedCommit string) cacheCleanupReport {
	unknown := cacheCleanupReport{}
	unknown.add("", "deferred", "unknown_observation")
	cache, err := os.UserCacheDir()
	if err != nil {
		return unknown
	}
	cache, err = filepath.EvalSymlinks(cache)
	if os.IsNotExist(err) {
		return cacheCleanupReport{}
	}
	if err != nil {
		return unknown
	}
	parent := filepath.Join(cache, "Nibfin", "candidates", "core")
	selected := ""
	if selectedCommit != "" {
		selected = filepath.Join(parent, selectedCommit)
	}
	return reconcileCoreCache(parent, cacheRetirement{
		selected: selected, observe: nativeCacheUse, retained: retainedCacheSource,
	})
}

func cacheDirectoryUnchanged(directory string, original os.FileInfo) bool {
	current, err := os.Lstat(directory)
	canonical, resolveErr := filepath.EvalSymlinks(directory)
	return err == nil && resolveErr == nil && canonical == directory &&
		os.SameFile(original, current)
}

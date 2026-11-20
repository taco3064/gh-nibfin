package cli

import (
	"os"
	"path/filepath"
)

// The reserving invocation already authenticated this inventory. Identity and
// content checks prevent failure cleanup from recursively erasing a replacement.
func cleanupReservedCache(directory string, info os.FileInfo,
	files map[string]coreArchiveFile) string {
	return cleanupReservedCacheObserved(directory, info, files, nil)
}

func cleanupReservedCacheObserved(directory string, info os.FileInfo,
	files map[string]coreArchiveFile, observe cacheUseObserver) string {
	parent := filepath.Dir(directory)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !cacheDirectoryUnchanged(parent, parentInfo) {
		return "changed_identity"
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return "removal_failure"
	}
	defer root.Close()
	name := filepath.Base(directory)
	current, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil || info == nil || !os.SameFile(info, current) {
		return "changed_identity"
	}
	tree := &cacheTree{reserved: true, info: info, files: map[string]coreManifestFile{}}
	for name, file := range files {
		mode := "100644"
		if file.mode == 0755 {
			mode = "100755"
		}
		tree.files[name] = coreManifestFile{Path: name, Mode: mode,
			Size: int64(len(file.data)), Hash: sha256Hash(file.data)}
	}
	return retireCacheTree(root, name, tree, func() string {
		reason := ""
		if observe != nil {
			reason = observe(directory)
		}
		if !cacheDirectoryUnchanged(parent, parentInfo) {
			return "changed_identity"
		}
		return reason
	}, nil)
}

func cleanupReservedReport(directory string, info os.FileInfo,
	files map[string]coreArchiveFile, publish func(cacheCleanupReport),
	observe cacheUseObserver) {
	reason := cleanupReservedCacheObserved(directory, info, files, observe)
	report := cacheCleanupReport{}
	if reason == "" {
		report.add(filepath.Base(directory), "removed", "owned_scratch")
	} else {
		report.add(filepath.Base(directory), "deferred", reason)
	}
	if publish != nil {
		publish(report)
	}
}

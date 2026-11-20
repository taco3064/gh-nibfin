package cli

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

type cacheTree struct {
	reserved   bool
	info       os.FileInfo
	files      map[string]coreManifestFile
	identities map[string]os.FileInfo
}

func classifyCache(root *os.Root, name string) (*cacheTree, string) {
	info, err := root.Lstat(name)
	if err != nil || !privateCacheInfo(info, true) {
		return nil, "foreign_entry"
	}
	source, err := root.OpenRoot(name)
	if err != nil {
		return nil, "changed_identity"
	}
	defer source.Close()
	data, err := source.ReadFile(coreManifestName)
	if err != nil {
		return nil, "foreign_entry"
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil, "foreign_entry"
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return nil, "foreign_entry"
	}
	pin := SourcePin{Repository: officialCore, Commit: name,
		ManifestHash: sha256Hash(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")))}
	declarations, err := parseCoreManifest(data, pin)
	if err != nil {
		return nil, "foreign_entry"
	}
	files := map[string]coreManifestFile{}
	for _, file := range declarations {
		files[file.Path] = file
	}
	// These are the maintained archive's mandatory source entry points.
	if _, ok := files["bin/nibfin-core.mjs"]; !ok {
		return nil, "foreign_entry"
	}
	if _, ok := files[".node-version"]; !ok {
		return nil, "foreign_entry"
	}
	files[coreManifestName] = coreManifestFile{Path: coreManifestName,
		Mode: "100644", Size: int64(len(data)), Hash: sha256Hash(data)}
	tree := &cacheTree{info: info, files: files}
	if reason := tree.inspect(source); reason != "" {
		return nil, reason
	}
	return tree, ""
}

func (tree *cacheTree) inspect(root *os.Root) string {
	allowed := map[string]bool{".": true}
	for name := range tree.files {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			allowed[dir] = true
		}
	}
	identities := map[string]os.FileInfo{}
	err := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry,
		walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if name == "." && !os.SameFile(tree.info, info) {
			return fs.ErrExist
		}
		if old, exists := tree.identities[name]; exists && !os.SameFile(old, info) {
			return fs.ErrExist
		}
		if info.IsDir() != entry.IsDir() ||
			(!info.IsDir() && !info.Mode().IsRegular()) {
			return fs.ErrPermission
		}
		if !tree.reserved && !privateCacheInfo(info, entry.IsDir()) {
			return fs.ErrPermission
		}
		if entry.IsDir() {
			if !allowed[name] {
				return fs.ErrInvalid
			}
		} else {
			declared, ok := tree.files[filepath.ToSlash(name)]
			if !ok {
				return fs.ErrInvalid
			}
			data, err := root.ReadFile(name)
			if err != nil {
				return err
			}
			mode := "100644"
			if info.Mode().Perm() == 0700 {
				mode = "100755"
			}
			if !declared.matches(mode, info.Size(), sha256Hash(data)) {
				return fs.ErrInvalid
			}
		}
		identities[name] = info
		return nil
	})
	if err == fs.ErrExist {
		return "changed_identity"
	}
	if err != nil {
		return "foreign_entry"
	}
	tree.identities = identities
	return ""
}

func retireCacheTree(parent *os.Root, name string, tree *cacheTree,
	observe func() string, remove func(*os.Root, string) error) string {
	if remove == nil {
		remove = (*os.Root).Remove
	}
	root, err := parent.OpenRoot(name)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		return "changed_identity"
	}
	defer root.Close()
	if reason := tree.inspect(root); reason != "" {
		return reason
	}
	names := make([]string, 0, len(tree.identities))
	for item := range tree.identities {
		if item != "." && item != coreManifestName {
			names = append(names, item)
		}
	}
	// Children before parents; ownership manifest survives interrupted removal.
	sort.Slice(names, func(i, j int) bool { return names[i] > names[j] })
	names = append(names, coreManifestName)
	for _, item := range names {
		if reason := observe(); reason != "" {
			return reason
		}
		current, err := parent.Lstat(name)
		if err != nil || !os.SameFile(tree.info, current) {
			return "changed_identity"
		}
		info, err := root.Lstat(item)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !os.SameFile(tree.identities[item], info) {
			return "changed_identity"
		}
		// Reclassify the whole remaining tree before every destructive step.
		if reason := tree.inspect(root); reason != "" {
			return reason
		}
		if reason := observe(); reason != "" {
			return reason
		}
		// Consumer observation may block while paths or contents change.
		// Validate again after it, with no observer between validation and removal.
		current, err = parent.Lstat(name)
		if err != nil || !os.SameFile(tree.info, current) {
			return "changed_identity"
		}
		if reason := tree.inspect(root); reason != "" {
			return reason
		}
		if err := remove(root, item); err != nil && !os.IsNotExist(err) {
			return "removal_failure"
		}
	}
	if reason := observe(); reason != "" {
		return reason
	}
	current, err := parent.Lstat(name)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil || !os.SameFile(tree.info, current) {
		return "changed_identity"
	}
	if reason := tree.inspect(root); reason != "" {
		return reason
	}
	if err := remove(parent, name); err != nil && !os.IsNotExist(err) {
		return "removal_failure"
	}
	return ""
}

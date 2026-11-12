package cli

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

type upgradeTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type upgradeTree struct {
	SHA       string             `json:"sha"`
	Truncated bool               `json:"truncated"`
	Entries   []upgradeTreeEntry `json:"tree"`
}

func treeHash(entries []upgradeTreeEntry) string {
	entries = append([]upgradeTreeEntry(nil), entries...)
	key := func(entry upgradeTreeEntry) string {
		if entry.Type == "tree" {
			return entry.Path + "/"
		}
		return entry.Path
	}
	sort.Slice(entries, func(i, j int) bool { return key(entries[i]) < key(entries[j]) })
	var body bytes.Buffer
	for _, entry := range entries {
		fmt.Fprintf(&body, "%s %s\x00", strings.TrimLeft(entry.Mode, "0"), entry.Path)
		sha, _ := hex.DecodeString(entry.SHA)
		body.Write(sha)
	}
	return gitObjectHash("tree", body.Bytes())
}

// Rehash the complete baseline before deriving the exact target tree. This also
// proves that unrelated files, executable modes and submodule pins are preserved.
func targetUpgradeTree(tree upgradeTree, changes []upgradeChange) (string, error) {
	invalid := errors.New("UPGRADE_BASE_TREE_UNVERIFIABLE")
	if tree.Truncated || !commitPattern.MatchString(tree.SHA) {
		return "", invalid
	}
	directories := map[string][]upgradeTreeEntry{".": {}}
	want := map[string]string{".": tree.SHA}
	seen := map[string]bool{}
	for _, entry := range tree.Entries {
		if entry.Path == "." || path.Clean(entry.Path) != entry.Path ||
			strings.HasPrefix(entry.Path, "/") || strings.HasPrefix(entry.Path, "../") ||
			seen[entry.Path] || !commitPattern.MatchString(entry.SHA) {
			return "", invalid
		}
		seen[entry.Path] = true
		switch entry.Type + ":" + entry.Mode {
		case "tree:040000":
			directories[entry.Path] = nil
			want[entry.Path] = entry.SHA
		case "blob:100644", "blob:100755", "blob:120000", "commit:160000":
		default:
			return "", invalid
		}
	}
	for _, entry := range tree.Entries {
		parent := path.Dir(entry.Path)
		if _, ok := directories[parent]; !ok {
			return "", invalid
		}
		entry.Path = path.Base(entry.Path)
		directories[parent] = append(directories[parent], entry)
	}
	ordered := make([]string, 0, len(directories))
	for directory, entries := range directories {
		if treeHash(entries) != want[directory] {
			return "", invalid
		}
		ordered = append(ordered, directory)
	}
	for _, change := range changes {
		parent := path.Dir(change.Path)
		entries, ok := directories[parent]
		if !ok {
			return "", errors.New("UNSUPPORTED_UPGRADE_RECIPE_DIRECTORY")
		}
		name := path.Base(change.Path)
		fresh := make([]upgradeTreeEntry, 0, len(entries)+1)
		found := false
		for _, entry := range entries {
			if entry.Path != name {
				fresh = append(fresh, entry)
				continue
			}
			if change.Before == nil || entry.Mode != "100644" ||
				entry.Type != "blob" || entry.SHA != blobHash([]byte(*change.Before)) {
				return "", errors.New("UPGRADE_BASE_CONTENT_OR_MODE_CHANGED")
			}
			found = true
		}
		if found != (change.Before != nil) {
			return "", invalid
		}
		if change.After != nil {
			fresh = append(fresh, upgradeTreeEntry{Path: name, Mode: "100644",
				Type: "blob", SHA: blobHash([]byte(*change.After))})
		}
		directories[parent] = fresh
	}
	// Descendants precede parents; tree entries contain already updated hashes.
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, directory := range ordered {
		sha := treeHash(directories[directory])
		if directory == "." {
			return sha, nil
		}
		parent := path.Dir(directory)
		for i, entry := range directories[parent] {
			if entry.Path == path.Base(directory) {
				directories[parent][i].SHA = sha
			}
		}
	}
	return "", invalid
}

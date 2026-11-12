package cli

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type upgradeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type upgradeTree struct {
	SHA       string         `json:"sha"`
	Truncated bool           `json:"truncated"`
	Entries   []upgradeEntry `json:"tree"`
}

type upgradeCommit struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
	Message string `json:"message"`
}

func gitObjectHash(kind string, data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func treeHash(entries map[string]upgradeEntry, directory string) string {
	children := []upgradeEntry{}
	for path, entry := range entries {
		rest, ok := strings.CutPrefix(path, directory)
		if ok && !strings.Contains(rest, "/") {
			entry.Path = rest
			if entry.Type == "tree" {
				entry.SHA = treeHash(entries, path+"/")
			}
			children = append(children, entry)
		}
	}
	key := func(entry upgradeEntry) string {
		if entry.Type == "tree" {
			return entry.Path + "/"
		}
		return entry.Path
	}
	sort.Slice(children, func(i, j int) bool { return key(children[i]) < key(children[j]) })
	var data bytes.Buffer
	for _, entry := range children {
		mode := strings.TrimLeft(entry.Mode, "0")
		fmt.Fprintf(&data, "%s %s\x00", mode, entry.Path)
		hash, _ := hex.DecodeString(entry.SHA)
		data.Write(hash)
	}
	return gitObjectHash("tree", data.Bytes())
}

func readUpgradeTree(api API, base, sha string) (map[string]upgradeEntry, error) {
	var tree upgradeTree
	endpoint := base + "/git/trees/" + sha + "?recursive=1"
	if err := api.Call("GET", endpoint, nil, &tree); err != nil {
		return nil, err
	}
	if tree.SHA != sha || tree.Truncated || len(tree.Entries) > 100000 {
		return nil, errors.New("UPGRADE_TREE_UNAVAILABLE_OR_TRUNCATED")
	}
	entries := map[string]upgradeEntry{}
	for _, entry := range tree.Entries {
		if entry.Path == "" || !commitPattern.MatchString(entry.SHA) {
			return nil, errors.New("INVALID_UPGRADE_TREE")
		}
		for _, part := range strings.Split(entry.Path, "/") {
			if part == "" || part == "." || part == ".." || strings.ContainsRune(part, 0) {
				return nil, errors.New("INVALID_UPGRADE_TREE")
			}
		}
		if _, exists := entries[entry.Path]; exists {
			return nil, errors.New("INVALID_UPGRADE_TREE")
		}
		valid := entry.Type == "tree" && entry.Mode == "040000" ||
			entry.Type == "blob" && (entry.Mode == "100644" ||
				entry.Mode == "100755" || entry.Mode == "120000") ||
			entry.Type == "commit" && entry.Mode == "160000"
		if !valid {
			return nil, errors.New("INVALID_UPGRADE_TREE")
		}
		entries[entry.Path] = entry
	}
	for path, entry := range entries {
		if index := strings.LastIndex(path, "/"); index >= 0 {
			if entries[path[:index]].Type != "tree" {
				return nil, errors.New("INCOMPLETE_UPGRADE_TREE")
			}
		}
		if entry.Type == "tree" && treeHash(entries, path+"/") != entry.SHA {
			return nil, errors.New("UPGRADE_TREE_HASH_MISMATCH")
		}
	}
	if treeHash(entries, "") != sha {
		return nil, errors.New("UPGRADE_TREE_HASH_MISMATCH")
	}
	return entries, nil
}

func changedUpgradeTree(plan upgradePlan) map[string]upgradeEntry {
	entries := map[string]upgradeEntry{}
	for path, entry := range plan.entries {
		entries[path] = entry
	}
	for _, change := range plan.Changes {
		if change.Kind == "deleted" {
			delete(entries, change.Path)
			continue
		}
		entries[change.Path] = upgradeEntry{Path: change.Path, Mode: "100644",
			Type: "blob", SHA: blobHash([]byte(change.Content))}
		parts := strings.Split(change.Path, "/")
		for i := 1; i < len(parts); i++ {
			path := strings.Join(parts[:i], "/")
			entries[path] = upgradeEntry{Path: path, Mode: "040000", Type: "tree"}
		}
	}
	return entries
}

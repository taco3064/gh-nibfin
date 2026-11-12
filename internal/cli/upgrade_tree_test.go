package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGitObjectHashKnownVectors(t *testing.T) {
	if gitObjectHash("tree", nil) != "4b825dc642cb6eb9a060e54bf8d69288fbee4904" ||
		blobHash([]byte("test content\n")) != "d670460b4b4aece5915caf5c68d12f560a9fe3e4" {
		t.Fatal("Git object identities changed")
	}
}

func TestUpgradeTreeRejectsTruncationModeAndUnrelatedChanges(t *testing.T) {
	f, selection, _ := upgradeFixture(t)
	plan, err := planUpgrade(f, "example/project", selection)
	if err != nil {
		t.Fatal(err)
	}
	var baseCommit upgradeCommit
	if err := f.Call("GET", upgradeRepo+"/git/commits/"+plan.Head,
		nil, &baseCommit); err != nil {
		t.Fatal(err)
	}
	var baseline upgradeTree
	if err := f.Call("GET", upgradeRepo+"/git/trees/"+baseCommit.Tree.SHA+"?recursive=1",
		nil, &baseline); err != nil {
		t.Fatal(err)
	}
	for _, problem := range []string{"truncated", "identity", "duplicate", "path",
		"unrelated", "modified-mode"} {
		data, _ := json.Marshal(baseline)
		var tree upgradeTree
		_ = json.Unmarshal(data, &tree)
		switch problem {
		case "truncated":
			tree.Truncated = true
		case "identity":
			tree.SHA = strings.Repeat("f", 40)
		case "duplicate":
			tree.Entries = append(tree.Entries, tree.Entries[0])
		case "path":
			tree.Entries[0].Path = "../outside"
		case "unrelated":
			for i, entry := range tree.Entries {
				if entry.Path == "unrelated" {
					tree.Entries[i].SHA = strings.Repeat("f", 40)
				}
			}
		case "modified-mode":
			for i, entry := range tree.Entries {
				if entry.Path == ".github/workflows/nibfin-wake.yml" {
					tree.Entries[i].Mode = "100755"
				}
			}
		}
		if _, err := targetUpgradeTree(tree, plan.Changes); err == nil {
			t.Fatal("unverifiable tree accepted: ", problem)
		}
	}
}

func TestUpgradeTreePreservesExecutableSymlinkAndSubmoduleIdentities(t *testing.T) {
	entries := []upgradeTreeEntry{
		{Path: "managed", Mode: "100644", Type: "blob", SHA: blobHash([]byte("old"))},
		{Path: "executable", Mode: "100755", Type: "blob", SHA: strings.Repeat("1", 40)},
		{Path: "link", Mode: "120000", Type: "blob", SHA: strings.Repeat("2", 40)},
		{Path: "module", Mode: "160000", Type: "commit", SHA: strings.Repeat("3", 40)},
	}
	before, after := "old", "new"
	baseline := upgradeTree{SHA: treeHash(entries), Entries: entries}
	got, err := targetUpgradeTree(baseline,
		[]upgradeChange{{Path: "managed", Before: &before, After: &after}})
	entries[0].SHA = blobHash([]byte(after))
	if err != nil || got != treeHash(entries) {
		t.Fatalf("unrelated file identities were not preserved: %s %v", got, err)
	}
}

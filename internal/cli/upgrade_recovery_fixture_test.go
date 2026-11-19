package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func (f *upgradeAPI) nativeRebase(input, output any) error {
	body := input.(map[string]any)
	variables := body["variables"].(map[string]any)
	args := variables["input"].(map[string]any)
	pull := f.data[upgradeRepo+"/pulls/9"].(upgradePull)
	refPath := upgradeRepo + "/git/ref/heads/" + url.PathEscape(upgradeBranchName)
	ref := f.data[refPath].(reference)
	if body["query"] != upgradeRebaseMutation || args["updateMethod"] != "REBASE" ||
		args["pullRequestId"] != pull.NodeID || args["expectedHeadOid"] != ref.Object.SHA {
		return errors.New("EXPECTED_HEAD_OR_NATIVE_REBASE_CONTRACT_MISMATCH")
	}
	response := map[string]any{"data": map[string]any{
		"updatePullRequestBranch": map[string]any{"clientMutationId": nil}}}
	switch f.rebaseResult {
	case "conflict":
		response = map[string]any{"errors": []map[string]string{
			{"message": "Branch cannot be rebased due to conflicts"}}}
	case "missing-data":
		response = map[string]any{"data": nil}
	case "unfinished":
		return errors.New("REBASE_NOT_STARTED")
	default:
		current := f.data[upgradeRepo+"/git/ref/heads/main"].(reference).Object.SHA
		old := f.data[upgradeRepo+"/git/commits/"+ref.Object.SHA].(upgradeCommit)
		parent := f.data[upgradeRepo+"/git/commits/"+current].(upgradeCommit)
		tree := f.data[upgradeRepo+"/git/trees/"+parent.Tree.SHA+"?recursive=1"].(upgradeTree)
		entries := map[string]upgradeEntry{}
		for _, entry := range tree.Entries {
			entries[entry.Path] = entry
		}
		// Apply the original proposal delta over the new default branch.
		original := f.data[upgradeRepo+"/git/commits/"+old.Parents[0].SHA].(upgradeCommit)
		before := f.data[upgradeRepo+"/git/trees/"+
			original.Tree.SHA+"?recursive=1"].(upgradeTree)
		after := f.data[upgradeRepo+"/git/trees/"+old.Tree.SHA+"?recursive=1"].(upgradeTree)
		oldEntries := map[string]upgradeEntry{}
		nextEntries := map[string]upgradeEntry{}
		for _, entry := range before.Entries {
			oldEntries[entry.Path] = entry
		}
		for _, entry := range after.Entries {
			nextEntries[entry.Path] = entry
			if entry.Type != "tree" && oldEntries[entry.Path] != entry {
				entries[entry.Path] = entry
			}
		}
		for path, entry := range oldEntries {
			if _, exists := nextEntries[path]; !exists && entry.Type != "tree" {
				delete(entries, path)
			}
		}
		old.Tree.SHA = fixtureTree(f.fakeAPI, entries)
		old.Parents = append(old.Parents[:0:0], struct {
			SHA string `json:"sha"`
		}{current})
		old.SHA = gitObjectHash("commit", []byte(current+old.Tree.SHA+old.Message))
		f.data[upgradeRepo+"/git/commits/"+old.SHA] = old
		ref.Object.SHA = old.SHA
		f.data[refPath] = ref
		pull.Head.SHA = old.SHA
		pull.Base.SHA = current
		f.data[upgradeRepo+"/pulls/9"] = pull
		f.data[pullListPath(upgradeBranchName)] = []upgradePull{pull}
	}
	data, _ := json.Marshal(response)
	return json.Unmarshal(data, output)
}

func advanceUpgradeBase(t *testing.T, f *upgradeAPI, plan upgradePlan) upgradePlan {
	t.Helper()
	next := strings.Repeat("b", 40)
	entries := map[string]upgradeEntry{}
	for path, entry := range f.baseEntries {
		entries[path] = entry
	}
	entries["main-change.txt"] = upgradeEntry{Path: "main-change.txt", Mode: "100644",
		Type: "blob", SHA: blobHash([]byte("unrelated new main content"))}
	commit := upgradeCommit{SHA: next}
	commit.Parents = append(commit.Parents, struct {
		SHA string `json:"sha"`
	}{plan.Base})
	commit.Tree.SHA = fixtureTree(f.fakeAPI, entries)
	f.data[upgradeRepo+"/git/commits/"+next] = commit
	ref := reference{Ref: "refs/heads/main"}
	ref.Object.SHA = next
	f.data[upgradeRepo+"/git/ref/heads/main"] = ref
	for path, text := range f.installed {
		f.data[upgradeRepo+"/contents/"+path+"?ref="+next] = encoded([]byte(text))
	}
	f.data[upgradeRepo+"/compare/"+next+"..."+plan.Base] = map[string]any{
		"status": "behind", "ahead_by": 0, "behind_by": 1,
		"base_commit":       map[string]string{"sha": next},
		"merge_base_commit": map[string]string{"sha": plan.Base}}
	if value, exists := f.data[upgradeRepo+"/pulls/9"]; exists {
		pull := value.(upgradePull)
		pull.Base.SHA = next
		f.data[upgradeRepo+"/pulls/9"] = pull
		f.data[pullListPath(upgradeBranchName)] = []upgradePull{pull}
	}
	current, err := planUpgrade(f, "example/project", "latest")
	if err != nil {
		t.Fatal(err)
	}
	return current
}

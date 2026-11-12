package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"
)

type upgradeIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Date  string `json:"date"`
}

type upgradeCommit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Tree    struct {
		SHA string `json:"sha"`
	} `json:"tree"`
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
	Author    upgradeIdentity `json:"author"`
	Committer upgradeIdentity `json:"committer"`
}

func upgradeDigest(value any) string {
	data, _ := json.Marshal(value)
	return sha256Hash(data)
}

func upgradeBranchName(plan upgradePlan) string {
	return "nibfin/upgrade-" + upgradeDigest(plan.Target.Pins)
}

func upgradeCommitIdentity() upgradeIdentity {
	// Deterministic content-addressed objects permit authoritative reconciliation
	// after interruption without a local journal or untracked external retries.
	return upgradeIdentity{"Nibfin upgrade", "noreply@github.com", "2000-01-01T00:00:00Z"}
}

func expectedUpgradeCommit(plan upgradePlan, tree string) (string, string) {
	message := "Upgrade verified Nibfin sources\n\nPlan: " + upgradeDigest(plan)
	actor := upgradeCommitIdentity()
	date, _ := time.Parse(time.RFC3339, actor.Date)
	identity := fmt.Sprintf("%s <%s> %d +0000", actor.Name, actor.Email, date.Unix())
	body := fmt.Sprintf("tree %s\nparent %s\nauthor %s\ncommitter %s\n\n%s",
		tree, plan.Head, identity, identity, message)
	return gitObjectHash("commit", []byte(body)), message
}

func verifyUpgradeCommit(api API, base, sha, tree, parent, message string) error {
	var observed upgradeCommit
	if err := api.Call("GET", base+"/git/commits/"+sha, nil, &observed); err != nil {
		return err
	}
	identity := upgradeCommitIdentity()
	if observed.SHA != sha || observed.Tree.SHA != tree ||
		len(observed.Parents) != 1 || observed.Parents[0].SHA != parent ||
		observed.Message != message || observed.Author != identity ||
		observed.Committer != identity {
		return errors.New("UPGRADE_COMMIT_READBACK_MISMATCH")
	}
	return nil
}

func prepareUpgradeObjects(api API, plan upgradePlan) (string, error) {
	base := "repos/" + plan.Repository.FullName
	var commit upgradeCommit
	if err := api.Call("GET", base+"/git/commits/"+plan.Head, nil, &commit); err != nil {
		return "", err
	}
	if commit.SHA != plan.Head || !commitPattern.MatchString(commit.Tree.SHA) {
		return "", errors.New("UPGRADE_BASE_COMMIT_UNVERIFIABLE")
	}
	var baseline upgradeTree
	if err := api.Call("GET", base+"/git/trees/"+commit.Tree.SHA+"?recursive=1",
		nil, &baseline); err != nil {
		return "", err
	}
	if baseline.SHA != commit.Tree.SHA {
		return "", errors.New("UPGRADE_BASE_TREE_MISMATCH")
	}
	target, err := targetUpgradeTree(baseline, plan.Changes)
	if err != nil {
		return "", err
	}
	sha, message := expectedUpgradeCommit(plan, target)
	var branch reference
	ref := base + "/git/ref/heads/" + url.PathEscape(upgradeBranchName(plan))
	if err := api.Call("GET", ref, nil, &branch); err == nil {
		if branch.Object.SHA != sha {
			return "", errors.New("UPGRADE_BRANCH_CONFLICT_INSPECT_BEFORE_RETRY")
		}
	} else if err.Error() != "NOT_FOUND" {
		return "", err
	}
	if err := verifyUpgradeCommit(api, base, sha, target, plan.Head, message); err == nil {
		return sha, nil
	} else if err.Error() != "NOT_FOUND" {
		return "", err
	}
	// Query by the computed identity before creating an object. A missing response
	// never becomes permission to create a different object or move a reference.
	var existing upgradeTree
	err = api.Call("GET", base+"/git/trees/"+target, nil, &existing)
	if err != nil && err.Error() != "NOT_FOUND" {
		return "", err
	}
	if err != nil {
		entries := make([]map[string]any, 0, len(plan.Changes))
		for _, change := range plan.Changes {
			entry := map[string]any{"path": change.Path, "mode": "100644", "type": "blob"}
			if change.After == nil {
				entry["sha"] = nil
			} else {
				entry["content"] = *change.After
			}
			entries = append(entries, entry)
		}
		_ = api.Call("POST", base+"/git/trees", map[string]any{
			"base_tree": baseline.SHA, "tree": entries}, nil)
		err = api.Call("GET", base+"/git/trees/"+target, nil, &existing)
	}
	if err != nil || existing.Truncated || existing.SHA != target ||
		treeHash(existing.Entries) != target {
		return "", errors.New("UPGRADE_TREE_PENDING_INSPECT_BEFORE_RETRY")
	}
	identity := upgradeCommitIdentity()
	_ = api.Call("POST", base+"/git/commits", map[string]any{
		"message": message, "tree": target, "parents": []string{plan.Head},
		"author": identity, "committer": identity}, nil)
	if err := verifyUpgradeCommit(api, base, sha, target, plan.Head, message); err != nil {
		return "", errors.New("UPGRADE_COMMIT_PENDING_INSPECT_BEFORE_RETRY")
	}
	return sha, nil
}

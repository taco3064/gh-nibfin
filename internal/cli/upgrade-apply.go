package cli

import (
	"errors"
	"net/url"
)

const upgradeAuthor = "Nibfin <nibfin@users.noreply.github.com>"
const upgradeDate = "2000-01-01T00:00:00Z"

func upgradeMessage(plan upgradePlan) string {
	return "Propose verified Nibfin source upgrade\n\nPlan: " + plan.ID + "\n"
}

func upgradeCommitID(plan upgradePlan, tree string) string {
	// Explicit metadata makes unknown commit responses recoverable by object ID.
	data := "tree " + tree + "\nparent " + plan.Base + "\nauthor " + upgradeAuthor +
		" 946684800 +0000\ncommitter " + upgradeAuthor + " 946684800 +0000\n\n" +
		upgradeMessage(plan)
	return gitObjectHash("commit", []byte(data))
}

func verifyUpgradeCommit(api API, base, sha, tree string, plan upgradePlan) error {
	var commit upgradeCommit
	if err := api.Call("GET", base+"/git/commits/"+sha, nil, &commit); err != nil {
		return err
	}
	if commit.SHA != sha || commit.Tree.SHA != tree || len(commit.Parents) != 1 ||
		commit.Parents[0].SHA != plan.Base || commit.Message != upgradeMessage(plan) {
		return errors.New("UPGRADE_COMMIT_READBACK_MISMATCH")
	}
	_, err := readUpgradeTree(api, base, tree)
	return err
}

func ensureUpgradeObjects(api API, plan upgradePlan, version string) (string, error) {
	base := "repos/" + plan.Repository.FullName
	tree := treeHash(changedUpgradeTree(plan), "")
	if _, err := readUpgradeTree(api, base, tree); err != nil {
		if err.Error() != "NOT_FOUND" {
			return "", err
		}
		if err := revalidateUpgrade(api, plan, version); err != nil {
			return "", err
		}
		entries := []map[string]any{}
		for _, change := range plan.Changes {
			entry := map[string]any{"path": change.Path, "mode": "100644", "type": "blob"}
			if change.Kind == "deleted" {
				entry["sha"] = nil
			} else {
				entry["content"] = change.Content
			}
			entries = append(entries, entry)
		}
		// The expected tree ID is known before the request, including preserved files.
		_ = api.Call("POST", base+"/git/trees", map[string]any{
			"base_tree": plan.BaseTree, "tree": entries}, nil)
		if _, err := readUpgradeTree(api, base, tree); err != nil {
			return "", errors.New("UPGRADE_TREE_UNKNOWN_READ_BACK_BEFORE_RETRY")
		}
	}
	sha := upgradeCommitID(plan, tree)
	if err := verifyUpgradeCommit(api, base, sha, tree, plan); err != nil {
		if err.Error() != "NOT_FOUND" {
			return "", err
		}
		if err := revalidateUpgrade(api, plan, version); err != nil {
			return "", err
		}
		identity := map[string]string{"name": "Nibfin",
			"email": "nibfin@users.noreply.github.com", "date": upgradeDate}
		_ = api.Call("POST", base+"/git/commits", map[string]any{
			"message": upgradeMessage(plan), "tree": tree, "parents": []string{plan.Base},
			"author": identity, "committer": identity}, nil)
		if err := verifyUpgradeCommit(api, base, sha, tree, plan); err != nil {
			return "", errors.New("UPGRADE_COMMIT_UNKNOWN_READ_BACK_BEFORE_RETRY")
		}
	}
	return sha, nil
}

func upgradeRef(api API, base, branch, sha string) error {
	var ref reference
	if err := api.Call("GET", base+"/git/ref/heads/"+url.PathEscape(branch),
		nil, &ref); err != nil {
		return err
	}
	if ref.Ref != "refs/heads/"+branch || ref.Object.SHA != sha {
		return errors.New("UPGRADE_BRANCH_CONFLICT_INSPECT_BEFORE_RETRY")
	}
	return nil
}

func applyUpgrade(api API, plan upgradePlan, version string) (InitResult, error) {
	result := InitResult{Status: "pending", Code: "UPGRADE_RECONCILIATION_REQUIRED"}
	if err := revalidateUpgrade(api, plan, version); err != nil {
		return result, err
	}
	if len(plan.Changes) == 0 {
		return InitResult{Status: "no_change", Code: "MANAGED_SOURCES_ALREADY_CURRENT"}, nil
	}
	base := "repos/" + plan.Repository.FullName
	branch := upgradeBranchName
	tree := treeHash(changedUpgradeTree(plan), "")
	expected := upgradeCommitID(plan, tree)
	// Inspect the durable branch before creating even unreachable objects.
	err := upgradeRef(api, base, branch, expected)
	existing := err == nil
	if err != nil && err.Error() != "NOT_FOUND" {
		return result, err
	}
	pulls, err := readUpgradePulls(api, plan, branch)
	if err != nil {
		return result, err
	}
	if !existing && len(pulls) != 0 {
		return result, errors.New("UPGRADE_PR_WITHOUT_BRANCH_INSPECT_BEFORE_RETRY")
	}
	sha := expected
	if existing {
		err = verifyUpgradeCommit(api, base, sha, tree, plan)
	} else {
		sha, err = ensureUpgradeObjects(api, plan, version)
	}
	if err != nil {
		return result, err
	}
	if !existing {
		if err := revalidateUpgrade(api, plan, version); err != nil {
			return result, err
		}
		// Never update or force-push an existing reference.
		_ = api.Call("POST", base+"/git/refs", map[string]string{
			"ref": "refs/heads/" + branch, "sha": sha}, nil)
		if err := upgradeRef(api, base, branch, sha); err != nil {
			return result, errors.New("UPGRADE_REF_UNKNOWN_READ_BACK_BEFORE_RETRY")
		}
	}
	if err := revalidateUpgrade(api, plan, version); err != nil {
		return result, err
	}
	if err := upgradeRef(api, base, branch, sha); err != nil {
		return result, err
	}
	pull, err := upgradePR(api, plan, version, branch, sha, existing)
	if err != nil {
		return result, err
	}
	result.PR = pull
	result.Code = "UPGRADE_PR_REQUIRES_REVIEW_CI_MERGE_AND_SEPARATE_HOST_ADOPTION"
	return result, nil
}

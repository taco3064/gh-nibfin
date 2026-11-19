package cli

import (
	"errors"
	"net/url"
	"reflect"
	"regexp"
)

var upgradeMessagePattern = regexp.MustCompile(
	`^Propose verified Nibfin source upgrade\n\nPlan: ([0-9a-f]{64})\n` +
		`(?:Sources: ([^\n]+)\n)?$`)

func sameUpgradeIntent(a, b upgradePlan) bool {
	return a.Repository.ID == b.Repository.ID &&
		a.Repository.FullName == b.Repository.FullName &&
		a.Repository.Owner.ID == b.Repository.Owner.ID &&
		a.Repository.DefaultBranch == b.Repository.DefaultBranch &&
		a.MemberID == b.MemberID && a.ManagedIssue == b.ManagedIssue &&
		reflect.DeepEqual(a.Current, b.Current) &&
		reflect.DeepEqual(a.Target, b.Target) && reflect.DeepEqual(a.Changes, b.Changes)
}

func refreshUpgrade(api API, plan upgradePlan, version string) (upgradePlan, error) {
	current, err := planUpgrade(api, plan.Repository.FullName, version, plan.ManagedIssue)
	if err != nil {
		return current, err
	}
	if !sameUpgradeIntent(plan, current) {
		return current, errors.New("UPGRADE_INTENT_CHANGED_PREVIEW_AGAIN")
	}
	return current, nil
}

func readUpgradeRef(api API, plan upgradePlan) (string, error) {
	var ref reference
	path := "repos/" + plan.Repository.FullName + "/git/ref/heads/" +
		url.PathEscape(upgradeBranchName)
	if err := api.Call("GET", path, nil, &ref); err != nil {
		return "", err
	}
	if ref.Ref != "refs/heads/"+upgradeBranchName ||
		!commitPattern.MatchString(ref.Object.SHA) {
		return "", errors.New("UPGRADE_BRANCH_CONFLICT_INSPECT_BEFORE_RETRY")
	}
	return ref.Object.SHA, nil
}

// Compare uses GitHub's complete ancestry counters, not the paginated commit list.
func upgradeRelationship(api API, plan upgradePlan, head string) (bool, error) {
	var comparison struct {
		Status string `json:"status"`
		Ahead  *int   `json:"ahead_by"`
		Behind *int   `json:"behind_by"`
		Base   struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	path := "repos/" + plan.Repository.FullName + "/compare/" + plan.Base + "..." + head
	if err := api.Call("GET", path, nil, &comparison); err != nil {
		return false, err
	}
	if comparison.Base.SHA != plan.Base || comparison.Ahead == nil ||
		comparison.Behind == nil || *comparison.Ahead < 0 || *comparison.Behind < 0 ||
		!commitPattern.MatchString(comparison.MergeBase.SHA) {
		return false, errors.New("UPGRADE_ANCESTRY_UNAVAILABLE")
	}
	// The proposal parent must be an ancestor of current main, never unrelated work.
	if comparison.Status == "identical" && *comparison.Ahead == 0 &&
		*comparison.Behind == 0 && comparison.MergeBase.SHA == head && head == plan.Base {
		return false, nil
	}
	if comparison.Status == "behind" && *comparison.Ahead == 0 &&
		*comparison.Behind > 0 && comparison.MergeBase.SHA == head {
		return true, nil
	}
	return false, errors.New("UPGRADE_PROPOSAL_PARENT_CONFLICT")
}

type upgradeProposal struct {
	Head   string
	PlanID string
	Behind bool
}

func observeUpgradeProposal(api API, plan upgradePlan, version,
	head string) (upgradeProposal, error) {
	proposal := upgradeProposal{Head: head}
	base := "repos/" + plan.Repository.FullName
	var commit upgradeCommit
	if err := api.Call("GET", base+"/git/commits/"+head, nil, &commit); err != nil {
		return proposal, err
	}
	metadata := upgradeMessagePattern.FindStringSubmatch(commit.Message)
	if commit.SHA != head || len(commit.Parents) != 1 || len(metadata) != 3 ||
		!commitPattern.MatchString(commit.Parents[0].SHA) {
		return proposal, errors.New("UPGRADE_BRANCH_CONFLICT_INSPECT_BEFORE_RETRY")
	}
	proposal.PlanID = metadata[1]
	if metadata[2] != "" && metadata[2] != upgradeSourceMetadata(plan.Target) {
		return proposal, errors.New("UPGRADE_BRANCH_CONFLICT_SOURCE_INTENT_CHANGED")
	}
	parent := commit.Parents[0].SHA
	historical, err := planUpgradeAt(api, plan.Repository.FullName, version,
		plan.ManagedIssue, parent)
	if err != nil {
		return proposal, err
	}
	// Legacy commits only bind sources through their original plan. Do not infer
	// an original target from today's release when that binding cannot be verified.
	if metadata[2] == "" {
		if err := verifyLegacyUpgradeBinding(api, historical, version,
			proposal.PlanID); err != nil {
			return proposal, err
		}
	}
	if !sameUpgradeIntent(plan, historical) {
		return proposal, errors.New("UPGRADE_BRANCH_CONFLICT_INTENT_CHANGED")
	}
	entries, err := readUpgradeTree(api, base, commit.Tree.SHA)
	if err != nil {
		return proposal, err
	}
	if treeHash(changedUpgradeTree(historical), "") != treeHash(entries, "") {
		return proposal, errors.New("UPGRADE_BRANCH_CONFLICT_MANAGED_CHANGE_SET")
	}
	if parent == plan.Base {
		// No compare request is necessary for a directly observed current parent.
		proposal.Behind = false
	} else {
		proposal.Behind, err = upgradeRelationship(api, plan, parent)
	}
	return proposal, err
}

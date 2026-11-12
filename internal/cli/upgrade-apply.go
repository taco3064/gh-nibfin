package cli

import (
	"errors"
	"net/url"
)

type upgradeResult struct {
	Status   string `json:"status"`
	Code     string `json:"code"`
	Branch   string `json:"branch,omitempty"`
	PR       string `json:"pull_request,omitempty"`
	Cache    string `json:"verified_candidate_cache,omitempty"`
	Adoption string `json:"host_adoption"`
	Inspect  string `json:"reconcile"`
}

func revalidateUpgrade(api API, plan upgradePlan) error {
	current, err := planUpgrade(api, plan.Repository.FullName, plan.Selection)
	if err != nil {
		return err
	}
	if !sameUpgradePlan(current, plan) {
		return errors.New("UPGRADE_PLAN_CHANGED_PREVIEW_AGAIN")
	}
	return nil
}

func (a App) applyUpgrade(plan upgradePlan) (upgradeResult, error) {
	result := upgradeResult{Status: "pending", Code: "UPGRADE_INCOMPLETE",
		Adoption: plan.Adoption, Branch: upgradeBranchName(plan),
		Inspect: "Read branch and PR identities; rerun the same upgrade selection " +
			"to reconcile. Never delete, overwrite or roll back an unknown branch."}
	if err := revalidateUpgrade(a.API, plan); err != nil {
		return result, err
	}
	if len(plan.Changes) == 0 {
		result.Status, result.Code = "completed", "REPOSITORY_TARGET_READ_BACK"
		return result, nil
	}
	cache, err := installPinnedCoreCache(a.API, a.Process, plan.Target.Pins)
	if err != nil {
		return result, err
	}
	result.Cache = cache
	if err := revalidateUpgrade(a.API, plan); err != nil {
		return result, err
	}
	sha, err := prepareUpgradeObjects(a.API, plan)
	if err != nil {
		return result, err
	}
	if err := revalidateUpgrade(a.API, plan); err != nil {
		return result, err
	}
	base := "repos/" + plan.Repository.FullName
	refPath := base + "/git/ref/heads/" + url.PathEscape(result.Branch)
	var branch reference
	err = a.API.Call("GET", refPath, nil, &branch)
	if err != nil && err.Error() != "NOT_FOUND" {
		return result, err
	}
	if err != nil {
		_ = a.API.Call("POST", base+"/git/refs", map[string]string{
			"ref": "refs/heads/" + result.Branch, "sha": sha}, nil)
	}
	if err := verifyUpgradeBranch(a.API, plan, result.Branch, sha); err != nil {
		return result, err
	}
	if err := revalidateUpgrade(a.API, plan); err != nil {
		return result, err
	}
	result.PR, err = reconcileUpgradePR(a.API, plan, result.Branch, sha)
	if err != nil {
		return result, err
	}
	if err := verifyUpgradeBranch(a.API, plan, result.Branch, sha); err != nil {
		return result, err
	}
	if err := revalidateUpgrade(a.API, plan); err != nil {
		return result, err
	}
	result.Status, result.Code = "awaiting_review_merge", "UPGRADE_PR_VERIFIED"
	return result, nil
}

func verifyUpgradeBranch(api API, plan upgradePlan, branch, sha string) error {
	base := "repos/" + plan.Repository.FullName
	var observed reference
	if err := api.Call("GET", base+"/git/ref/heads/"+url.PathEscape(branch),
		nil, &observed); err != nil || observed.Object.SHA != sha ||
		observed.Ref != "refs/heads/"+branch {
		return errors.New("UPGRADE_BRANCH_PENDING_INSPECT_BEFORE_RETRY")
	}
	for _, change := range plan.Changes {
		body, _, err := content(api, plan.Repository.FullName, change.Path, sha)
		if change.After == nil {
			if err == nil || err.Error() != "NOT_FOUND" {
				return errors.New("UPGRADE_DELETION_READBACK_MISMATCH")
			}
		} else if err != nil || string(body) != *change.After {
			return errors.New("UPGRADE_CONTENT_READBACK_MISMATCH")
		}
	}
	return nil
}

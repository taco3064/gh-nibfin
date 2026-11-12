package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type upgradePRRef struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	} `json:"repo"`
}

type upgradePR struct {
	Number int          `json:"number"`
	URL    string       `json:"html_url"`
	State  string       `json:"state"`
	Head   upgradePRRef `json:"head"`
	Base   upgradePRRef `json:"base"`
}

func reconcileUpgradePR(api API, plan upgradePlan, branch, sha string) (string, error) {
	base := "repos/" + plan.Repository.FullName
	owner := strings.Split(plan.Repository.FullName, "/")[0]
	query := base + "/pulls?state=all&per_page=100&head=" +
		url.QueryEscape(owner+":"+branch)
	var pulls []upgradePR
	if err := api.Call("GET", query, nil, &pulls); err != nil {
		return "", errors.New("UPGRADE_PR_LOOKUP_PENDING_INSPECT_BEFORE_RETRY")
	}
	if len(pulls) == 0 {
		// Check the branch immediately before attempting the only PR mutation.
		if err := verifyUpgradeBranch(api, plan, branch, sha); err != nil {
			return "", err
		}
		if err := revalidateUpgrade(api, plan); err != nil {
			return "", err
		}
		_ = api.Call("POST", base+"/pulls", map[string]string{
			"title": "Upgrade verified Nibfin sources", "head": branch,
			"base": plan.Repository.DefaultBranch,
			"body": "Review the exact managed changes and current CI before merging. " +
				"Default-branch adoption is not complete. Preserve active work's " +
				"original approved context. Host adoption is separately authorized."}, nil)
		if err := api.Call("GET", query, nil, &pulls); err != nil {
			return "", errors.New("UPGRADE_PR_RESULT_PENDING_INSPECT_BEFORE_RETRY")
		}
	}
	if len(pulls) != 1 || pulls[0].Number <= 0 {
		return "", errors.New("UPGRADE_PR_IDENTITY_PENDING_INSPECT_BEFORE_RETRY")
	}
	var observed upgradePR
	endpoint := fmt.Sprintf("%s/pulls/%d", base, pulls[0].Number)
	if err := api.Call("GET", endpoint, nil, &observed); err != nil {
		return "", errors.New("UPGRADE_PR_READBACK_UNAVAILABLE")
	}
	wantURL := fmt.Sprintf("https://github.com/%s/pull/%d",
		plan.Repository.FullName, pulls[0].Number)
	if observed.Number != pulls[0].Number || observed.URL != wantURL ||
		observed.State != "open" || observed.Head.Ref != branch ||
		observed.Head.SHA != sha || observed.Base.Ref != plan.Repository.DefaultBranch ||
		observed.Base.SHA != plan.Head {
		return "", errors.New("UPGRADE_PR_READBACK_MISMATCH")
	}
	for _, ref := range []upgradePRRef{observed.Head, observed.Base} {
		if ref.Repo.ID != plan.Repository.ID || ref.Repo.FullName != plan.Repository.FullName {
			return "", errors.New("UPGRADE_PR_REPOSITORY_MISMATCH")
		}
	}
	return observed.URL, nil
}

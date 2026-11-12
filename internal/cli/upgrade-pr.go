package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type upgradePull struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	URL    string `json:"html_url"`
	Body   string `json:"body"`
	Head   struct {
		Ref        string     `json:"ref"`
		SHA        string     `json:"sha"`
		Repository Repository `json:"repo"`
	} `json:"head"`
	Base struct {
		SHA        string     `json:"sha"`
		Ref        string     `json:"ref"`
		Repository Repository `json:"repo"`
	} `json:"base"`
}

func upgradePRBody(plan upgradePlan) string {
	return "Verified upgrade proposal. Plan: " + plan.ID + "\n\n" + hostUpgradeObligation
}

func readUpgradePulls(api API, plan upgradePlan, branch string) ([]upgradePull, error) {
	owner := strings.Split(plan.Repository.FullName, "/")[0]
	query := "?state=all&per_page=100&head=" + url.QueryEscape(owner+":"+branch)
	var pulls []upgradePull
	if err := api.Call("GET", "repos/"+plan.Repository.FullName+"/pulls"+query,
		nil, &pulls); err != nil {
		return nil, err
	}
	if len(pulls) > 1 {
		return nil, errors.New("UPGRADE_PRS_AMBIGUOUS_INSPECT_BEFORE_RETRY")
	}
	return pulls, nil
}

func upgradePR(api API, plan upgradePlan, version, branch, sha string,
	existingBranch bool) (string, error) {
	pulls, err := readUpgradePulls(api, plan, branch)
	if err != nil {
		return "", err
	}
	if len(pulls) == 0 {
		// An existing branch with no visible PR may be a previously lost PR response.
		// It is never permission to replay that mutation on a repeated invocation.
		if existingBranch {
			return "", errors.New("UPGRADE_PR_UNKNOWN_INSPECT_EXISTING_BRANCH_BEFORE_RETRY")
		}
		if err := revalidateUpgrade(api, plan, version); err != nil {
			return "", err
		}
		if err := upgradeRef(api, "repos/"+plan.Repository.FullName,
			branch, sha); err != nil {
			return "", err
		}
		_ = api.Call("POST", "repos/"+plan.Repository.FullName+"/pulls", map[string]any{
			"title": "Upgrade verified Nibfin managed sources", "head": branch,
			"base": plan.Repository.DefaultBranch, "body": upgradePRBody(plan)}, nil)
		pulls, err = readUpgradePulls(api, plan, branch)
		if err != nil || len(pulls) != 1 {
			return "", errors.New("UPGRADE_PR_UNKNOWN_READ_BACK_BEFORE_RETRY")
		}
	}
	pull := pulls[0]
	if pull.Number <= 0 {
		return "", errors.New("UPGRADE_PR_READBACK_MISMATCH")
	}
	var observed upgradePull
	endpoint := fmt.Sprintf("repos/%s/pulls/%d", plan.Repository.FullName, pull.Number)
	if err := api.Call("GET", endpoint, nil, &observed); err != nil {
		return "", err
	}
	wantURL := fmt.Sprintf("https://github.com/%s/pull/%d",
		plan.Repository.FullName, pull.Number)
	if observed.Number != pull.Number || observed.State != "open" ||
		observed.URL != wantURL || observed.Body != upgradePRBody(plan) ||
		observed.Head.SHA != sha || observed.Head.Ref != branch ||
		observed.Base.Ref != plan.Repository.DefaultBranch || observed.Base.SHA != plan.Base ||
		observed.Head.Repository.ID != plan.Repository.ID ||
		observed.Base.Repository.ID != plan.Repository.ID ||
		observed.Head.Repository.FullName != plan.Repository.FullName ||
		observed.Base.Repository.FullName != plan.Repository.FullName {
		return "", errors.New("UPGRADE_PR_READBACK_MISMATCH")
	}
	if err := revalidateUpgrade(api, plan, version); err != nil {
		return "", err
	}
	if err := upgradeRef(api, "repos/"+plan.Repository.FullName,
		branch, sha); err != nil {
		return "", err
	}
	return observed.URL, nil
}

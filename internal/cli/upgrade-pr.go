package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type upgradePull struct {
	NodeID string `json:"node_id"`
	Number int    `json:"number"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
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

func upgradeProposalBody(planID string) string {
	return "Verified upgrade proposal. Plan: " + planID + "\n\n" + hostUpgradeObligation
}

func readUpgradePulls(api API, plan upgradePlan, branch string) ([]upgradePull, error) {
	owner := strings.Split(plan.Repository.FullName, "/")[0]
	state := "all"
	if plan.Target.handoff != nil {
		state = "open"
	}
	query := "?state=" + state + "&per_page=100&head=" + url.QueryEscape(owner+":"+branch)
	var raw json.RawMessage
	if err := api.Call("GET", "repos/"+plan.Repository.FullName+"/pulls"+query,
		nil, &raw); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(raw))
	var pulls []upgradePull
	if !strings.HasPrefix(text, "[") || json.Unmarshal(raw, &pulls) != nil ||
		len(pulls) > 100 {
		return nil, errors.New("UPGRADE_PR_LIST_INCOMPLETE")
	}
	for _, item := range pulls {
		if item.Number <= 0 {
			return nil, errors.New("UPGRADE_PR_LIST_INCOMPLETE")
		}
	}
	// A full page is already ambiguous, never successful absence. No later page
	// could make multiple reserved-branch proposals safe to mutate.
	if len(pulls) > 1 {
		return nil, errors.New("UPGRADE_PRS_AMBIGUOUS_INSPECT_BEFORE_RETRY")
	}
	return pulls, nil
}

func upgradePR(api API, plan upgradePlan, version, branch, sha string) (string, error) {
	proposal, err := observeUpgradeProposal(api, plan, version, sha)
	if err != nil {
		return "", err
	}
	pulls, err := readUpgradePulls(api, plan, branch)
	if err != nil {
		return "", err
	}
	if len(pulls) == 0 {
		if err := revalidateUpgrade(api, plan, version); err != nil {
			return "", err
		}
		if err := upgradeRef(api, "repos/"+plan.Repository.FullName, branch, sha); err != nil {
			return "", err
		}
		body := upgradeProposalBody(proposal.PlanID)
		request := map[string]any{
			"title": "Upgrade verified Nibfin managed sources", "head": branch,
			"base": plan.Repository.DefaultBranch, "body": body}
		if plan.Target.handoff != nil {
			// Core must bind this specific PR before it can be merged.
			request["draft"] = true
		}
		_ = api.Call("POST", "repos/"+plan.Repository.FullName+"/pulls", request, nil)
		pulls, err = readUpgradePulls(api, plan, branch)
		if err != nil {
			return "", err
		}
		if len(pulls) != 1 {
			return "", errors.New("UPGRADE_PR_UNKNOWN_READ_BACK_BEFORE_RETRY")
		}
	}
	observed, err := readUpgradePull(api, plan, pulls[0].Number, proposal)
	if err != nil {
		return "", err
	}
	if proposal.Behind {
		return rebaseUpgrade(api, plan, version, observed, proposal)
	}
	return finishUpgradeProposal(api, plan, version, observed, proposal)
}

func finishUpgradeProposal(api API, plan upgradePlan, version string,
	pull upgradePull, proposal upgradeProposal) (string, error) {
	if err := revalidateUpgrade(api, plan, version); err != nil {
		return "", err
	}
	if err := upgradeRef(api, "repos/"+plan.Repository.FullName,
		upgradeBranchName, proposal.Head); err != nil {
		return "", err
	}
	return pull.URL, nil
}

func readUpgradePull(api API, plan upgradePlan, number int,
	proposal upgradeProposal) (upgradePull, error) {
	var observed upgradePull
	endpoint := fmt.Sprintf("repos/%s/pulls/%d", plan.Repository.FullName, number)
	if err := api.Call("GET", endpoint, nil, &observed); err != nil {
		return observed, err
	}
	wantURL := fmt.Sprintf("https://github.com/%s/pull/%d",
		plan.Repository.FullName, number)
	if observed.Number != number || observed.State != "open" ||
		observed.URL != wantURL || observed.Body != upgradeProposalBody(proposal.PlanID) ||
		observed.Head.SHA != proposal.Head || observed.Head.Ref != upgradeBranchName ||
		observed.Base.Ref != plan.Repository.DefaultBranch || observed.Base.SHA != plan.Base ||
		observed.Head.Repository.ID != plan.Repository.ID ||
		observed.Base.Repository.ID != plan.Repository.ID ||
		observed.Head.Repository.FullName != plan.Repository.FullName ||
		observed.Base.Repository.FullName != plan.Repository.FullName {
		return observed, errors.New("UPGRADE_PR_READBACK_MISMATCH")
	}
	if err := upgradeRef(api, "repos/"+plan.Repository.FullName,
		upgradeBranchName, proposal.Head); err != nil {
		return observed, err
	}
	return observed, nil
}

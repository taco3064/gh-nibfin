package cli

import (
	"errors"
	"fmt"
	"strings"
)

const upgradeRebaseMutation = `mutation($input: UpdatePullRequestBranchInput!) {
 updatePullRequestBranch(input: $input) { clientMutationId }
}`

func rebaseUpgrade(api API, plan upgradePlan, version string,
	pull upgradePull, proposal upgradeProposal) (string, error) {
	current, err := refreshUpgrade(api, plan, version)
	if err != nil {
		return "", err
	}
	// Reobserve the exact proposal and PR immediately before the guarded mutation.
	observed, err := readUpgradePull(api, current, pull.Number, proposal)
	if err != nil {
		return "", err
	}
	checked, err := observeUpgradeProposal(api, current, version, proposal.Head)
	if err != nil {
		return "", err
	}
	if !checked.Behind {
		return finishUpgradeProposal(api, current, version, observed, checked)
	}
	if observed.NodeID == "" {
		return "", errors.New("UPGRADE_PR_NATIVE_ID_UNAVAILABLE")
	}
	var response struct {
		Data *struct {
			Updated *struct{} `json:"updatePullRequestBranch"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	operationErr := api.Call("POST", "graphql", map[string]any{
		"query": upgradeRebaseMutation,
		"variables": map[string]any{"input": map[string]any{
			"pullRequestId": observed.NodeID, "expectedHeadOid": proposal.Head,
			"updateMethod": "REBASE"}}}, &response)
	if len(response.Errors) != 0 {
		messages := []string{}
		for _, item := range response.Errors {
			messages = append(messages, item.Message)
		}
		operationErr = fmt.Errorf("UPGRADE_NATIVE_REBASE_FAILED: %s",
			strings.Join(messages, "; "))
	} else if operationErr == nil &&
		(response.Data == nil || response.Data.Updated == nil) {
		operationErr = errors.New("UPGRADE_NATIVE_REBASE_RESULT_UNAVAILABLE")
	}
	// Even an error or missing response may follow a durable provider effect.
	current, err = refreshUpgrade(api, current, version)
	if err != nil {
		return "", err
	}
	head, err := readUpgradeRef(api, current)
	if err != nil {
		return "", err
	}
	after, err := observeUpgradeProposal(api, current, version, head)
	if err != nil {
		return "", err
	}
	if after.PlanID != proposal.PlanID {
		return "", errors.New("UPGRADE_PROPOSAL_CHANGED_DURING_REBASE")
	}
	pulls, err := readUpgradePulls(api, current, upgradeBranchName)
	if err != nil {
		return "", err
	}
	if len(pulls) != 1 || pulls[0].Number != pull.Number {
		return "", errors.New("UPGRADE_PR_CHANGED_DURING_REBASE")
	}
	verified, err := readUpgradePull(api, current, pull.Number, after)
	if err != nil {
		return "", err
	}
	if verified.NodeID != observed.NodeID {
		return "", errors.New("UPGRADE_PR_CHANGED_DURING_REBASE")
	}
	if after.Behind {
		if operationErr != nil {
			return "", operationErr
		}
		return "", errors.New("UPGRADE_NATIVE_REBASE_PENDING_REPEAT_EXPLICIT_APPLY")
	}
	return finishUpgradeProposal(api, current, version, verified, after)
}

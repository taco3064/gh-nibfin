package cli

import (
	"errors"
	"strconv"
	"strings"
)

type upgradeRoutePublisher interface {
	publishRouteSecret(repository, name string) error
}

type upgradeRouteSetup struct {
	SecretState string               `json:"secret_state"`
	Secret      nativeSecretMetadata `json:"secret"`
}

func prepareUpgradeRoute(api API, plan InitPlan, upgrade upgradePlan, version string,
	recipe upgradeHandoffRecipe, state *upgradeRouteSetup, save func() error) error {
	if err := nativeWakeAuthority(api, plan); err != nil {
		return err
	}
	base := "repos/" + plan.Repository.FullName + "/actions/"
	variable := actionsVariable{Name: recipe.variable, Value: recipe.execution.Repository}
	var current actionsVariable
	err := api.Call("GET", base+"variables/"+recipe.variable, nil, &current)
	missing := err != nil && err.Error() == "NOT_FOUND"
	if !missing && err != nil {
		return err
	}
	if !missing && current != variable {
		return errors.New("UPGRADE_NOTIFICATION_DESTINATION_CONFLICT")
	}
	secret, exists, err := observeUpgradeSecret(api, base, recipe.secret)
	if err != nil {
		return err
	}
	if missing && exists {
		return errors.New("UPGRADE_NOTIFICATION_SECRET_DESTINATION_UNVERIFIED")
	}
	if exists {
		switch state.SecretState {
		case "confirmed":
			if secret != state.Secret {
				return errors.New("UPGRADE_NOTIFICATION_SECRET_CHANGED")
			}
			return nil
		case "published":
			state.SecretState, state.Secret = "confirmed", secret
			return save()
		case "pending":
			// An unresolved write can re-enter its authorized upsert. Metadata
			// alone cannot attribute the old effect or show token equality.
		default:
			if err := verifyUpgradeRouteOwnership(api, upgrade, version, recipe); err != nil {
				return err
			}
		}
	}
	if missing {
		_ = api.Call("POST", base+"variables", variable, nil)
		err := api.Call("GET", base+"variables/"+recipe.variable, nil, &current)
		if err != nil {
			return err
		}
		if current != variable {
			return errors.New("UPGRADE_NOTIFICATION_DESTINATION_REQUIRES_READBACK")
		}
	}
	publisher, ok := api.(upgradeRoutePublisher)
	if !ok {
		return errors.New("UPGRADE_NOTIFICATION_SECRET_PUBLICATION_UNAVAILABLE")
	}
	if err := nativeWakeAuthority(api, plan); err != nil {
		return err
	}
	state.SecretState = "pending"
	if err := save(); err != nil {
		return err
	}
	writeErr := publisher.publishRouteSecret(plan.Repository.FullName, recipe.secret)
	if writeErr != nil {
		return writeErr
	}
	state.SecretState = "published"
	if err := save(); err != nil {
		return err
	}
	currentSecret, confirmed, err := observeUpgradeSecret(api, base, recipe.secret)
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("UPGRADE_NOTIFICATION_SECRET_REQUIRES_READBACK")
	}
	state.SecretState, state.Secret = "confirmed", currentSecret
	return save()
}

func verifyUpgradeRouteOwnership(api API, plan upgradePlan, version string,
	recipe upgradeHandoffRecipe) error {
	if _, present := plan.entries[recipe.noticePath]; present {
		source, err := installedUpgradeFile(api, plan, recipe.noticePath)
		if err != nil {
			return err
		}
		if strings.ReplaceAll(source, "\r\n", "\n") == recipe.notice {
			return nil
		}
	}
	head, err := readUpgradeRef(api, plan)
	if err != nil {
		if err.Error() == "NOT_FOUND" {
			return errors.New("UPGRADE_NOTIFICATION_SECRET_OWNERSHIP_UNVERIFIED")
		}
		return err
	}
	proposal, err := observeUpgradeProposal(api, plan, version, head)
	if err != nil {
		return err
	}
	pulls, err := readUpgradePulls(api, plan, upgradeBranchName)
	if err != nil {
		return err
	}
	if len(pulls) != 1 {
		return errors.New("UPGRADE_NOTIFICATION_SECRET_OWNERSHIP_UNVERIFIED")
	}
	_, err = readUpgradePull(api, plan, pulls[0].Number, proposal)
	return err
}

func observeUpgradeSecret(api API, base, name string) (
	nativeSecretMetadata, bool, error) {
	var metadata nativeSecretMetadata
	err := api.Call("GET", base+"secrets/"+name, nil, &metadata)
	if err != nil {
		if err.Error() == "NOT_FOUND" {
			return metadata, false, nil
		}
		return metadata, false, err
	}
	if metadata.Name != name || metadata.CreatedAt == "" || metadata.UpdatedAt == "" {
		return metadata, false, errors.New("UPGRADE_NOTIFICATION_SECRET_UNVERIFIED")
	}
	return metadata, true, nil
}

func upgradePullNumber(repo, url string) (int, error) {
	prefix := "https://github.com/" + repo + "/pull/"
	if !strings.HasPrefix(url, prefix) {
		return 0, errors.New("UPGRADE_PR_URL_UNVERIFIED")
	}
	number, err := strconv.Atoi(strings.TrimPrefix(url, prefix))
	if err != nil || number <= 0 || url != prefix+strconv.Itoa(number) {
		return 0, errors.New("UPGRADE_PR_URL_UNVERIFIED")
	}
	return number, nil
}

const readyUpgradeMutation = `mutation($id:ID!) {
 markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{id}}
}`

func readyBoundUpgradeProposal(api API, plan upgradePlan, version string,
	bound managedUpgradeProposal) error {
	current, err := refreshUpgrade(api, plan, version)
	if err != nil {
		return err
	}
	head, err := readUpgradeRef(api, current)
	if err != nil {
		return err
	}
	proposal, err := observeUpgradeProposal(api, current, version, head)
	if err != nil {
		return err
	}
	pull, err := readUpgradePull(api, current, bound.Pull, proposal)
	if err != nil {
		return err
	}
	if pull.NodeID != bound.Node || proposal.Behind || pull.URL != bound.URL {
		return errors.New("UPGRADE_PR_CHANGED_BEFORE_READY")
	}
	if !pull.Draft {
		return nil
	}
	writeErr := api.Call("POST", "graphql", map[string]any{
		"query": readyUpgradeMutation, "variables": map[string]string{"id": bound.Node},
	}, nil)
	pull, err = readUpgradePull(api, current, bound.Pull, proposal)
	if err != nil {
		return err
	}
	if pull.NodeID != bound.Node {
		return errors.New("UPGRADE_PR_CHANGED_AFTER_READY")
	}
	if pull.Draft {
		if writeErr != nil {
			return writeErr
		}
		return errors.New("UPGRADE_PR_READY_REQUIRES_READBACK")
	}
	return nil
}

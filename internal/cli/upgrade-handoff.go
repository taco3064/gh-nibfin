package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type upgradeHandoffPorts struct {
	bridge  func(any) ([]byte, error)
	publish func(upgradePlan) (InitResult, error)
	ready   func(upgradePlan, managedUpgradeProposal) error
	recover upgradeHandoffRecovery
}

type upgradeHandoffRecovery func(upgradePlan, managedUpgradeProposal, bool) (
	upgradeRecoveryObservation, error)

type upgradeHandoffInput struct {
	Kind         string              `json:"kind"`
	Repositories []int64             `json:"repositories"`
	Source       upgradeHandoffPin   `json:"source"`
	Activation   *nativeSourceTarget `json:"activation"`
}

type upgradeHandoffPin struct {
	Repository   string `json:"repository"`
	Commit       string `json:"commit"`
	ManifestHash string `json:"manifestHash"`
}

type upgradeHandoffOutcome struct {
	Status         string                       `json:"status"`
	Message        string                       `json:"message,omitempty"`
	Migration      []nativeMigrationResult      `json:"migration"`
	Continuation   *nativeContinuation          `json:"continuation"`
	Upgrade        *managedUpgradeProgress      `json:"upgrade,omitempty"`
	ExecutionFirst string                       `json:"execution_merge_first,omitempty"`
	CoreRunURL     string                       `json:"core_run_url,omitempty"`
	Code           string                       `json:"code,omitempty"`
	Cause          *coreOperationDiagnostic     `json:"cause,omitempty"`
	Recovery       []upgradeRecoveryObservation `json:"recovery,omitempty"`
}

func upgradeHandoffPlans(api API, host sharedHost, name, version string,
	target upgradeSource, adopted relayAdoptions) ([]upgradePlan, bool, error) {
	if err := nativeMigrationBindings(adopted, host); err != nil {
		return nil, false, err
	}
	target.issueOnly = &issueOnlyUpgradeBinding{host.SharedExecution, host.MemberID}
	if target.handoff == nil {
		return nil, false, errors.New("CORE_UPGRADE_RECEIVER_REQUIRED")
	}
	sharedChange := !nativeCoreSourcesMatch(adopted.Pin, target.Producers.Core)
	if sharedChange {
		// The serving Core must recognize the event transport before it can
		// authorize a promotion. Other managed files use the selected source.
		known, err := readUpgradeHandoffRecipe(api, adopted.Pin, host.SharedExecution)
		if err != nil {
			return nil, false, err
		}
		if known == nil {
			return nil, false, errors.New("CORE_UPGRADE_RECEIVER_REQUIRED")
		}
		target.handoff = known
	}
	executionReady, err := upgradeExecutionRoute(api, host, *target.handoff)
	if err != nil {
		return nil, false, err
	}
	plans := make([]upgradePlan, 0, len(adopted.Origins))
	executionObserved, selected := false, false
	for _, origin := range adopted.Origins {
		isExecution := origin.RepositoryID == host.RepositoryID
		isSelected := origin.Repository == name
		executionObserved = executionObserved || isExecution
		selected = selected || isSelected
		if !sharedChange && !isSelected && !(isExecution && !executionReady) {
			continue
		}
		plan, err := planNativeUpgradeTarget(api, origin.Repository, version, target)
		if err != nil {
			return nil, false, err
		}
		if plan.Repository.ID != origin.RepositoryID || plan.MemberID != host.MemberID {
			return nil, false, errors.New("NATIVE_MIGRATION_IDENTITY_CHANGED")
		}
		if isExecution {
			plans = append([]upgradePlan{plan}, plans...)
		} else {
			plans = append(plans, plan)
		}
	}
	if !executionObserved && !executionReady {
		return nil, false, errors.New("UPGRADE_EXECUTION_REPOSITORY_NOT_ADOPTED")
	}
	if !selected {
		return nil, false, errors.New("REQUESTED_REPOSITORY_NOT_ADOPTED")
	}
	return plans, executionReady, nil
}

func runUpgradeHandoff(plans []upgradePlan, execReady bool, recipe upgradeHandoffRecipe,
	core SourcePin, activation *nativeSourceTarget, ports upgradeHandoffPorts) (
	upgradeHandoffOutcome, error) {
	result := upgradeHandoffOutcome{Status: "pending", Migration: []nativeMigrationResult{}}
	if !execReady {
		result.ExecutionFirst = recipe.execution.Repository
	}
	input := upgradeHandoffInput{Kind: recipe.prepare, Activation: activation,
		Source: upgradeHandoffPin{core.Repository, core.Commit, core.ManifestHash}}
	for _, plan := range plans {
		input.Repositories = append(input.Repositories, plan.Repository.ID)
	}
	data, err := ports.bridge(input)
	progress, err := decodeManagedUpgrade(data, err, recipe.execution, "")
	if err != nil {
		return result, err
	}
	result.Upgrade = &progress
	var firstErr error
	if len(progress.Proposals) != 0 {
		data, err = ports.bridge(map[string]any{"kind": recipe.observe,
			"operationId": progress.OperationID})
		observed, observeErr := decodeManagedUpgrade(data, err, recipe.execution,
			progress.OperationID)
		if observeErr == nil {
			progress = observed
			result.Upgrade = &progress
		}
		// A recorded proposal may still need its first native route. Retrying its
		// existing publisher and bind can establish the route without a new PR.
	}
	var direct []int64
	for _, plan := range plans {
		if len(plan.Changes) == 0 {
			if merged := mergedProposal(progress, plan.Repository); merged != nil {
				retry := upgradeConsumerNeedsRecovery(progress, plan.Repository)
				observed, err := ports.recover(plan, *merged, retry)
				if observed.RunURL != "" {
					result.Recovery = append(result.Recovery, observed)
				}
				if err != nil && firstErr == nil {
					firstErr = err
				}
			} else {
				// Prepare routing for already-current callers without publishing a PR.
				if _, err := ports.publish(plan); err == nil {
					direct = append(direct, plan.Repository.ID)
				} else if firstErr == nil {
					firstErr = err
				}
			}
			continue
		}
		if err := handoffUpgradeProposal(&result, plan, recipe, ports); err != nil &&
			firstErr == nil {
			firstErr = err
		}
	}
	if len(direct) > 0 {
		request := map[string]any{"kind": recipe.complete,
			"operationId": progress.OperationID}
		if len(progress.Consumers) > len(plans) {
			request["repositories"] = direct
		}
		data, err = ports.bridge(request)
		completed, err := decodeManagedUpgrade(data, err, recipe.execution,
			progress.OperationID)
		if err == nil {
			result.Upgrade = &completed
		} else if firstErr == nil {
			firstErr = err
		}
	}
	return result, firstErr
}

func upgradeConsumerNeedsRecovery(progress managedUpgradeProgress, repo Repository) bool {
	if progress.Continuation == "pending" || progress.Readiness == "pending" {
		return true
	}
	for _, consumer := range progress.Consumers {
		if consumer.RepositoryID == repo.ID && consumer.Repository == repo.FullName {
			return consumer.Installation == "pending"
		}
	}
	return progress.Installation == "pending"
}

func mergedProposal(p managedUpgradeProgress, repo Repository) *managedUpgradeProposal {
	for _, proposal := range p.Proposals {
		if proposal.RepositoryID == repo.ID && proposal.Repository == repo.FullName &&
			proposal.Merged {
			return &proposal
		}
	}
	return nil
}

func handoffUpgradeProposal(result *upgradeHandoffOutcome, plan upgradePlan,
	recipe upgradeHandoffRecipe, ports upgradeHandoffPorts) error {
	proposal, err := ports.publish(plan)
	entry := nativeMigrationResult{Repository: plan.Repository.FullName,
		Status: "pending", Proposal: &proposal}
	result.Migration = append(result.Migration, entry)
	if err != nil {
		return err
	}
	proposal.Code = "UPGRADE_PR_CREATED_OR_REUSED_HANDOFF_PENDING"
	number, err := upgradePullNumber(entry.Repository, proposal.PR)
	if err != nil {
		return err
	}
	id := result.Upgrade.OperationID
	data, err := ports.bridge(map[string]any{"kind": recipe.bind, "operationId": id,
		"repositoryId": plan.Repository.ID, "pullNumber": number})
	progress, err := decodeManagedUpgrade(data, err, recipe.execution, id)
	if err != nil {
		return err
	}
	result.Upgrade = &progress
	for _, bound := range progress.Proposals {
		if bound.RepositoryID == plan.Repository.ID && bound.Repository == entry.Repository &&
			bound.Pull == number && bound.URL == proposal.PR && !bound.Merged &&
			(bound.Handoff == "confirmed" || bound.Handoff == "review-pending") {
			if err := ports.ready(plan, bound); err != nil {
				return err
			}
			proposal.Code = "UPGRADE_PR_CREATED_OR_REUSED_REVIEW_AND_MERGE_PENDING"
			result.Migration[len(result.Migration)-1].Review = "review_pending"
			return nil
		}
	}
	return errors.New("UPGRADE_HANDOFF_BINDING_UNVERIFIED")
}

func reportUpgradeHandoff(out io.Writer, result upgradeHandoffOutcome, err error) error {
	if result.Upgrade != nil {
		result.Continuation = nativeUpgradeContinuation(*result.Upgrade)
	}
	if result.Upgrade != nil && result.Upgrade.CoreRunID > 0 {
		result.CoreRunURL = fmt.Sprintf("https://github.com/%s/actions/runs/%d",
			officialCore, result.Upgrade.CoreRunID)
	}
	if hasUpgradeReview(result.Migration) {
		result.Message = "Upgrade pull requests were created or reused. " +
			"Review and merge proposals marked review_pending."
		if err != nil {
			result.Message += " Proposals without confirmed binding can remain drafts."
		}
		if result.ExecutionFirst != "" {
			result.Message += " Merge the execution repository proposal first."
		}
		if err == nil && result.Upgrade != nil && result.Upgrade.Handoff == "confirmed" {
			result.Message += " Core confirmed handoff and will continue after merge."
		}
	} else if result.Upgrade != nil && result.Upgrade.Installation == "installed" &&
		(result.Upgrade.Readiness == "ready" || result.Upgrade.Readiness == "not-required") &&
		(result.Upgrade.Continuation == "paused" ||
			result.Upgrade.Continuation == "dispatched") && err == nil {
		result.Status = "completed"
	}
	if result.Status == "pending" && err == nil {
		code := "UPGRADE_CONTINUATION_PENDING"
		if result.Upgrade != nil && result.Upgrade.Readiness == "maintenance-required" {
			code = "UPGRADE_VM_MAINTENANCE_REQUIRED"
		} else if result.Upgrade != nil &&
			result.Upgrade.Installation == "maintenance-required" {
			code = "CORE_ONLY_MAINTENANCE_REQUIRED"
		} else if len(result.Migration) > 0 {
			code = "NATIVE_MIGRATION_PR_PENDING"
		}
		err = &CommandError{Code: code, Exit: 2}
	}
	if err != nil {
		result.Code = err.Error()
		result.Cause = coreOperationCause(err)
		var provider *selectedProviderFailure
		if result.Cause == nil && errors.As(err, &provider) {
			result.Cause = &coreOperationDiagnostic{Code: provider.Code,
				GitHubAPI: &provider.Diagnostic}
		}
	}
	if json.NewEncoder(out).Encode(result) != nil {
		return &CommandError{Code: "OUTPUT_UNAVAILABLE_AFTER_UPGRADE", Exit: 2}
	}
	if err == nil {
		return nil
	}
	var command *CommandError
	if errors.As(err, &command) {
		return err
	}
	return &CommandError{Code: err.Error(), Exit: 2}
}

func hasUpgradeReview(results []nativeMigrationResult) bool {
	for _, item := range results {
		if item.Proposal != nil && item.Proposal.PR != "" {
			return true
		}
	}
	return false
}

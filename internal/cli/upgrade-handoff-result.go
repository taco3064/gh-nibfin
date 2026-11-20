package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
)

var upgradeOperationID = regexp.MustCompile(`^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)

type selectedProviderFailure struct {
	Code       string
	Diagnostic nativeGitHubDiagnostic
}

func (failure *selectedProviderFailure) Error() string { return failure.Code }

type managedUpgradeProposal struct {
	Repository   string                  `json:"repository"`
	RepositoryID int64                   `json:"repositoryId"`
	Pull         int                     `json:"pullNumber"`
	ID           int64                   `json:"pullId"`
	Node         string                  `json:"pullNodeId"`
	URL          string                  `json:"proposalUrl"`
	Merged       bool                    `json:"merged"`
	Handoff      string                  `json:"handoff"`
	ExecutionRun *upgradeExecutionRunRef `json:"executionRun,omitempty"`
}

type upgradeExecutionRunRef struct {
	ID  int64  `json:"runId"`
	URL string `json:"url"`
}

type managedUpgradeProgress struct {
	OperationID  string                   `json:"operationId"`
	Execution    string                   `json:"executionRepository"`
	ExecutionID  int64                    `json:"executionRepositoryId"`
	Proposals    []managedUpgradeProposal `json:"proposals"`
	Handoff      string                   `json:"handoff"`
	Installation string                   `json:"installation"`
	Readiness    string                   `json:"readiness"`
	Continuation string                   `json:"continuation"`
	CoreRunID    int64                    `json:"coreRunId,omitempty"`
	Consumers    []struct {
		Repository   string                   `json:"repository"`
		RepositoryID int64                    `json:"repositoryId"`
		Installation string                   `json:"installation"`
		Handoff      string                   `json:"handoff"`
		Cause        *coreOperationDiagnostic `json:"cause,omitempty"`
	} `json:"consumers"`
	ReadinessCause      *coreOperationDiagnostic `json:"readinessCause,omitempty"`
	InstallationCause   *coreOperationDiagnostic `json:"installationCause,omitempty"`
	HandoffCause        *coreOperationDiagnostic `json:"handoffCause,omitempty"`
	ContinuationCause   *coreOperationDiagnostic `json:"continuationCause,omitempty"`
	Cleanup             *coreOperationDiagnostic `json:"cleanup,omitempty"`
	ContinuationResults []json.RawMessage        `json:"continuationResults,omitempty"`
}

func nativeUpgradeContinuation(result managedUpgradeProgress) *nativeContinuation {
	entries := result.ContinuationResults
	if entries == nil {
		entries = []json.RawMessage{}
	}
	value := &nativeContinuation{Status: result.Continuation, Results: entries}
	if cause := result.ContinuationCause; cause != nil {
		value.Code, value.GitHubAPI = cause.Code, cause.GitHubAPI
	}
	return value
}

func handoffState(status string) bool {
	return slices.Contains([]string{"pending", "review-pending", "confirmed"}, status)
}

func validUpgradeProposal(p managedUpgradeProposal, execution string) bool {
	if p.RepositoryID <= 0 || p.Pull <= 0 || p.ID <= 0 || p.Node == "" ||
		!repoPattern.MatchString(p.Repository) || !handoffState(p.Handoff) ||
		p.URL != fmt.Sprintf("https://github.com/%s/pull/%d", p.Repository, p.Pull) {
		return false
	}
	return p.ExecutionRun == nil || p.ExecutionRun.ID > 0 && p.ExecutionRun.URL ==
		fmt.Sprintf("https://github.com/%s/actions/runs/%d", execution, p.ExecutionRun.ID)
}

func decodeManagedUpgrade(data []byte, callErr error, execution SharedExecution,
	operation string) (managedUpgradeProgress, error) {
	var result struct {
		Schema   int `json:"schemaVersion"`
		Status   string
		Progress managedUpgradeProgress `json:"upgrade"`
	}
	if json.Unmarshal(data, &result) != nil || callErr != nil || result.Schema != 1 ||
		result.Status != "complete" {
		if len(data) == 0 && callErr != nil {
			return result.Progress, callErr
		}
		return result.Progress, pendingCoreOperation(data, "UPGRADE_HANDOFF_PENDING")
	}
	p := result.Progress
	if !upgradeOperationID.MatchString(p.OperationID) || operation != "" &&
		p.OperationID != operation || p.Execution != execution.Repository ||
		p.ExecutionID != execution.RepositoryID || !handoffState(p.Handoff) ||
		!slices.Contains([]string{"pending", "installed", "maintenance-required"},
			p.Installation) || !slices.Contains([]string{"pending", "ready", "not-required",
		"maintenance-required"}, p.Readiness) ||
		!slices.Contains([]string{"pending", "paused", "dispatched"}, p.Continuation) {
		return p, errors.New("UPGRADE_HANDOFF_RESULT_UNVERIFIED")
	}
	for _, proposal := range p.Proposals {
		if !validUpgradeProposal(proposal, execution.Repository) {
			return p, errors.New("UPGRADE_HANDOFF_PROPOSAL_UNVERIFIED")
		}
	}
	for _, item := range p.Consumers {
		if item.RepositoryID <= 0 || !repoPattern.MatchString(item.Repository) ||
			!handoffState(item.Handoff) ||
			(item.Installation != "pending" && item.Installation != "installed") {
			return p, errors.New("UPGRADE_HANDOFF_CONSUMER_UNVERIFIED")
		}
		sanitizeUpgradeCause(item.Cause)
	}
	for _, entry := range []*coreOperationDiagnostic{p.HandoffCause, p.InstallationCause,
		p.ReadinessCause, p.ContinuationCause, p.Cleanup} {
		sanitizeUpgradeCause(entry)
	}
	return p, nil
}

func sanitizeUpgradeCause(entry *coreOperationDiagnostic) {
	if entry == nil {
		return
	}
	if !nativeCode.MatchString(entry.Code) {
		entry.Code = ""
	}
	if !slices.Contains([]string{"pending", "retired", "retained"}, entry.Status) {
		entry.Status = ""
	}
	entry.Installation = ""
	data, _ := json.Marshal(entry.GitHubAPI)
	entry.GitHubAPI = nativeGitHubCause(data)
}

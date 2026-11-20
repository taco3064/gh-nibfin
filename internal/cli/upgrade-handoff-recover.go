package cli

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

type upgradeWorkflowRun struct {
	ID         int64      `json:"id"`
	WorkflowID int64      `json:"workflow_id"`
	URL        string     `json:"html_url"`
	Title      string     `json:"display_title"`
	Event      string     `json:"event"`
	HeadBranch string     `json:"head_branch"`
	Repository Repository `json:"repository"`
	HeadRepo   Repository `json:"head_repository"`
	Attempt    int        `json:"run_attempt"`
	Status     string     `json:"status"`
	Conclusion string     `json:"conclusion"`
}

type upgradeRecoveryObservation struct {
	RunURL string `json:"run_url"`
	State  string `json:"state"`
}

type upgradeWorkflowLocator struct {
	repo     Repository
	file     string
	title    string
	mergedAt time.Time
	notice   bool
}

func (lookup upgradeWorkflowLocator) endpoint() string {
	return "repos/" + lookup.repo.FullName + "/actions/workflows/" + path.Base(lookup.file)
}

func (lookup upgradeWorkflowLocator) match(run upgradeWorkflowRun, id int64) bool {
	branchMatches := run.Event == "workflow_dispatch" &&
		run.HeadBranch == lookup.repo.DefaultBranch || lookup.notice &&
		run.Event == "pull_request" && (run.HeadBranch == upgradeBranchName ||
		run.HeadBranch == lookup.repo.DefaultBranch)
	states := []string{"completed", "queued", "in_progress", "pending", "requested",
		"waiting"}
	knownStatus := slices.Contains(states, run.Status) &&
		(run.Status != "completed" || run.Conclusion != "")
	return run.ID > 0 && run.WorkflowID == id && run.Attempt > 0 &&
		(lookup.title == "" || run.Title == lookup.title) && branchMatches &&
		run.Repository.ID == lookup.repo.ID &&
		run.HeadRepo.ID == lookup.repo.ID && run.URL == lookup.runURL(run.ID) && knownStatus
}

func (lookup upgradeWorkflowLocator) runURL(id int64) string {
	return fmt.Sprintf("https://github.com/%s/actions/runs/%d", lookup.repo.FullName, id)
}

func upgradeNativeWorkflowID(api API, lookup upgradeWorkflowLocator) (int64, error) {
	var workflow struct {
		ID    int64
		Path  string
		State string
	}
	base := lookup.endpoint()
	if err := api.Call("GET", base, nil, &workflow); err != nil {
		return 0, err
	}
	if workflow.ID <= 0 || workflow.Path != lookup.file || workflow.State != "active" {
		return 0, errors.New("UPGRADE_RECOVERY_WORKFLOW_UNVERIFIED")
	}
	return workflow.ID, nil
}

func readUpgradeWorkflowRun(api API, lookup upgradeWorkflowLocator) (
	upgradeWorkflowRun, error) {
	workflowID, err := upgradeNativeWorkflowID(api, lookup)
	if err != nil {
		return upgradeWorkflowRun{}, err
	}
	base := lookup.endpoint()
	var completed upgradeWorkflowRun
	seen := map[int64]bool{}
	for page := 1; ; page++ {
		var list struct {
			Total *int                 `json:"total_count"`
			Runs  []upgradeWorkflowRun `json:"workflow_runs"`
		}
		query := url.Values{"created": {">=" + lookup.mergedAt.Format(time.RFC3339)},
			"per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if err := api.Call("GET", base+"/runs?"+query.Encode(), nil, &list); err != nil {
			return upgradeWorkflowRun{}, err
		}
		if list.Runs == nil || list.Total == nil || *list.Total < 0 {
			return upgradeWorkflowRun{}, errors.New("UPGRADE_RECOVERY_RUN_LIST_UNVERIFIED")
		}
		for _, run := range list.Runs {
			if run.ID <= 0 || seen[run.ID] {
				return upgradeWorkflowRun{}, errors.New("UPGRADE_RECOVERY_RUN_LIST_UNVERIFIED")
			}
			seen[run.ID] = true
			if run.Title != lookup.title {
				continue
			}
			if !lookup.match(run, workflowID) {
				return upgradeWorkflowRun{}, errors.New("UPGRADE_RECOVERY_RUN_UNVERIFIED")
			}
			if run.Status != "completed" || run.Conclusion == "success" {
				return run, nil
			}
			if run.ID > completed.ID {
				completed = run
			}
		}
		if len(list.Runs) < 100 {
			if len(seen) != *list.Total {
				return upgradeWorkflowRun{}, errors.New("UPGRADE_RECOVERY_RUN_LIST_INCOMPLETE")
			}
			return completed, nil
		}
	}
}

func readExactUpgradeRun(api API, lookup upgradeWorkflowLocator, id int64) (
	upgradeWorkflowRun, error) {
	var run upgradeWorkflowRun
	workflowID, err := upgradeNativeWorkflowID(api, lookup)
	if err != nil {
		return run, err
	}
	endpoint := fmt.Sprintf("repos/%s/actions/runs/%d", lookup.repo.FullName, id)
	if err := api.Call("GET", endpoint, nil, &run); err != nil {
		return run, err
	}
	if run.ID != id || !lookup.match(run, workflowID) {
		return run, errors.New("UPGRADE_RECOVERY_RUN_UNVERIFIED")
	}
	return run, nil
}

func unfinishedUpgradeRun(run upgradeWorkflowRun) bool {
	return run.Status == "completed" && run.Conclusion != "success"
}

func observeOrRetryUpgradeRun(api API, lookup upgradeWorkflowLocator, id int64,
	retry bool) (upgradeRecoveryObservation, error) {
	var result upgradeRecoveryObservation
	current, err := readExactUpgradeRun(api, lookup, id)
	if err != nil {
		return result, err
	}
	result = upgradeRecoveryObservation{current.URL, current.Status}
	if !retry || !unfinishedUpgradeRun(current) {
		return result, nil
	}
	endpoint := fmt.Sprintf("repos/%s/actions/runs/%d/rerun", lookup.repo.FullName, id)
	writeErr := api.Call("POST", endpoint, nil, nil)
	observed, err := readExactUpgradeRun(api, lookup, id)
	if err != nil {
		return result, err
	}
	result.State = observed.Status
	if observed.Attempt == current.Attempt && unfinishedUpgradeRun(observed) {
		if writeErr != nil {
			return result, writeErr
		}
		return result, errors.New("UPGRADE_RECOVERY_RERUN_REQUIRES_READBACK")
	}
	return result, nil
}

func readMergedUpgrade(api API, plan upgradePlan, known managedUpgradeProposal) (
	time.Time, error) {
	var pull struct {
		upgradePull
		ID       int64  `json:"id"`
		Merged   *bool  `json:"merged"`
		MergedAt string `json:"merged_at"`
		User     User   `json:"user"`
	}
	endpoint := fmt.Sprintf("repos/%s/pulls/%d", plan.Repository.FullName, known.Pull)
	if err := api.Call("GET", endpoint, nil, &pull); err != nil {
		return time.Time{}, err
	}
	if !known.Merged || pull.Merged == nil || !*pull.Merged || pull.State != "closed" ||
		pull.ID != known.ID || pull.NodeID != known.Node || pull.Number != known.Pull ||
		pull.URL != known.URL || pull.User.ID != plan.MemberID || pull.User.Type != "User" ||
		pull.Head.Ref != upgradeBranchName || pull.Base.Ref != plan.Repository.DefaultBranch ||
		pull.Head.Repository.ID != plan.Repository.ID ||
		pull.Base.Repository.ID != plan.Repository.ID {
		return time.Time{}, errors.New("UPGRADE_RECOVERY_MERGED_PR_UNVERIFIED")
	}
	mergedAt, err := time.Parse(time.RFC3339, pull.MergedAt)
	if err != nil {
		return time.Time{}, errors.New("UPGRADE_RECOVERY_MERGE_TIME_UNVERIFIED")
	}
	return mergedAt, nil
}

func recoverMergedUpgrade(api API, plan upgradePlan, known managedUpgradeProposal,
	recipe upgradeHandoffRecipe, retry bool) (upgradeRecoveryObservation, error) {
	var outcome upgradeRecoveryObservation
	mergedAt, err := readMergedUpgrade(api, plan, known)
	if err != nil {
		return outcome, err
	}
	execution, user, err := repository(api, recipe.execution.Repository, true)
	if err != nil {
		return outcome, err
	}
	if execution.ID != recipe.execution.RepositoryID || user.ID != plan.MemberID {
		return outcome, errors.New("UPGRADE_RECOVERY_EXECUTION_IDENTITY_CHANGED")
	}
	if known.ExecutionRun != nil {
		// The installed Core already bound this exact native ID, including older
		// authorized runs created before the workflows had a custom run name.
		lookup := upgradeWorkflowLocator{repo: execution, file: recipe.execPath,
			mergedAt: mergedAt}
		return observeOrRetryUpgradeRun(api, lookup, known.ExecutionRun.ID, retry)
	}
	if !strings.Contains(recipe.notice, "Nibfin upgrade notification · PR ") ||
		!strings.Contains(recipe.executor, "Nibfin upgrade · repository ") {
		return outcome, errors.New("UPGRADE_RECOVERY_NATIVE_RUN_LOCATOR_UNAVAILABLE")
	}
	notice, err := installedUpgradeFile(api, plan, recipe.noticePath)
	if err != nil {
		return outcome, err
	}
	if strings.ReplaceAll(notice, "\r\n", "\n") != recipe.notice {
		return outcome, errors.New("UPGRADE_RECOVERY_NOTIFICATION_CHANGED")
	}
	title := fmt.Sprintf("Nibfin upgrade · repository %d · PR %d", known.RepositoryID,
		known.Pull)
	lookup := upgradeWorkflowLocator{repo: execution, file: recipe.execPath, title: title,
		mergedAt: mergedAt}
	receiver, err := readUpgradeWorkflowRun(api, lookup)
	if err != nil {
		return outcome, err
	}
	if receiver.ID != 0 {
		return observeOrRetryUpgradeRun(api, lookup, receiver.ID, retry)
	}
	lookup = upgradeWorkflowLocator{repo: plan.Repository, file: recipe.noticePath,
		title:    fmt.Sprintf("Nibfin upgrade notification · PR %d", known.Pull),
		mergedAt: mergedAt, notice: true}
	prior, err := readUpgradeWorkflowRun(api, lookup)
	if err != nil {
		return outcome, err
	}
	if prior.ID != 0 && !unfinishedUpgradeRun(prior) {
		return upgradeRecoveryObservation{prior.URL, prior.Status}, nil
	}
	if prior.ID != 0 {
		return observeOrRetryUpgradeRun(api, lookup, prior.ID, retry)
	}
	if !retry {
		return outcome, errors.New("UPGRADE_RECOVERY_NATIVE_RESULT_UNAVAILABLE")
	}
	writeErr := api.Call("POST", lookup.endpoint()+"/dispatches", map[string]any{
		"ref":    plan.Repository.DefaultBranch,
		"inputs": map[string]string{"pull_number": strconv.Itoa(known.Pull)},
	}, nil)
	current, err := readUpgradeWorkflowRun(api, lookup)
	if err != nil {
		return outcome, err
	}
	if current.ID == 0 {
		if writeErr != nil {
			return outcome, writeErr
		}
		return outcome, errors.New("UPGRADE_RECOVERY_DISPATCH_REQUIRES_READBACK")
	}
	return upgradeRecoveryObservation{current.URL, current.Status}, nil
}

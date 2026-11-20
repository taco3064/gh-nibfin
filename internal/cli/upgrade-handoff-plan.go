package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const upgradeHandoffContract = "src/upgrade-continuation-contract.json"

type upgradeHandoffRecipe struct {
	execution  SharedExecution
	noticePath string
	execPath   string
	variable   string
	secret     string
	prepare    string
	bind       string
	observe    string
	complete   string
	notice     string
	executor   string
}

var upgradeWorkflowPath = regexp.MustCompile(`^\.github/workflows/nibfin-[\w-]+\.yml$`)
var upgradeRouteName = regexp.MustCompile(`^NIBFIN_UPGRADE_[A-Z_]+$`)

func readUpgradeHandoffRecipe(api API, pin SourcePin, execution SharedExecution) (
	*upgradeHandoffRecipe, error) {
	if pin.Repository != officialCore || !commitPattern.MatchString(pin.Commit) ||
		execution.RepositoryID <= 0 || !repoPattern.MatchString(execution.Repository) {
		return nil, errors.New("UPGRADE_HANDOFF_SOURCE_INVALID")
	}
	data, _, err := content(api, pin.Repository, upgradeHandoffContract, pin.Commit)
	if err != nil {
		if err.Error() == "NOT_FOUND" {
			return nil, nil
		}
		return nil, err
	}
	var contract struct {
		Schema         int `json:"schemaVersion"`
		Bridge         string
		ProposalBranch string
		Recipes        struct{ Source, Notification, Execution string }
		Notification   struct{ DestinationVariable, DispatchSecret string }
		Administrator  struct{ Input, Prepare, Bind, Observe, Complete string }
	}
	if json.Unmarshal(data, &contract) != nil || contract.Schema != 1 ||
		contract.Bridge != maintenanceBridge || contract.ProposalBranch != upgradeBranchName ||
		contract.Recipes.Source != "src/upgrade-continuation-recipes.json" ||
		!upgradeWorkflowPath.MatchString(contract.Recipes.Notification) ||
		!upgradeWorkflowPath.MatchString(contract.Recipes.Execution) ||
		contract.Recipes.Notification == contract.Recipes.Execution ||
		!upgradeRouteName.MatchString(contract.Notification.DestinationVariable) ||
		!upgradeRouteName.MatchString(contract.Notification.DispatchSecret) ||
		contract.Administrator.Input != "private-json-stdin" ||
		contract.Administrator.Prepare != "upgrade-prepare" ||
		contract.Administrator.Bind != "upgrade-bind" ||
		contract.Administrator.Observe != "upgrade-observe" ||
		contract.Administrator.Complete != "upgrade-complete" {
		return nil, errors.New("UPGRADE_HANDOFF_CONTRACT_INVALID")
	}
	data, _, err = content(api, pin.Repository, contract.Recipes.Source, pin.Commit)
	if err != nil {
		return nil, err
	}
	var recipes struct{ Notification, Execution []string }
	if json.Unmarshal(data, &recipes) != nil || !validUpgradeRecipe(recipes.Notification) ||
		!validUpgradeRecipe(recipes.Execution) ||
		!strings.Contains(strings.Join(recipes.Execution, "\n"), "MEMBER_ID") {
		return nil, errors.New("UPGRADE_HANDOFF_RECIPES_INVALID")
	}
	return &upgradeHandoffRecipe{execution, contract.Recipes.Notification,
		contract.Recipes.Execution, contract.Notification.DestinationVariable,
		contract.Notification.DispatchSecret, contract.Administrator.Prepare,
		contract.Administrator.Bind, contract.Administrator.Observe,
		contract.Administrator.Complete, strings.Join(recipes.Notification, "\n"),
		strings.Join(recipes.Execution, "\n")}, nil
}

func validUpgradeRecipe(lines []string) bool {
	if len(lines) < 2 || lines[len(lines)-1] != "" {
		return false
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "\r\n") {
			return false
		}
	}
	return true
}

func (recipe upgradeHandoffRecipe) files(repoID, memberID int64) map[string]string {
	files := map[string]string{recipe.noticePath: recipe.notice}
	if repoID == recipe.execution.RepositoryID {
		files[recipe.execPath] = strings.ReplaceAll(recipe.executor, "MEMBER_ID",
			strconv.FormatInt(memberID, 10))
	}
	return files
}

func composeUpgradeHandoff(api API, plan *upgradePlan) error {
	next := plan.Target.handoff
	var old *upgradeHandoffRecipe
	for file, expected := range next.files(plan.Repository.ID, plan.MemberID) {
		for _, change := range plan.Changes {
			if change.Path == file {
				return errors.New("UPGRADE_HANDOFF_PATH_CONFLICT")
			}
		}
		if _, exists := plan.entries[file]; !exists {
			plan.Changes = append(plan.Changes, upgradeChange{file, "added", expected})
			continue
		}
		actual, err := installedUpgradeFile(api, *plan, file)
		if err != nil {
			return err
		}
		actual = strings.ReplaceAll(actual, "\r\n", "\n")
		if actual == expected {
			continue
		}
		if old == nil {
			old, err = readUpgradeHandoffRecipe(api, plan.Current.Core, next.execution)
			if err != nil {
				return err
			}
		}
		if old == nil || old.files(plan.Repository.ID, plan.MemberID)[file] != actual {
			return fmt.Errorf("UNRECOGNIZED_MANAGED_PATH: %s", file)
		}
		plan.Changes = append(plan.Changes, upgradeChange{file, "modified", expected})
	}
	sort.Slice(plan.Changes, func(i, j int) bool {
		return plan.Changes[i].Path < plan.Changes[j].Path
	})
	return nil
}

func upgradeExecutionRoute(api API, host sharedHost, recipe upgradeHandoffRecipe) (
	bool, error) {
	repo, _, err := repository(api, host.Repository, false)
	if err != nil {
		return false, err
	}
	if repo.ID != host.RepositoryID || repo.DefaultBranch == "" {
		return false, errors.New("UPGRADE_EXECUTION_REPOSITORY_CHANGED")
	}
	var observed struct {
		ID    int64
		Path  string
		State string
	}
	endpoint := "repos/" + repo.FullName + "/actions/workflows/" +
		path.Base(recipe.execPath)
	if err := api.Call("GET", endpoint, nil, &observed); err != nil {
		if err.Error() == "NOT_FOUND" {
			return false, nil
		}
		return false, err
	}
	if observed.ID <= 0 || observed.Path != recipe.execPath || observed.State != "active" {
		return false, errors.New("UPGRADE_EXECUTION_WORKFLOW_CONFLICT")
	}
	var ref reference
	path := "repos/" + repo.FullName + "/git/ref/heads/" + url.PathEscape(repo.DefaultBranch)
	if err := api.Call("GET", path, nil, &ref); err != nil {
		return false, err
	}
	if ref.Ref != "refs/heads/"+repo.DefaultBranch ||
		!commitPattern.MatchString(ref.Object.SHA) {
		return false, errors.New("UPGRADE_EXECUTION_DEFAULT_BRANCH_UNVERIFIED")
	}
	actual, _, err := content(api, repo.FullName, recipe.execPath, ref.Object.SHA)
	if err != nil {
		if err.Error() == "NOT_FOUND" {
			return false, nil
		}
		return false, err
	}
	return strings.ReplaceAll(string(actual), "\r\n", "\n") ==
		recipe.files(repo.ID, host.MemberID)[recipe.execPath], nil
}

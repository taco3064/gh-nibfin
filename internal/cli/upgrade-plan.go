package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
)

const upgradeBranchName = "nibfin/upgrade"

const hostUpgradeObligation = "PR only; merge requires native review, " +
	"protections and CI. " +
	"Host adoption is a separate trusted operation; no running host, Runner, " +
	"delivery, credentials, grants or Pause state was changed."

type upgradeChange struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Content string `json:"content,omitempty"`
}

type upgradePlan struct {
	ManagedIssue  bool            `json:"managed_issue_proposal,omitempty"`
	DesiredRecipe string          `json:"desired_issue_recipe,omitempty"`
	Branch        string          `json:"branch"`
	Repository    Repository      `json:"repository"`
	Base          string          `json:"base"`
	BaseTree      string          `json:"base_tree"`
	MemberID      int64           `json:"member_id"`
	Current       Producers       `json:"current"`
	Target        upgradeSource   `json:"target"`
	Compatibility string          `json:"compatibility"`
	Changes       []upgradeChange `json:"changes"`
	HostAdoption  string          `json:"host_adoption"`
	ID            string          `json:"id"`
	entries       map[string]upgradeEntry
	issueRecipe   string
	frozenTarget  bool
}

var memberBindingPattern = regexp.MustCompile(`nibfin-user-([1-9][0-9]{0,15})`)

func planUpgrade(api API, name, version string, managed ...bool) (upgradePlan, error) {
	managedIssue := len(managed) != 0 && managed[0]
	return planUpgradeAt(api, name, version, managedIssue, "")
}

// A retained proposal is checked against the verified installation at its parent.
func planUpgradeAt(api API, name, version string, managed bool,
	revision string) (upgradePlan, error) {
	return planUpgradeRecipeAt(api, name, version, managed, "", revision)
}

func planNativeUpgrade(api API, name, version string) (upgradePlan, error) {
	current, err := planUpgrade(api, name, version)
	if err != nil || current.issueRecipe == "issue" {
		// Native-to-native upgrade keeps historical proposal identity, including
		// legacy PRs whose original message contains only its plan ID.
		return current, err
	}
	plan, err := planUpgradeSourceAt(api, name, version, false, "issue", "", &current.Target)
	// Public PR-only upgrades retain their original release revalidation policy.
	plan.frozenTarget = false
	return plan, err
}

func replanUpgrade(api API, plan upgradePlan, version,
	revision string) (upgradePlan, error) {
	if !plan.frozenTarget {
		return planUpgradeRecipeAt(api, plan.Repository.FullName, version,
			plan.ManagedIssue, plan.DesiredRecipe, revision)
	}
	return planUpgradeSourceAt(api, plan.Repository.FullName, version,
		plan.ManagedIssue, plan.DesiredRecipe, revision, &plan.Target)
}

func planUpgradeRecipeAt(api API, name, version string, managed bool,
	desired, revision string) (upgradePlan, error) {
	return planUpgradeSourceAt(api, name, version, managed, desired, revision, nil)
}

func planNativeUpgradeTarget(api API, name, version string,
	target upgradeSource) (upgradePlan, error) {
	current, err := planUpgradeSourceAt(api, name, version, false, "", "", &target)
	if err != nil || current.issueRecipe == "issue" {
		return current, err
	}
	return planUpgradeSourceAt(api, name, version, false, "issue", "", &target)
}

func planUpgradeSourceAt(api API, name, version string, managed bool,
	desired, revision string, selected *upgradeSource) (upgradePlan, error) {
	plan := upgradePlan{Branch: upgradeBranchName, Changes: []upgradeChange{},
		HostAdoption: hostUpgradeObligation}
	plan.ManagedIssue = managed
	plan.DesiredRecipe = desired
	plan.frozenTarget = selected != nil
	if selected != nil && (selected.Kind != "" && selected.Kind != "installed-current" ||
		selected.Kind == "installed-current" && (selected.Execution == nil ||
			!repoPattern.MatchString(selected.Execution.Repository) ||
			selected.Execution.RepositoryID <= 0)) {
		return plan, errors.New("NATIVE_MIGRATION_TARGET_AUTHORITY_INVALID")
	}
	if desired != "" && desired != "issue" {
		return plan, errors.New("UNSUPPORTED_UPGRADE_ISSUE_RECIPE")
	}
	repo, _, err := repository(api, name, false)
	if err != nil {
		return plan, err
	}
	plan.Repository = repo
	if repo.DefaultBranch == "" {
		return plan, errors.New("INITIALIZED_TARGET_REQUIRED")
	}
	base := "repos/" + repo.FullName
	var ref reference
	if err := api.Call("GET", base+"/git/ref/heads/"+
		url.PathEscape(repo.DefaultBranch), nil, &ref); err != nil {
		return plan, err
	}
	if ref.Ref != "refs/heads/"+repo.DefaultBranch ||
		!commitPattern.MatchString(ref.Object.SHA) {
		return plan, errors.New("TARGET_HEAD_UNAVAILABLE")
	}
	plan.Base = ref.Object.SHA
	if revision != "" {
		plan.Base = revision
	}
	var commit upgradeCommit
	if err := api.Call("GET", base+"/git/commits/"+plan.Base,
		nil, &commit); err != nil {
		return plan, err
	}
	if commit.SHA != plan.Base || !commitPattern.MatchString(commit.Tree.SHA) {
		return plan, errors.New("TARGET_TREE_UNAVAILABLE")
	}
	plan.BaseTree = commit.Tree.SHA
	plan.entries, err = readUpgradeTree(api, base, plan.BaseTree)
	if err != nil {
		return plan, err
	}
	pins, err := installedUpgradeFile(api, plan, ".nibfin/producer-pins.json")
	if err != nil {
		return plan, err
	}
	if decodeUpgradePins([]byte(pins), &plan.Current) != nil {
		return plan, errors.New("INVALID_INSTALLED_PRODUCERS")
	}
	readRecipes := upgradeRecipes
	if selected != nil && (selected.Kind == "installed-current" || selected.maintained) {
		readRecipes = maintainedUpgradeRecipes
	}
	recipes, err := readRecipes(api, plan.Current)
	if err != nil {
		return plan, err
	}
	wake, err := installedUpgradeFile(api, plan, ".github/workflows/nibfin-wake.yml")
	if err != nil {
		return plan, err
	}
	members := memberBindingPattern.FindAllStringSubmatch(wake, -1)
	if len(members) == 0 {
		return plan, errors.New("INSTALLED_MEMBER_BINDING_REQUIRED")
	}
	plan.MemberID, err = strconv.ParseInt(members[0][1], 10, 64)
	if err != nil || plan.MemberID > 9007199254740991 {
		return plan, errors.New("INVALID_INSTALLED_MEMBER_BINDING")
	}
	issue, err := installedUpgradeFile(api, plan, managedIssueWorkflow)
	if err != nil {
		return plan, err
	}
	recipe := recognizeIssueRecipe(plan.Current, plan.MemberID, recipes, issue)
	if recipe == "" {
		return plan, errors.New("INSTALLED_ISSUE_CALLER_NOT_RECOGNIZED")
	}
	plan.issueRecipe = recipe
	old := templatesForRecipe(plan.Current, plan.MemberID, recipes, recipe)
	for path, expected := range old {
		observed, err := installedUpgradeFile(api, plan, path)
		if err != nil {
			return plan, err
		}
		if observed != expected {
			return plan, fmt.Errorf("INSTALLED_MANAGED_FILE_CHANGED: %s", path)
		}
	}
	if selected == nil {
		plan.Target, err = resolveUpgrade(api, version)
	} else {
		plan.Target = *selected
		plan.Target.recipes, err = readRecipes(api, selected.Producers)
	}
	if err != nil {
		return plan, err
	}
	if plan.ManagedIssue && recipe != "issue-managed" {
		if err := requireManagedRouteEligible(api, plan.Repository.FullName); err != nil {
			return plan, err
		}
		if err := requireInactiveSharedWake(api, plan.Repository.FullName); err != nil {
			return plan, err
		}
		recipe = "issue-managed"
	}
	if plan.DesiredRecipe != "" {
		recipe = plan.DesiredRecipe
	}
	if len(plan.Target.recipes[recipe]) == 0 {
		return plan, errors.New("TARGET_ISSUE_RECIPE_UNSUPPORTED")
	}
	next := templatesForRecipe(plan.Target.Producers, plan.MemberID,
		plan.Target.recipes, recipe)
	paths := map[string]bool{}
	for path := range old {
		paths[path] = true
	}
	for path := range next {
		paths[path] = true
	}
	// Both known layouts reserve this path. Never leave or overwrite a replacement.
	paths[".github/workflows/nibfin-stage.yml"] = true
	ordered := []string{}
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		before, installed := old[path]
		after, wanted := next[path]
		if selected != nil && selected.Kind == "installed-current" && installed &&
			path == ".nibfin/producer-pins.json" &&
			installedProducerSourcesMatch([]byte(before), selected.Producers) {
			after = before
		}
		if !installed {
			if _, exists := plan.entries[path]; exists {
				return plan, fmt.Errorf("UNRECOGNIZED_MANAGED_PATH: %s", path)
			}
			if !wanted {
				continue
			}
			plan.Changes = append(plan.Changes, upgradeChange{path, "added", after})
		} else if !wanted {
			plan.Changes = append(plan.Changes, upgradeChange{path, "deleted", ""})
		} else if before != after {
			plan.Changes = append(plan.Changes, upgradeChange{path, "modified", after})
		}
	}
	if plan.Target.handoff != nil {
		if err := composeUpgradeHandoff(api, &plan); err != nil {
			return plan, err
		}
	}
	plan.Compatibility = "verified immutable sources and recognized managed layout"
	data, _ := json.Marshal(plan)
	plan.ID = sha256Hash(data)
	return plan, nil
}

func installedUpgradeFile(api API, plan upgradePlan, path string) (string, error) {
	entry, exists := plan.entries[path]
	if !exists || entry.Mode != "100644" || entry.Type != "blob" {
		return "", fmt.Errorf("INSTALLED_MANAGED_FILE_MISSING_OR_UNSAFE: %s", path)
	}
	data, sha, err := content(api, plan.Repository.FullName, path, plan.Base)
	if err != nil {
		return "", err
	}
	if entry.SHA != sha {
		return "", errors.New("INSTALLED_TREE_CONTENT_MISMATCH")
	}
	return string(data), nil
}

func revalidateUpgrade(api API, plan upgradePlan, version string) error {
	repo, _, err := repository(api, plan.Repository.FullName, false)
	if err != nil {
		return err
	}
	if repo.ID != plan.Repository.ID || repo.FullName != plan.Repository.FullName ||
		repo.Owner.ID != plan.Repository.Owner.ID ||
		repo.DefaultBranch != plan.Repository.DefaultBranch {
		return errors.New("UPGRADE_REPOSITORY_OR_DEFAULT_BRANCH_CHANGED_PREVIEW_AGAIN")
	}
	var ref reference
	if err := api.Call("GET", "repos/"+repo.FullName+"/git/ref/heads/"+
		url.PathEscape(repo.DefaultBranch), nil, &ref); err != nil {
		return err
	}
	if ref.Ref != "refs/heads/"+repo.DefaultBranch || ref.Object.SHA != plan.Base {
		return errors.New("UPGRADE_BASE_CHANGED_PREVIEW_AGAIN")
	}
	current, err := replanUpgrade(api, plan, version, "")
	if err != nil {
		return err
	}
	if current.ID != plan.ID {
		return errors.New("UPGRADE_PLAN_CHANGED_PREVIEW_AGAIN")
	}
	return nil
}

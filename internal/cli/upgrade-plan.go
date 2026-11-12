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
}

var memberBindingPattern = regexp.MustCompile(`nibfin-user-([1-9][0-9]{0,15})`)

func planUpgrade(api API, name, version string) (upgradePlan, error) {
	plan := upgradePlan{Branch: upgradeBranchName, Changes: []upgradeChange{},
		HostAdoption: hostUpgradeObligation}
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
	recipes, err := upgradeRecipes(api, plan.Current)
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
	old := templates(plan.Current, plan.MemberID, recipes)
	for path, expected := range old {
		observed, err := installedUpgradeFile(api, plan, path)
		if err != nil {
			return plan, err
		}
		if observed != expected {
			return plan, fmt.Errorf("INSTALLED_MANAGED_FILE_CHANGED: %s", path)
		}
	}
	plan.Target, err = resolveUpgrade(api, version)
	if err != nil {
		return plan, err
	}
	next := templates(plan.Target.Producers, plan.MemberID, plan.Target.recipes)
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
	current, err := planUpgrade(api, plan.Repository.FullName, version)
	if err != nil {
		return err
	}
	if current.ID != plan.ID {
		return errors.New("UPGRADE_PLAN_CHANGED_PREVIEW_AGAIN")
	}
	return nil
}

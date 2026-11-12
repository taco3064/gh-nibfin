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

var installedMemberPattern = regexp.MustCompile(`nibfin-user-([1-9][0-9]*)`)

type upgradeChange struct {
	Path   string  `json:"path"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

type upgradePlan struct {
	Repository Repository       `json:"repository"`
	Head       string           `json:"base_commit"`
	MemberID   int64            `json:"member_id"`
	Selection  upgradeSelection `json:"selection"`
	Installed  Producers        `json:"installed"`
	Target     upgradeSource    `json:"target"`
	Changes    []upgradeChange  `json:"changes"`
	Compatible bool             `json:"compatible"`
	Adoption   string           `json:"host_adoption"`
	ActiveWork string           `json:"active_work"`
}

func upgradeHead(api API, repo Repository) (string, error) {
	var ref reference
	if repo.DefaultBranch == "" {
		return "", errors.New("INITIALIZED_TARGET_REQUIRED")
	}
	path := "repos/" + repo.FullName + "/git/ref/heads/" +
		url.PathEscape(repo.DefaultBranch)
	if err := api.Call("GET", path, nil, &ref); err != nil {
		return "", err
	}
	if !commitPattern.MatchString(ref.Object.SHA) {
		return "", errors.New("TARGET_HEAD_UNAVAILABLE")
	}
	return ref.Object.SHA, nil
}

func installedTemplates(api API, repo, head string) (
	Producers, int64, map[string]string, error,
) {
	data, _, err := content(api, repo, producerPath, head)
	if err != nil {
		return Producers{}, 0, nil, err
	}
	pins, err := decodeProducers(data)
	if err == nil {
		err = verifyProducers(api, pins)
	}
	if err != nil {
		return pins, 0, nil, err
	}
	recipes, err := callerRecipes(api, pins.Core)
	if err != nil {
		return pins, 0, nil, err
	}
	wake, _, err := content(api, repo, wakeWorkflowPath, head)
	if err != nil {
		return pins, 0, nil, err
	}
	matches := installedMemberPattern.FindAllSubmatch(wake, -1)
	if len(matches) == 0 {
		return pins, 0, nil, errors.New("INSTALLED_MEMBER_UNRECOGNIZED")
	}
	member, err := strconv.ParseInt(string(matches[0][1]), 10, 64)
	if err != nil || member > 9007199254740991 {
		return pins, 0, nil, errors.New("INSTALLED_MEMBER_UNRECOGNIZED")
	}
	files := templates(pins, member, recipes)
	for path, expected := range files {
		body, _, err := content(api, repo, path, head)
		if err != nil || string(body) != expected {
			return pins, 0, nil, fmt.Errorf("MODIFIED_MANAGED_FILE: %s", path)
		}
	}
	return pins, member, files, nil
}

func planUpgrade(api API, name string, selection upgradeSelection) (upgradePlan, error) {
	plan := upgradePlan{Selection: selection, Changes: []upgradeChange{},
		Adoption:   "separate_authorized_host_adoption_required",
		ActiveWork: "retain_original_approved_core_skills_workflow_context"}
	repo, _, err := repository(api, name, false)
	if err != nil {
		return plan, err
	}
	plan.Repository = repo
	plan.Head, err = upgradeHead(api, repo)
	if err != nil {
		return plan, err
	}
	var before map[string]string
	plan.Installed, plan.MemberID, before, err = installedTemplates(api,
		repo.FullName, plan.Head)
	if err != nil {
		return plan, err
	}
	plan.Target, err = resolveUpgrade(api, selection)
	if err != nil {
		return plan, err
	}
	recipes, err := callerRecipes(api, plan.Target.Pins.Core)
	if err != nil {
		return plan, err
	}
	after := templates(plan.Target.Pins, plan.MemberID, recipes)
	paths := map[string]bool{}
	for path := range before {
		paths[path] = true
	}
	for path := range after {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		old, had := before[path]
		fresh, has := after[path]
		if !had {
			_, _, err := content(api, repo.FullName, path, plan.Head)
			if err == nil || err.Error() != "NOT_FOUND" {
				return plan, fmt.Errorf("UNRECOGNIZED_MANAGED_PATH: %s", path)
			}
		}
		if had && has && old == fresh {
			continue
		}
		change := upgradeChange{Path: path}
		if had {
			change.Before = &old
		}
		if has {
			change.After = &fresh
		}
		plan.Changes = append(plan.Changes, change)
	}
	// A stale legacy stage file is also a conflict in the unified layout.
	stage := stageWorkflowPath
	if !paths[stage] {
		_, _, err := content(api, repo.FullName, stage, plan.Head)
		if err == nil || err.Error() != "NOT_FOUND" {
			return plan, errors.New("UNRECOGNIZED_MANAGED_STAGE_FILE")
		}
	}
	plan.Compatible = true
	return plan, nil
}

func sameUpgradePlan(left, right upgradePlan) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

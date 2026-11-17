package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strconv"
)

type relayWorkflow struct {
	ID     int64  `json:"id"`
	Path   string `json:"path"`
	Commit string `json:"commit"`
	Blob   string `json:"blob"`
}

type relayOrigin struct {
	Repository   string        `json:"repository"`
	RepositoryID int64         `json:"repositoryId"`
	Ref          string        `json:"ref"`
	Workflow     relayWorkflow `json:"workflow"`
}

type relayConfiguration struct {
	Version int           `json:"schemaVersion"`
	Origins []relayOrigin `json:"origins"`
}

type relayAdoptions struct {
	MemberID int64     `json:"memberId"`
	Pin      SourcePin `json:"pin"`
	Origins  []struct {
		Repository   string `json:"repository"`
		RepositoryID int64  `json:"repositoryId"`
		Event        struct {
			Repository   string `json:"repository"`
			RepositoryID int64  `json:"repositoryId"`
			MemberID     int64  `json:"memberId"`
			WorkflowID   int64  `json:"workflowId"`
			WorkflowPath string `json:"workflowPath"`
			SourceCommit string `json:"sourceCommit"`
			WorkflowBlob string `json:"workflowBlob"`
			Reusable     struct {
				Repository string `json:"repository"`
				Path       string `json:"path"`
				Commit     string `json:"commit"`
			} `json:"reusable"`
		} `json:"event"`
	} `json:"origins"`
}

func strictRelayConfig(data []byte) (relayConfiguration, error) {
	var config relayConfiguration
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&config)
	var extra any
	if err != nil || decoder.Decode(&extra) != io.EOF || config.Version != 1 ||
		len(config.Origins) < 1 || len(config.Origins) > 10 {
		return config, errors.New("MANAGED_RELAY_CONFIGURATION_CONFLICT")
	}
	ids := map[int64]bool{}
	for _, origin := range config.Origins {
		if origin.RepositoryID <= 0 || ids[origin.RepositoryID] ||
			!repoPattern.MatchString(origin.Repository) || origin.Ref == "" ||
			origin.Workflow.ID <= 0 || origin.Workflow.Path != managedIssueWorkflow ||
			!commitPattern.MatchString(origin.Workflow.Commit) ||
			!commitPattern.MatchString(origin.Workflow.Blob) {
			return config, errors.New("MANAGED_RELAY_CONFIGURATION_CONFLICT")
		}
		ids[origin.RepositoryID] = true
	}
	return config, nil
}

func relayBindings(api API, data []byte, plan InitPlan,
	current relayConfiguration) (relayConfiguration, error) {
	config := relayConfiguration{Version: 1, Origins: []relayOrigin{}}
	var adopted relayAdoptions
	if json.Unmarshal(data, &adopted) != nil || adopted.MemberID != plan.Member.ID ||
		!reflect.DeepEqual(adopted.Pin, Pins().Core) || len(adopted.Origins) > 10 {
		return config, errors.New("MANAGED_RELAY_ADOPTION_CONFLICT")
	}
	recipes, err := callerRecipes(api, Pins().Core)
	if err != nil || len(recipes["issue-managed"]) == 0 {
		return config, errors.New("MANAGED_CALLER_SOURCE_UNAVAILABLE")
	}
	expected := templatesForRecipe(Pins(), plan.Member.ID, recipes,
		"issue-managed")[managedIssueWorkflow]
	ids := map[int64]bool{}
	for _, origin := range adopted.Origins {
		required := origin.RepositoryID == plan.Repository.ID
		for _, existing := range current.Origins {
			required = required || existing.RepositoryID == origin.RepositoryID
		}
		if !required {
			continue
		}
		event := origin.Event
		if ids[origin.RepositoryID] || origin.RepositoryID <= 0 ||
			origin.Repository != event.Repository ||
			origin.RepositoryID != event.RepositoryID || event.MemberID != plan.Member.ID ||
			event.WorkflowID <= 0 || event.WorkflowPath != managedIssueWorkflow ||
			!commitPattern.MatchString(event.SourceCommit) ||
			!commitPattern.MatchString(event.WorkflowBlob) ||
			event.Reusable.Repository != Pins().Core.Repository ||
			event.Reusable.Path != ".github/workflows/issue.yml" ||
			event.Reusable.Commit != Pins().Core.Commit {
			return config, errors.New("MANAGED_RELAY_ADOPTION_CONFLICT")
		}
		ids[origin.RepositoryID] = true
		repo, _, err := repository(api, origin.Repository, false)
		if err != nil || repo.ID != origin.RepositoryID || repo.DefaultBranch == "" {
			return config, errors.New("MANAGED_RELAY_ORIGIN_CHANGED")
		}
		binding := relayOrigin{origin.Repository, origin.RepositoryID,
			repo.DefaultBranch, relayWorkflow{event.WorkflowID, event.WorkflowPath,
				event.SourceCommit, event.WorkflowBlob}}
		if err := verifyRelayOrigin(api, binding, expected); err != nil {
			return config, err
		}
		config.Origins = append(config.Origins, binding)
	}
	if !ids[plan.Repository.ID] {
		return config, errors.New("MANAGED_RELAY_TARGET_NOT_ADOPTED")
	}
	return config, nil
}

func verifyRelayOrigin(api API, origin relayOrigin, expected string) error {
	base := "repos/" + origin.Repository
	var branch struct {
		Name      string               `json:"name"`
		Protected bool                 `json:"protected"`
		Commit    struct{ SHA string } `json:"commit"`
	}
	path := base + "/branches/" + url.PathEscape(origin.Ref)
	if err := api.Call("GET", path, nil, &branch); err != nil ||
		branch.Name != origin.Ref ||
		!commitPattern.MatchString(branch.Commit.SHA) {
		return errors.New("MANAGED_RELAY_BRANCH_REQUIRES_READBACK")
	}
	var workflow struct {
		ID    int64  `json:"id"`
		Path  string `json:"path"`
		State string `json:"state"`
	}
	if err := api.Call("GET", base+"/actions/workflows/"+
		strconv.FormatInt(origin.Workflow.ID, 10), nil, &workflow); err != nil ||
		workflow.ID != origin.Workflow.ID || workflow.Path != origin.Workflow.Path ||
		workflow.State != "active" {
		return errors.New("MANAGED_RELAY_WORKFLOW_REQUIRES_READBACK")
	}
	for _, ref := range []string{origin.Workflow.Commit, branch.Commit.SHA} {
		body, blob, err := content(api, origin.Repository, origin.Workflow.Path, ref)
		if err != nil || blob != origin.Workflow.Blob || string(body) != expected {
			return errors.New("MANAGED_RELAY_CALLER_REQUIRES_UPGRADE")
		}
	}
	return nil
}

func reconcileRelayOrigins(current, adopted relayConfiguration,
	target int64) (relayConfiguration, error) {
	// Only the requested target is added. Other inactive adoptions do not gain
	// routing as a side effect. Every existing origin must remain exactly bound.
	next := current
	next.Version = 1
	next.Origins = append([]relayOrigin{}, current.Origins...)
	for _, origin := range current.Origins {
		found := false
		for _, candidate := range adopted.Origins {
			found = found || origin == candidate
		}
		if !found {
			return next, errors.New("MANAGED_RELAY_CONFIGURATION_CONFLICT")
		}
	}
	for _, origin := range adopted.Origins {
		if origin.RepositoryID != target {
			continue
		}
		for _, existing := range current.Origins {
			if existing.RepositoryID == target {
				return next, nil
			}
		}
		next.Origins = append(next.Origins, origin)
		return next, nil
	}
	return next, errors.New("MANAGED_RELAY_TARGET_NOT_ADOPTED")
}

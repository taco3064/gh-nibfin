package cli

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
)

type Change struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type InitPlan struct {
	Sources       *Producers       `json:"sources,omitempty"`
	NativeManaged bool             `json:"native_managed,omitempty"`
	Agent         *agentBinding    `json:"agent,omitempty"`
	IssueRecipe   string           `json:"issue_recipe,omitempty"`
	Repository    Repository       `json:"repository"`
	Member        User             `json:"member"`
	Head          string           `json:"head"`
	Files         []Change         `json:"files"`
	Labels        []string         `json:"labels"`
	Execution     *SharedExecution `json:"shared_execution,omitempty"`
	allowUntitled bool
}

type reference struct {
	Ref    string `json:"ref"`
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

func planInit(api API, name string) (InitPlan, error) {
	return planInitWithExecution(api, name, "")
}

func planInitWithExecution(api API, name, execution string) (InitPlan, error) {
	return planInitRecipe(api, name, execution, "issue")
}

func planInitRecipe(api API, name, execution, recipe string) (InitPlan, error) {
	return planInitRecipeWithPins(api, name, execution, recipe, Pins())
}

func planInitRecipeWithPins(api API, name, execution, recipe string,
	pins Producers) (InitPlan, error) {
	return planInitRecipeLayout(api, name, execution, recipe, pins, false)
}

func planInitRecipeLayout(api API, name, execution, recipe string,
	pins Producers, allowUntitled bool) (InitPlan, error) {
	repo, user, err := repository(api, name, false)
	plan := InitPlan{Repository: repo, Member: user, Files: []Change{}, Labels: []string{}}
	if err != nil {
		return plan, err
	}
	plan.IssueRecipe = recipe
	if err := verifyProducers(api, pins); err != nil {
		return plan, err
	}
	var ref reference
	path := "repos/" + name + "/git/ref/heads/" + url.PathEscape(repo.DefaultBranch)
	if repo.DefaultBranch == "" {
		return plan, errors.New("INITIALIZED_TARGET_REQUIRED")
	}
	if err := api.Call("GET", path, nil, &ref); err != nil {
		return plan, err
	}
	if len(ref.Object.SHA) != 40 {
		return plan, errors.New("TARGET_HEAD_UNAVAILABLE")
	}
	plan.Head = ref.Object.SHA
	recipes, err := callerRecipes(api, pins.Core)
	if err != nil {
		return plan, err
	}
	if recipe != "issue" && recipe != "issue-managed" || len(recipes[recipe]) == 0 {
		return plan, errors.New("MANAGED_CALLER_SOURCE_UNAVAILABLE")
	}
	files := templatesForRecipe(pins, user.ID, recipes, recipe)
	if execution != "" {
		if err := planSharedExecution(api, &plan, files, pins.Core, execution); err != nil {
			return plan, err
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		data, _, err := content(api, name, path, plan.Head)
		if err != nil && err.Error() != "NOT_FOUND" {
			return plan, err
		}
		if err == nil {
			if !initFileMatches(path, files[path], data, pins,
				recipe == "issue" && execution != "") &&
				!matchesUntitledCaller(path, files[path], string(data), allowUntitled) {
				if path == managedIssueWorkflow && recipe == "issue-managed" &&
					recognizeIssueRecipe(pins, user.ID, recipes, string(data)) == "issue" {
					return plan, &CommandError{
						Code: "MANAGED_CALLER_REQUIRES_EXPLICIT_UPGRADE", Exit: 2}
				}
				return plan, fmt.Errorf("EXISTING_FILE_CONFLICT: %s", path)
			}
			continue
		}
		plan.Files = append(plan.Files, Change{Path: path, Content: files[path]})
	}
	for _, label := range []string{"scope", "shaped", "delivered"} {
		var observed struct {
			Name string `json:"name"`
		}
		err := api.Call("GET", "repos/"+name+"/labels/"+label, nil, &observed)
		if err != nil && err.Error() != "NOT_FOUND" {
			return plan, err
		}
		if err != nil {
			plan.Labels = append(plan.Labels, label)
		} else if observed.Name != label {
			return plan, errors.New("LABEL_READBACK_MISMATCH")
		}
	}
	return plan, nil
}

func initFileMatches(path, expected string, actual []byte, pins Producers,
	native bool) bool {
	// Artifact locators are retrieval metadata, not installed source. Preserve
	// actual Core/Action integrity and caller bindings while allowing new fetches.
	return string(actual) == expected || native && path == ".nibfin/producer-pins.json" &&
		installedProducerSourcesMatch(actual, pins)
}

type InitResult struct {
	Status          string               `json:"status"`
	Code            string               `json:"code"`
	PR              string               `json:"pull_request,omitempty"`
	Wake            string               `json:"shared_wake,omitempty"`
	HostAdoption    string               `json:"shared_host_adoption,omitempty"`
	CoreDeployment  string               `json:"shared_core_deployment,omitempty"`
	RunnerPackages  []string             `json:"shared_runner_packages,omitempty"`
	RunnerSlots     []sharedRegistration `json:"shared_runners,omitempty"`
	ServiceTemplate string               `json:"shared_service_template,omitempty"`
	WakeDestination string               `json:"shared_wake_destination,omitempty"`
}

type installResultEncoder interface{ Encode(any) error }

func applyInit(api API, plan InitPlan) (InitResult, error) {
	return applyInitWithPins(api, plan, Pins())
}

func applyInitWithPins(api API, plan InitPlan, pins Producers) (InitResult, error) {
	result := InitResult{Status: "pending", Code: "INSTALLATION_INCOMPLETE"}
	execution := ""
	if plan.Execution != nil {
		execution = plan.Execution.Repository
	}
	current, err := planInitRecipeWithPins(api, plan.Repository.FullName, execution,
		initIssueRecipe(plan), pins)
	if err != nil {
		return result, err
	}
	if current.Head != plan.Head || len(current.Files) != len(plan.Files) ||
		current.Member.ID != plan.Member.ID ||
		!sameSharedExecution(current.Execution, plan.Execution) {
		return result, errors.New("INSTALLATION_PLAN_CHANGED")
	}
	for index, file := range current.Files {
		if file != plan.Files[index] {
			return result, errors.New("INSTALLATION_PLAN_CHANGED")
		}
	}
	base := "repos/" + plan.Repository.FullName
	if len(plan.Files) > 0 {
		branch, err := createInstallBranch(api, base, plan)
		if err != nil {
			return result, err
		}
		pullURL, err := createInstallPR(api, plan, branch)
		if err != nil {
			return result, err
		}
		result.PR = pullURL
	}
	if err := ensureInitLabels(api, base, plan.Labels); err != nil {
		return result, err
	}
	if len(plan.Files) == 0 {
		if plan.Execution != nil {
			result.Code = "SHARED_HOST_ADOPTION_REQUIRED"
			result.Wake = sharedWakeConfiguration(api, plan.Repository.FullName, *plan.Execution)
		} else {
			result.Status, result.Code = "completed", "INITIALIZATION_READ_BACK"
		}
	} else {
		result.Code = "INSTALLATION_PR_REQUIRES_REVIEW_AND_MERGE"
	}
	return result, nil
}

func ensureInitLabels(api API, base string, labels []string) error {
	for _, label := range labels {
		body := map[string]string{"name": label, "color": "ededed",
			"description": "Nibfin lifecycle; only authorized Core may advance work"}
		writeErr := api.Call("POST", base+"/labels", body, nil)
		var observed struct {
			Name string `json:"name"`
		}
		if err := api.Call("GET", base+"/labels/"+label, nil, &observed); err != nil ||
			observed.Name != label {
			return errors.New("LABEL_RESULT_UNKNOWN_READ_BACK_BEFORE_RETRY")
		}
		_ = writeErr // Read-back, not the initial response, establishes the effect.
	}
	return nil
}

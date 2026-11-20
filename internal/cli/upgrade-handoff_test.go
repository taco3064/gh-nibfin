package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const handoffTestID = "6803af57-60e3-471b-9106-fd004081691c"

func handoffTestRecipe() upgradeHandoffRecipe {
	return upgradeHandoffRecipe{
		execution:  SharedExecution{"example/project", 10},
		noticePath: ".github/workflows/nibfin-upgrade-notify.yml",
		execPath:   ".github/workflows/nibfin-upgrade-execute.yml",
		variable:   "NIBFIN_UPGRADE_DESTINATION",
		secret:     "NIBFIN_UPGRADE_DISPATCH_TOKEN",
		prepare:    "upgrade-prepare",
		bind:       "upgrade-bind",
		observe:    "upgrade-observe",
		complete:   "upgrade-complete",
		notice:     "name: notification\n",
		executor:   "name: executor for MEMBER_ID\n",
	}
}

func TestUpgradeHandoffPlansUseMaintainedCoreRecipes(t *testing.T) {
	api := upgradeFixture(t, false, true)
	recipe := handoffTestRecipe()
	base := "repos/" + officialCore + "/contents/"
	ref := "?ref=" + api.target.Core.Commit
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "bridge": maintenanceBridge, "proposalBranch": upgradeBranchName,
		"recipes": map[string]string{"source": "src/upgrade-continuation-recipes.json",
			"notification": recipe.noticePath, "execution": recipe.execPath},
		"notification": map[string]string{"destinationVariable": recipe.variable,
			"dispatchSecret": recipe.secret},
		"administrator": map[string]string{"input": "private-json-stdin",
			"prepare": recipe.prepare, "bind": recipe.bind, "observe": recipe.observe,
			"complete": recipe.complete},
	})
	if err != nil {
		t.Fatal(err)
	}
	api.data[base+upgradeHandoffContract+ref] = encoded(manifest)
	api.data[base+"src/upgrade-continuation-recipes.json"+ref] = encoded([]byte(
		`{"notification":["name: notification",""],` +
			`"execution":["name: executor for MEMBER_ID",""]}`))
	loaded, err := readUpgradeHandoffRecipe(api, api.target.Core, recipe.execution)
	if err != nil || loaded == nil || *loaded != recipe {
		t.Fatal("selected Core contract was not decoded", loaded, err)
	}
	target := upgradeSource{Producers: api.target, handoff: loaded}
	plan, err := planNativeUpgradeTarget(api, recipe.execution.Repository, "latest", target)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]string{recipe.noticePath: recipe.notice,
		recipe.execPath: strings.ReplaceAll(recipe.executor, "MEMBER_ID", "42")}
	for _, change := range plan.Changes {
		if want, ok := wanted[change.Path]; ok {
			if change.Kind != "added" || change.Content != want {
				t.Fatal("wrong maintained caller", change)
			}
			delete(wanted, change.Path)
		}
	}
	if len(wanted) != 0 || len(api.writes) != 0 {
		t.Fatal("read-only preview did not compose both callers", wanted, api.writes)
	}
}

func handoffTestProgress(recipe upgradeHandoffRecipe) managedUpgradeProgress {
	return managedUpgradeProgress{OperationID: handoffTestID,
		Execution: recipe.execution.Repository, ExecutionID: recipe.execution.RepositoryID,
		Handoff: "pending", Installation: "pending", Readiness: "pending",
		Continuation: "pending"}
}

func handoffTestResponse(progress managedUpgradeProgress) ([]byte, error) {
	return json.Marshal(map[string]any{
		"schemaVersion": 1, "status": "complete", "upgrade": progress})
}

func TestUpgradeHandoffWaitsForBoundDraftAndCompletesNoChangePeer(t *testing.T) {
	recipe := handoffTestRecipe()
	proposal := managedUpgradeProposal{Repository: recipe.execution.Repository,
		RepositoryID: 10, Pull: 4, ID: 99, Node: "PR_test", Handoff: "review-pending",
		URL: "https://github.com/example/project/pull/4"}
	primary := upgradePlan{Repository: Repository{FullName: proposal.Repository, ID: 10},
		Changes: []upgradeChange{{recipe.execPath, "added", recipe.executor}}}
	peer := upgradePlan{Repository: Repository{FullName: "example/peer", ID: 20}}
	var checks []string
	progress := handoffTestProgress(recipe)
	ports := upgradeHandoffPorts{
		bridge: func(input any) ([]byte, error) {
			switch payload := input.(type) {
			case upgradeHandoffInput:
				checks = append(checks, "prepare")
				if !reflect.DeepEqual(payload.Repositories, []int64{10, 20}) ||
					payload.Activation == nil || payload.Activation.RunID != 3 {
					t.Fatal("wrong original intent", payload)
				}
			case map[string]any:
				if payload["operationId"] != handoffTestID {
					t.Fatal("operation changed", payload)
				}
				checks = append(checks, payload["kind"].(string))
				if payload["kind"] == recipe.bind {
					progress.Proposals = []managedUpgradeProposal{proposal}
					progress.Handoff = "review-pending"
				} else if payload["kind"] != recipe.complete {
					t.Fatal("unexpected bridge request", payload)
				}
			}
			return handoffTestResponse(progress)
		},
		publish: func(plan upgradePlan) (InitResult, error) {
			checks = append(checks, plan.Repository.FullName)
			if plan.Repository.ID == 10 {
				return InitResult{PR: proposal.URL, Status: "pending"}, nil
			}
			return InitResult{Status: "no_change"}, nil
		},
		ready: func(_ upgradePlan, observed managedUpgradeProposal) error {
			if observed != proposal {
				t.Fatal("draft was not positively bound", observed)
			}
			checks = append(checks, "ready")
			return nil
		},
	}
	activation := &nativeSourceTarget{RunID: 3, ArtifactID: 7}
	result, err := runUpgradeHandoff([]upgradePlan{primary, peer}, false,
		recipe, SourcePin{}, activation, ports)
	if err != nil || !reflect.DeepEqual(checks, []string{"prepare", proposal.Repository,
		recipe.bind, "ready", peer.Repository.FullName, recipe.complete}) {
		t.Fatal("unexpected managed upgrade sequence", checks, err)
	}
	var out bytes.Buffer
	err = reportUpgradeHandoff(&out, result, nil)
	if err == nil || !strings.Contains(out.String(), proposal.URL) ||
		!strings.Contains(out.String(), "Merge the execution repository proposal first") ||
		strings.Contains(out.String(), "will continue after merge") {
		t.Fatal("incorrect bootstrap review report", out.String(), err)
	}
}

func TestUpgradeHandoffUnknownBindingDoesNotReleaseDraft(t *testing.T) {
	recipe := handoffTestRecipe()
	repo := Repository{FullName: recipe.execution.Repository, ID: 10}
	plan := upgradePlan{Repository: repo,
		Changes: []upgradeChange{{recipe.noticePath, "added", recipe.notice}}}
	progress := handoffTestProgress(recipe)
	ports := upgradeHandoffPorts{
		bridge: func(req any) ([]byte, error) {
			if _, ok := req.(upgradeHandoffInput); ok {
				return handoffTestResponse(progress)
			}
			return nil, errors.New("RESPONSE_UNAVAILABLE")
		},
		publish: func(upgradePlan) (InitResult, error) {
			return InitResult{PR: "https://github.com/example/project/pull/4"}, nil
		},
		ready: func(upgradePlan, managedUpgradeProposal) error {
			t.Fatal("unknown binding released the draft")
			return nil
		},
	}
	outcome, err := runUpgradeHandoff([]upgradePlan{plan}, true, recipe,
		SourcePin{}, nil, ports)
	if err == nil || len(outcome.Migration) != 1 ||
		outcome.Migration[0].Proposal.Code != "UPGRADE_PR_CREATED_OR_REUSED_HANDOFF_PENDING" {
		t.Fatal("lost binding must preserve native proposal recovery", outcome, err)
	}
}

func TestUpgradeHandoffReusesNativeDraftAndReadsBackLostReadyResponse(t *testing.T) {
	api := upgradeFixture(t, true, true)
	recipe := handoffTestRecipe()
	api.data[pullListPath(upgradeBranchName, "open")] = []upgradePull{}
	target := upgradeSource{Producers: api.target, handoff: &recipe}
	plan, err := planNativeUpgradeTarget(api, recipe.execution.Repository, "latest", target)
	if err != nil {
		t.Fatal(err)
	}
	first, err := applyUpgrade(api, plan, "latest")
	if err != nil {
		t.Fatal(err)
	}
	pull := api.data[upgradeRepo+"/pulls/9"].(upgradePull)
	if !pull.Draft || first.PR != pull.URL {
		t.Fatal("expected read-back draft", first)
	}
	bound := managedUpgradeProposal{Repository: recipe.execution.Repository,
		RepositoryID: recipe.execution.RepositoryID, Pull: pull.Number, Node: pull.NodeID,
		URL: pull.URL}
	api.lost = "graphql"
	if err := readyBoundUpgradeProposal(api, plan, "latest", bound); err != nil {
		t.Fatal("lost ready response did not observe effect", err)
	}
	if err := readyBoundUpgradeProposal(api, plan, "latest", bound); err != nil {
		t.Fatal("confirmed ready was not reused", err)
	}
	second, err := applyUpgrade(api, plan, "latest")
	if err != nil || second.PR != first.PR || countWrite(api.writes, "POST graphql") != 1 ||
		countWrite(api.writes, "POST "+upgradeRepo+"/pulls") != 1 {
		t.Fatal("confirmed proposal was repeated", second, err, api.writes)
	}
}

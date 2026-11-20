package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	registerHandoffFixture(t, api, api.target.Core, recipe)
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

func registerHandoffFixture(t *testing.T, api *upgradeAPI, pin SourcePin,
	recipe upgradeHandoffRecipe) {
	t.Helper()
	base := "repos/" + officialCore + "/contents/"
	ref := "?ref=" + pin.Commit
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
}

func TestUpgradeHandoffPreviewMatchesSelectedAndBootstrapApply(t *testing.T) {
	for _, installed := range []bool{true, false} {
		api, host, migration, _ := nativeConsumersFixture(t)
		adopted, _ := migration.bindings()
		const selected = "taco3064/nibfin-e2e"
		const unrelated = "taco3064/nibfin-act"
		api.apis[unrelated].denyRead = "repos/" + unrelated + "/git/ref/heads/main"
		recipe := handoffTestRecipe()
		recipe.execution = host.SharedExecution
		target := upgradeSource{Producers: api.first.old, Kind: "installed-current",
			Execution: &host.SharedExecution, maintained: true, handoff: &recipe}
		registerHandoffFixture(t, api.first.upgradeAPI, adopted.Pin, recipe)
		if installed {
			base := "repos/" + host.Repository
			var ref reference
			if err := api.Call("GET", base+"/git/ref/heads/main", nil, &ref); err != nil {
				t.Fatal(err)
			}
			api.first.data[base+"/actions/workflows/nibfin-upgrade-execute.yml"] =
				map[string]any{"id": 8, "path": recipe.execPath, "state": "active"}
			asset := base + "/contents/" + recipe.execPath + "?ref=" + ref.Object.SHA
			data := []byte(strings.ReplaceAll(recipe.executor, "MEMBER_ID", "42"))
			api.first.data[asset] = encoded(data)
		}
		var applied []upgradePlan
		app := App{API: api, Process: upgradeNoProcess{t}, upgrade: &upgradeCommandOperations{
			discover: func() (*sharedHost, error) { return &host, nil },
			verify:   func(sharedHost) error { return nil },
			selectSource: func(sharedHost, string) (upgradeSource, error) {
				return target, nil
			},
			previewHandoff: func(host sharedHost, name, version string, source upgradeSource) (
				[]upgradePlan, bool, error) {
				return upgradeHandoffPlans(api, host, name, version, source, adopted)
			},
			applyHandoff: func(host sharedHost, name, version string, _ io.Writer) error {
				var err error
				applied, _, err = upgradeHandoffPlans(api, host, name, version, target, adopted)
				return err
			},
		}}
		var out bytes.Buffer
		err := app.runUpgrade([]string{"--repo", selected, "--apply"}, &out)
		if err != nil {
			t.Fatal("unrelated consumer blocked managed apply", err)
		}
		var preview struct {
			Plan  upgradePlan   `json:"plan"`
			Plans []upgradePlan `json:"managed_plans"`
			First string        `json:"execution_merge_first"`
		}
		if err := json.NewDecoder(&out).Decode(&preview); err != nil {
			t.Fatal(err)
		}
		wanted := []string{selected}
		if !installed {
			wanted = []string{host.Repository, selected}
			if preview.First != host.Repository {
				t.Fatal("first installation did not expose merge order", preview.First)
			}
		}
		if preview.Plan.Repository.FullName != selected || len(preview.Plans) != len(wanted) ||
			len(applied) != len(wanted) {
			t.Fatal("unexpected public preview/apply scope", preview, len(applied))
		}
		for index, repo := range wanted {
			if preview.Plans[index].Repository.FullName != repo ||
				preview.Plans[index].Repository.ID != applied[index].Repository.ID ||
				!reflect.DeepEqual(preview.Plans[index].Changes, applied[index].Changes) {
				t.Fatal("preview differed from apply planning", repo)
			}
		}
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

func TestUpgradeHandoffDoesNotAdoptUnboundExistingDispatchSecret(t *testing.T) {
	api := upgradeFixture(t, false, true)
	recipe := handoffTestRecipe()
	plan := fixtureUpgradePlan(t, api)
	user := User{ID: plan.MemberID, Login: "maintainer", Type: "User"}
	api.data["user"] = user
	installation := InitPlan{NativeManaged: true, IssueRecipe: "issue",
		Repository: plan.Repository, Member: user, Execution: &recipe.execution}
	base := upgradeRepo + "/actions/"
	api.data[base+"variables/"+recipe.variable] = actionsVariable{
		Name: recipe.variable, Value: recipe.execution.Repository}
	secret := nativeSecretMetadata{Name: recipe.secret, CreatedAt: "created",
		UpdatedAt: "updated"}
	api.data[base+"secrets/"+recipe.secret] = secret
	save := func() error { t.Fatal("unexpected write to intent"); return nil }
	err := prepareUpgradeRoute(api, installation, plan, "latest", recipe,
		&upgradeRouteSetup{}, save)
	if err == nil || err.Error() != "UPGRADE_NOTIFICATION_SECRET_OWNERSHIP_UNVERIFIED" ||
		len(api.writes) != 0 {
		t.Fatal("unowned credential was treated as managed", err, api.writes)
	}
	confirmed := upgradeRouteSetup{SecretState: "confirmed", Secret: secret}
	err = prepareUpgradeRoute(api, installation, plan, "latest", recipe, &confirmed, save)
	if err != nil || len(api.writes) != 0 {
		t.Fatal("owned credential was not reused", err, api.writes)
	}
}

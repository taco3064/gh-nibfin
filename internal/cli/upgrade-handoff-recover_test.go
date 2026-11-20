package cli

import (
	"errors"
	"net/url"
	"path"
	"testing"
)

type upgradeReplayAPI struct {
	*fakeAPI
	onDispatch func()
	dispatch   string
	denyRead   string
}

func (api upgradeReplayAPI) Call(method, endpoint string, input, output any) error {
	if method == "GET" && endpoint == api.denyRead {
		return errors.New("RATE_LIMITED")
	}
	if method == "POST" && endpoint == api.dispatch {
		api.writes = append(api.writes, endpoint)
		api.onDispatch()
		return errors.New("LOST_RESPONSE")
	}
	return api.fakeAPI.Call(method, endpoint, input, output)
}

func TestUpgradeHandoffRecoversExactNativeNotificationAndReusesIt(t *testing.T) {
	recipe := handoffTestRecipe()
	recipe.notice = "name: Notification\nrun-name: Nibfin upgrade notification · PR 9\n"
	recipe.executor = "name: Execute\nrun-name: Nibfin upgrade · repository 10 · PR 9\n"
	fixture := upgradeFixture(t, false, true)
	plan := fixtureUpgradePlan(t, fixture)
	fixture.data["user"] = User{ID: plan.MemberID, Login: "maintainer", Type: "User"}
	fixture.data[upgradeRepo+"/contents/"+recipe.noticePath+"?ref="+plan.Base] =
		encoded([]byte(recipe.notice))
	plan.entries[recipe.noticePath] = upgradeEntry{Path: recipe.noticePath,
		Mode: "100644", Type: "blob", SHA: blobHash([]byte(recipe.notice))}
	known := managedUpgradeProposal{Repository: plan.Repository.FullName, RepositoryID: 10,
		ID: 80, Node: "PR_known", Pull: 9, Merged: true,
		URL: "https://github.com/example/project/pull/9"}
	merged := "2026-11-20T10:01:11Z"
	fixture.data[upgradeRepo+"/pulls/9"] = map[string]any{
		"id": known.ID, "node_id": known.Node, "number": known.Pull, "merged": true,
		"merged_at": merged, "state": "closed", "html_url": known.URL,
		"user": User{ID: plan.MemberID, Login: "maintainer", Type: "User"},
		"head": map[string]any{"ref": upgradeBranchName, "repo": plan.Repository},
		"base": map[string]any{"ref": plan.Repository.DefaultBranch, "repo": plan.Repository},
	}
	query := url.Values{"created": {">=" + merged}, "per_page": {"100"}, "page": {"1"}}
	root := upgradeRepo + "/actions/workflows/"
	notice := root + path.Base(recipe.noticePath)
	receiver := root + path.Base(recipe.execPath)
	noticeRuns := notice + "/runs?" + query.Encode()
	receiverRuns := receiver + "/runs?" + query.Encode()
	for index, item := range []string{recipe.noticePath, recipe.execPath} {
		base := root + path.Base(item)
		fixture.data[base] = map[string]any{"id": index + 1, "path": item, "state": "active"}
		fixture.data[base+"/runs?"+query.Encode()] = map[string]any{
			"total_count": 0, "workflow_runs": []upgradeWorkflowRun{}}
	}
	run := upgradeWorkflowRun{ID: 55, WorkflowID: 1,
		Title: "Nibfin upgrade notification · PR 9", Event: "workflow_dispatch",
		HeadBranch: plan.Repository.DefaultBranch, Status: "queued", Attempt: 1,
		Repository: plan.Repository, HeadRepo: plan.Repository,
		URL: "https://github.com/example/project/actions/runs/55"}
	api := upgradeReplayAPI{fakeAPI: fixture.fakeAPI, dispatch: notice + "/dispatches",
		denyRead: receiverRuns, onDispatch: func() {
			fixture.data[noticeRuns] = map[string]any{
				"total_count": 1, "workflow_runs": []upgradeWorkflowRun{run}}
		}}
	if _, err := recoverMergedUpgrade(api, plan, known, recipe); err == nil ||
		len(api.writes) != 0 {
		t.Fatal("failed native read allowed dispatch", err, api.writes)
	}
	api.denyRead = ""
	for range 2 {
		result, err := recoverMergedUpgrade(api, plan, known, recipe)
		if err != nil || result.RunURL != run.URL || result.State != "queued" {
			t.Fatal("exact native dispatch was not observed", result, err)
		}
	}
	if len(api.writes) != 1 || api.writes[0] != api.dispatch {
		t.Fatal("confirmed notification dispatch repeated", api.writes)
	}
}

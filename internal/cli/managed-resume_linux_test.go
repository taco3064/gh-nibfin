package cli

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func managedResumeFixture(t *testing.T) (*fakeAPI, InitPlan, func()) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	api := initFixture()
	pin := Pins().Core
	source := "repos/" + pin.Repository + "/contents/"
	api.data[source+managedHostEntry+"?ref="+pin.Commit] = encoded([]byte("core entry"))
	api.data[source+sharedRunnerWorkflow+"?ref="+pin.Commit] = encoded([]byte("runner"))
	api.data[source+"src/shared-runner-contract.json?ref="+pin.Commit] = encoded([]byte(
		"{\"schemaVersion\":1,\"destinationVariable\":\"NIBFIN_SHARED_RUNNER_DESTINATION\"," +
			"\"workflowPath\":\".github/workflows/shared-runner.yml\"}"))
	name := "example/project"
	path := "repos/" + name
	project := api.data[path].(Repository)
	project.Owner.Login = "example"
	api.data[path] = project
	for _, label := range []string{"scope", "shaped", "delivered"} {
		api.data[path+"/labels/"+label] = map[string]string{"name": label}
	}
	plan, err := planInitWithExecution(api, name, name)
	if err != nil {
		t.Fatal(err)
	}
	return api, plan, managedResumePull(api, plan)
}

func managedResumePull(api *fakeAPI, plan InitPlan) func() {
	name := plan.Repository.FullName
	path := "repos/" + name
	branch := "nibfin/init-" + plan.Head[:12]
	head, merged := strings.Repeat("b", 40), strings.Repeat("c", 40)
	query := path + "/pulls?state=all&per_page=2&head=" + url.QueryEscape("example:"+branch)
	api.data[query] = []map[string]int{{"number": 7}}
	pr := map[string]any{
		"number": 7, "html_url": "https://github.com/" + name + "/pull/7",
		"state": "open", "merged": false,
		"head": map[string]any{"ref": branch, "sha": head,
			"repo": map[string]int64{"id": plan.Repository.ID}},
		"base": map[string]any{"ref": "main",
			"repo": map[string]int64{"id": plan.Repository.ID}},
	}
	api.data[path+"/pulls/7"] = pr
	for _, file := range plan.Files {
		api.data[path+"/contents/"+file.Path+"?ref="+head] = encoded([]byte(file.Content))
	}
	merge := func() {
		pr["state"], pr["merged"] = "closed", true
		ref := reference{Ref: "refs/heads/main"}
		ref.Object.SHA = merged
		api.data[path+"/git/ref/heads/main"] = ref
		api.data[path+"/branches/main"] = map[string]any{"name": "main", "protected": true,
			"commit": map[string]string{"sha": merged}}
		for _, file := range plan.Files {
			api.data[path+"/contents/"+file.Path+"?ref="+merged] = encoded([]byte(file.Content))
		}
		delete(api.data, query)
	}
	return merge
}

func TestManagedResumeUsesDurableIntentAndControlledHost(t *testing.T) {
	api, plan, merge := managedResumeFixture(t)
	app := App{API: api}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	token := []byte("synthetic-fixture")
	var pending *CommandError
	err := app.resumeManagedInit(ctx, plan, token, &out, managedHostOperations{})
	if !errors.As(err, &pending) || pending.Code != "MANAGED_INSTALL_AWAITING_PR_MERGE" {
		t.Fatalf("expected an interrupted PR wait: %v", err)
	}
	merge()
	registrations := []sharedRegistration{
		{ID: 25, Name: "slot-1", Status: "offline"},
		{ID: 26, Name: "slot-2", Status: "offline"},
	}
	host, err := decodeSharedHost([]byte(`{
		"schemaVersion":1, "repositoryId":10, "repository":"example/project", "memberId":7,
		"adoptedRepositoryIds":[10],
		"runners":[{"id":25,"name":"slot-1"},{"id":26,"name":"slot-2"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	updateNative := func(first, second string) {
		labels := []struct {
			Name string `json:"name"`
		}{{"nibfin-user-7"}, {"nibfin-shared"}}
		api.data["repos/example/project/actions/runners?per_page=100&page=1"] = map[string]any{
			"total_count": 2, "runners": []RunnerInfo{
				{ID: 25, Name: "slot-1", OS: "Linux", Status: first,
					Labels: labels},
				{ID: 26, Name: "slot-2", OS: "Linux", Status: second,
					Labels: labels},
			},
		}
	}
	var adopted *sharedHost
	var prepares, registrationsMade, starts int
	ops := managedHostOperations{
		discover: func() (*sharedHost, error) { return adopted, nil },
		prepare:  func(InitPlan) error { prepares++; return nil },
		credentials: func(member int64, value []byte) error {
			if member != 7 || !bytes.Equal(value, token) {
				return errors.New("fixture credential binding changed")
			}
			return nil
		},
		stage: func(InitPlan) ([]string, error) { return []string{"one", "two"}, nil },
		register: func(InitPlan, []string) ([]sharedRegistration, error) {
			registrationsMade++
			return registrations, nil
		},
		adopt: func(InitPlan, []sharedRegistration) (*sharedHost, error) {
			adopted = host
			return adopted, nil
		},
		staged: func(_ InitPlan, observed []sharedRegistration) ([]string, error) {
			if observed[0].ID != 25 || observed[0].Status != "online" ||
				observed[1].ID != 26 || observed[1].Status != "offline" {
				t.Fatalf("expected independent partial start readback: %+v", observed)
			}
			return nil, nil
		},
		start: func(paths []string) error {
			starts++
			if starts == 1 {
				updateNative("online", "offline")
				return errors.New("fixture interrupted after slot one")
			}
			if len(paths) != 0 {
				t.Fatal("resume must use existing slots")
			}
			updateNative("online", "online")
			return nil
		},
		cleanup: func(_ InitPlan, slots []sharedRegistration) error {
			if !sharedRunnersOnline(slots) {
				t.Fatal("cleanup must follow online readback")
			}
			return nil
		},
	}
	ready := context.Background()
	if err := app.resumeManagedInit(ready, plan, token, &out, ops); err == nil {
		t.Fatal("the first service start must be interrupted")
	}
	if err := app.resumeManagedInit(ready, plan, token, &out, ops); err != nil {
		t.Fatal(err)
	}
	peer := plan.Repository
	peer.ID, peer.FullName = 20, "example/peer"
	base := "repos/" + peer.FullName
	api.data[base] = peer
	api.data[base+"/git/ref/heads/main"] = reference{Ref: "refs/heads/main", Object: struct {
		SHA string `json:"sha"`
	}{SHA: strings.Repeat("d", 40)}}
	api.data["repos/example/project/actions/workflows/shared-runner.yml"] = map[string]any{
		"id": 40, "path": sharedRunnerWorkflow, "state": "active",
	}
	for _, label := range []string{"scope", "shaped", "delivered"} {
		api.data[base+"/labels/"+label] = map[string]string{"name": label}
	}
	peerPlan, err := planInitWithExecution(api, peer.FullName, plan.Repository.FullName)
	if err != nil {
		t.Fatal(err)
	}
	mergePeer := managedResumePull(api, peerPlan)
	err = app.resumeManagedInit(ctx, peerPlan, token, &out, managedHostOperations{})
	if !errors.As(err, &pending) || pending.Code != "MANAGED_INSTALL_AWAITING_PR_MERGE" {
		t.Fatalf("expected an interrupted peer PR wait: %v", err)
	}
	mergePeer()
	adoptions := 0
	ops.adoptPeer = func(original sharedHost, plan InitPlan) error {
		if !original.samePool(host) || plan.Repository.ID != 20 {
			t.Fatal("peer must retain both original slots and its own repository identity")
		}
		adoptions++
		host.AdoptedRepositoryIDs = append(host.AdoptedRepositoryIDs, 20)
		return nil
	}
	for range 2 {
		if err := app.resumeManagedInit(ready, peerPlan, token, &out, ops); err != nil {
			t.Fatal(err)
		}
	}
	if starts != 2 || prepares != 1 || registrationsMade != 1 || len(api.writes) != 0 ||
		adoptions != 1 ||
		!strings.Contains(out.String(), "MANAGED_INSTALLATION_INACTIVE") ||
		bytes.Contains(out.Bytes(), token) {
		t.Fatalf("unexpected resume effects or handoff: %d, %d, %d",
			starts, prepares, registrationsMade)
	}
}

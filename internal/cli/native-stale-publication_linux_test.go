package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type nativeStalePublicationAPI struct {
	*nativeWakeAPI
	installer *installingAPI
	events    []string
}

func (a *nativeStalePublicationAPI) Call(method, path string, input, output any) error {
	a.events = append(a.events, method+" "+path)
	base := "repos/example/project"
	if method == "GET" && strings.HasPrefix(path, base+"/pulls?state=") {
		pulls := []installPR{}
		if pull, ok := a.data[base+"/pulls/8"].(installPR); ok {
			pulls = append(pulls, pull)
		}
		return assign(output, pulls)
	}
	if method == "GET" || strings.Contains(path, "/actions/variables") {
		return a.nativeWakeAPI.Call(method, path, input, output)
	}
	err := a.installer.Call(method, path, input, output)
	if path == base+"/pulls" {
		pull := a.data[base+"/pulls/8"].(installPR)
		pull.Head.Repo.ID, pull.Base.Repo.ID = 10, 10
		a.data[base+"/pulls/8"] = pull
	}
	return err
}

func TestNativeStaleMissingPRReentersOriginalPublication(t *testing.T) {
	base, plan, _ := managedResumeFixture(t)
	plan.NativeManaged = true
	old := Pins()
	old.Artifact.ID++
	original := historicalNativePlan(plan, old)
	native := &nativeWakeAPI{fakeAPI: base}
	state := configureNativeFixture(native, plan)
	saveNativeFixtureIntent(t, managedInstallIntent{
		Plan: original, Pins: old, Native: state})
	base.data["repos/example/project/git/commits/"+plan.Head] = map[string]any{
		"tree": map[string]string{"sha": strings.Repeat("d", 40)}}
	api := &nativeStalePublicationAPI{nativeWakeAPI: native,
		installer: &installingAPI{fakeAPI: base, plan: original, lostPR: true}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	app := App{API: api}
	for attempt := range 2 {
		err := app.resumeManagedInit(ctx, plan, nil, &out, managedHostOperations{})
		var pending *CommandError
		if !errors.As(err, &pending) || pending.Code != "MANAGED_INSTALL_AWAITING_PR_MERGE" {
			t.Fatal("original authorized publication did not resume", err)
		}
		if len(api.writes) != 4 || api.secretWrites != 0 {
			t.Fatal("source/credential or PR writes repeated", attempt, api.writes)
		}
	}
	firstWrite := -1
	readOriginal := false
	for index, event := range api.events {
		if strings.HasPrefix(event, "GET repos/example/project/pulls?state=all") {
			readOriginal = true
		}
		if strings.HasPrefix(event, "POST ") {
			firstWrite = index
			break
		}
	}
	if !readOriginal || firstWrite < 0 {
		t.Fatal("publication did not observe the original provider result first")
	}
	for _, file := range original.Files {
		data, _, err := content(api, plan.Repository.FullName, file.Path,
			strings.Repeat("c", 40))
		if err != nil || string(data) != file.Content {
			t.Fatal("new default metadata replaced the original publication target", file.Path)
		}
	}
}

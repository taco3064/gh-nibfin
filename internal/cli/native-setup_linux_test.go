package cli

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type nativeSetupAPI struct {
	*fakeAPI
	events       []string
	lostVariable bool
}

func (a *nativeSetupAPI) Call(method, path string, input, output any) error {
	a.events = append(a.events, method+" "+path)
	if method == "GET" {
		return a.fakeAPI.Call(method, path, input, output)
	}
	variable, ok := input.(actionsVariable)
	if !ok {
		return errors.New("unexpected fixture write")
	}
	if method == "POST" {
		path += "/" + variable.Name
	}
	a.data[path] = variable
	if a.lostVariable {
		return errors.New("fixture response lost")
	}
	return nil
}

type fixtureSecretPublisher struct {
	api   *nativeSetupAPI
	calls int
	lost  bool
}

func (p *fixtureSecretPublisher) PublishWakeSecret(repo string) error {
	p.calls++
	p.api.events = append(p.api.events, "secret-private-input")
	p.api.data["repos/"+repo+"/actions/secrets/"+nativeWakeSecret] =
		nativeSecretMetadata{Name: nativeWakeSecret, UpdatedAt: "observed-write"}
	if p.lost {
		return errors.New("fixture lost response")
	}
	return nil
}

func nativeSetupFixture() (*nativeSetupAPI, *fixtureSecretPublisher,
	nativeSetupRequest) {
	api := &nativeSetupAPI{fakeAPI: initFixture()}
	api.data["repos/example/project/actions/permissions"] = map[string]bool{
		"enabled": true,
	}
	request := nativeSetupRequest{
		Account:     api.data["user"].(User),
		Target:      api.data["repos/example/project"].(Repository),
		Destination: SharedExecution{Repository: "example/project", RepositoryID: 10},
	}
	return api, &fixtureSecretPublisher{api: api}, request
}

func TestNativeSetupConfirmedWritesAndRetry(t *testing.T) {
	api, publisher, request := nativeSetupFixture()
	api.lostVariable = true
	observation := nativeCredentialObservation{}
	var saved []nativeCredentialObservation
	save := func() error { saved = append(saved, observation); return nil }
	for range 2 {
		if err := prepareNativeSetup(api, publisher, request, &observation, save); err != nil {
			t.Fatal(err)
		}
	}
	if publisher.calls != 1 || len(saved) != 2 || saved[0].State != "attempted" ||
		saved[1].State != "confirmed" {
		t.Fatalf("confirmed writes must be reused: %+v, %d", saved, publisher.calls)
	}
	if got := sharedWakeConfiguration(api, request.Target.FullName,
		request.Destination); got != "configuration_required" {
		t.Fatalf("preparation must leave activation disabled: %s", got)
	}
}

func TestNativeSetupUnknownSecretEffectNeverReplaced(t *testing.T) {
	api, publisher, request := nativeSetupFixture()
	publisher.lost = true
	observation := nativeCredentialObservation{}
	for range 2 {
		err := prepareNativeSetup(api, publisher, request, &observation,
			func() error { return nil })
		var pending *CommandError
		if !errors.As(err, &pending) || pending.Exit != 2 {
			t.Fatalf("unknown write must remain pending: %v", err)
		}
	}
	if publisher.calls != 1 || observation.State != "attempted" {
		t.Fatal("metadata cannot confirm or justify replacing an unknown credential")
	}
}

func TestNativeSetupRefusesConflictsAndChangedIdentities(t *testing.T) {
	for _, scenario := range []string{"secret", "destination", "account", "permission"} {
		t.Run(scenario, func(t *testing.T) {
			api, publisher, request := nativeSetupFixture()
			base := "repos/example/project/actions/"
			switch scenario {
			case "secret":
				api.data[base+"secrets/"+nativeWakeSecret] = nativeSecretMetadata{
					Name: nativeWakeSecret, UpdatedAt: "unrelated",
				}
			case "destination":
				api.data[base+"variables/"+sharedDestinationVariable] = actionsVariable{
					Name: sharedDestinationVariable, Value: "unrelated",
				}
			case "account":
				request.Account.ID++
			case "permission":
				api.data[base+"permissions"] = map[string]bool{"enabled": false}
			}
			observation := nativeCredentialObservation{}
			if err := prepareNativeSetup(api, publisher, request, &observation,
				func() error { return nil }); err == nil || publisher.calls != 0 {
				t.Fatal("unsafe credential setup was allowed")
			}
		})
	}
}

func TestNativeSetupIntentContainsNoCredential(t *testing.T) {
	_, _, request := nativeSetupFixture()
	observation := nativeCredentialObservation{State: "attempted"}
	data, err := json.Marshal(observation)
	if err != nil || string(data) != `{"state":"attempted"}` {
		t.Fatal("intent must contain only operation observations")
	}
	api, publisher, _ := nativeSetupFixture()
	before := observation
	err = prepareNativeSetup(api, publisher, request, &observation,
		func() error { return errors.New("fixture storage failed") })
	if err == nil || publisher.calls != 0 || !reflect.DeepEqual(observation, before) {
		t.Fatal("durable attempted observation must precede secret transport")
	}
}

func TestNativeSecretPermissionDiagnosticsStayPrivate(t *testing.T) {
	for _, message := range []string{
		"failed to fetch public key: HTTP 403: fixture-private-detail",
		"HTTP 401: fixture-private-detail",
		"failed (HTTP 403) fixture-private-detail",
	} {
		if !nativeSecretPermissionRefused(message) {
			t.Fatal("known authentication/permission refusal was not classified")
		}
	}
	if nativeSecretPermissionRefused("network failure with fixture-private-detail") {
		t.Fatal("unknown response cannot establish a permission refusal")
	}
}

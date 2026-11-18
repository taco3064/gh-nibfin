package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

func TestDashboardDetailChecksNativeIdentityBeforeCollections(t *testing.T) {
	for _, repo := range []string{
		`{"id":9,"full_name":"example/project","permissions":{"pull":true}}`,
		`{"id":2,"full_name":"other/project","permissions":{"pull":true}}`,
		`{"id":2,"full_name":"example/project","permissions":{"pull":false}}`,
		`{"id":2,"full_name":"example/project","private":true,` +
			`"permissions":{"pull":true}}`,
	} {
		api := readableDashboardAPI()
		api.repo = repo
		provider := dashboardOverviewRepository{api: api,
			subject: snapshot.Subject{ID: 1, Login: "reader"}}
		value := provider.CollectRepository(snapshot.Repository{
			ID: 2, Name: "example/project"})
		if len(value.Repository.Items) != 0 ||
			!reflect.DeepEqual(api.calls, []string{"user", "repos/example/project"}) {
			t.Fatal("unverified repository reached collections", api.calls, value)
		}
	}
}

func TestDashboardDetailUsesVerifiedRepository(t *testing.T) {
	api := readableDashboardAPI()
	provider := dashboardOverviewRepository{api: api,
		subject: snapshot.Subject{ID: 1, Login: "reader"}}
	value := provider.CollectRepository(snapshot.Repository{ID: 2, Name: "example/project"})
	if len(value.Repository.Items) != 1 || value.Repository.Items[0].ID != 2 ||
		len(api.calls) <= 2 {
		t.Fatal("verified repository not collected", api.calls, value)
	}
}

type changingDashboardSubject struct{ *dashboardAPI }

func (a changingDashboardSubject) Call(method, path string, input, output any) error {
	err := a.dashboardAPI.Call(method, path, input, output)
	if strings.Contains(path, "/issues?") {
		a.user = `{"id":99,"login":"other","type":"User"}`
	}
	return err
}

func TestDashboardDetailDiscardsReadAfterSubjectChanges(t *testing.T) {
	api := changingDashboardSubject{readableDashboardAPI()}
	provider := dashboardOverviewRepository{api: api,
		subject: snapshot.Subject{ID: 1, Login: "reader"}}
	value := provider.CollectRepository(snapshot.Repository{ID: 2, Name: "example/project"})
	if len(value.Repository.Items) != 0 || api.calls[len(api.calls)-1] != "user" {
		t.Fatal("changed subject received collected detail", api.calls, value)
	}
}

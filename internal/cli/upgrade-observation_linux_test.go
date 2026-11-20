package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
)

type upgradeHTTPFixture func(*http.Request) (*http.Response, error)

func (f upgradeHTTPFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestUpgradeObservationPublicIdentityUsesNoCredentials(t *testing.T) {
	client := &http.Client{Transport: upgradeHTTPFixture(func(r *http.Request) (
		*http.Response, error,
	) {
		if r.Method != "GET" || r.URL.String() != "https://api.github.com/repos/o/r" ||
			r.Header.Get("Authorization") != "" {
			t.Fatal("nonpublic request", r)
		}
		return &http.Response{StatusCode: 200,
			Body: io.NopCloser(strings.NewReader(`{"id":10,"full_name":"o/r"}`))}, nil
	})}
	id, err := publicUpgradeRepositoryWithClient(context.Background(), "o/r", client)
	if err != nil || id != 10 {
		t.Fatal(id, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.Transport = upgradeHTTPFixture(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})
	if _, err := publicUpgradeRepositoryWithClient(ctx, "o/r", client); err == nil {
		t.Fatal("cancelled observation established identity")
	}
}

func TestUpgradeObservationUsesAuthoritativeAdoptionAndPoolComparison(t *testing.T) {
	host := upgradeTestHost()
	observed := upgradeHostObservation(upgradeTestObservation(), &host, 10)
	if observed.Adoption != "observed" ||
		!matchesUpgradeInstallation(observed.Installation, &host) {
		t.Fatal(observed)
	}
	if strings.Contains(observed.Installation, "token") {
		t.Fatal("credential export")
	}
	for _, ids := range [][]int64{nil, {99}} {
		host.AdoptedRepositoryIDs = ids
		observed = upgradeHostObservation(observed, &host, 10)
		if observed.Adoption == "observed" {
			t.Fatal("host adopted was treated as repo adopted")
		}
	}
	host.Runners[0].ID++
	if matchesUpgradeInstallation(upgradeTestObservation().Installation, &host) {
		t.Fatal("replacement pool matched")
	}
}

func TestUpgradeLinuxBoundaryRejectsChangedTargetForLatestAndStable(t *testing.T) {
	for _, version := range []string{"latest", "v1.2.3"} {
		host := upgradeTestHost()
		app := App{upgrade: &upgradeCommandOperations{
			discover: func() (*sharedHost, error) { return &host, nil },
			verify:   func(sharedHost) error { return nil },
		}}
		// Nil provider and mutation operations prove rejection precedes planning.
		err := app.Run([]string{"upgrade", "--repo=o/r", "--version=" + version,
			"--apply", "--linux-target-identity=" + strings.Repeat("f", 32),
			"--linux-target-arch=" + runtime.GOARCH,
			"--linux-target-installation=" + upgradeInstallation(host)}, io.Discard)
		if err == nil || err.Error() != "GUEST_TARGET_CHANGED" {
			t.Fatal(version, err)
		}
	}
}

func TestUpgradeObservationBoundaryExportsOnlyControlledResult(t *testing.T) {
	expected := errors.New("SHARED_HOST_INSPECTION_UNAVAILABLE")
	app := App{upgradeObservation: func(string) (upgradeGuestObservation, error) {
		return upgradeGuestObservation{}, expected
	}}
	if err := app.Run([]string{upgradeObserveCommand, "o/r"}, io.Discard); err != expected {
		t.Fatal(err)
	}
	if app.Run([]string{upgradeObserveCommand, "../bad"}, io.Discard) == nil {
		t.Fatal("invalid observation repository accepted")
	}
}

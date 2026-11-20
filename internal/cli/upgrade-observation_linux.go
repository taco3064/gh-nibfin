package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"runtime"
	"slices"
	"strings"
)

// Public repository identity is read without desktop or guest credentials.
// Authorization is deliberately left to the normal guest upgrade boundary.
func publicUpgradeRepository(ctx context.Context, name string) (int64, error) {
	return publicUpgradeRepositoryWithClient(ctx, name, http.DefaultClient)
}

func publicUpgradeRepositoryWithClient(ctx context.Context, name string,
	client *http.Client) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://api.github.com/repos/"+name, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(req)
	if err != nil {
		return 0, errors.New("PUBLIC_REPOSITORY_OBSERVATION_UNAVAILABLE")
	}
	defer response.Body.Close()
	var repo Repository
	decoder := json.NewDecoder(io.LimitReader(response.Body, desktopOutputLimit))
	if response.StatusCode != http.StatusOK ||
		decoder.Decode(&repo) != nil ||
		repo.ID <= 0 || repo.Private || !strings.EqualFold(repo.FullName, name) {
		return 0, errors.New("PUBLIC_REPOSITORY_OBSERVATION_UNAVAILABLE")
	}
	return repo.ID, nil
}

func (a App) observeInstalledUpgrade(name string) (upgradeGuestObservation, error) {
	o := upgradeGuestObservation{Repository: name, Identity: linuxIdentity(),
		Architecture: runtime.GOARCH, Adoption: "required"}
	if o.Identity == "" || (o.Architecture != "amd64" && o.Architecture != "arm64") {
		return o, errors.New("GUEST_IDENTITY_UNAVAILABLE")
	}
	host, err := a.upgradeCommandOperations().discover()
	if err != nil {
		return o, err
	}
	if host == nil {
		return o, nil
	}
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	id, err := publicUpgradeRepository(ctx, name)
	if err != nil {
		return o, err
	}
	return upgradeHostObservation(o, host, id), nil
}

func (a App) checkUpgradeExpectation(o upgradeOptions, host *sharedHost) error {
	if o.Identity == "" && o.Architecture == "" && o.Installation == "" {
		return nil
	}
	if o.Identity != linuxIdentity() || !machineIdentity.MatchString(o.Identity) ||
		o.Architecture != runtime.GOARCH || !matchesUpgradeInstallation(o.Installation, host) {
		return errors.New("GUEST_TARGET_CHANGED")
	}
	// Reuse the existing Linux handoff's native platform and machine checks.
	f := flag.NewFlagSet("upgrade-handoff", flag.ContinueOnError)
	guard := a.linuxHandoffGuard(f)
	if err := f.Parse([]string{"--linux-target-identity", o.Identity,
		"--linux-target-arch", o.Architecture}); err != nil {
		return err
	}
	if err := guard(); err != nil {
		return err
	}
	current, err := a.upgradeCommandOperations().discover()
	if err != nil {
		return err
	}
	if !host.samePool(current) || current == nil ||
		!slices.Equal(host.AdoptedRepositoryIDs, current.AdoptedRepositoryIDs) {
		return errors.New("GUEST_TARGET_CHANGED")
	}
	return nil
}

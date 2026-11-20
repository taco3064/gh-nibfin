package cli

import (
	"errors"
	"flag"
	"io"
)

type upgradeOptions struct {
	Repo, Version                        string
	Apply                                bool
	Target                               string
	Consent                              bool
	Identity, Architecture, Installation string
}

func parseUpgradeOptions(args []string, desktop bool) (upgradeOptions, error) {
	o := upgradeOptions{}
	f := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.Repo, "repo", "", "Initialized public OWNER/REPO")
	f.StringVar(&o.Version, "version", "latest", "Official release or latest")
	f.BoolVar(&o.Apply, "apply", false, "Apply through the guest operation")
	retired := f.Bool("managed-issue", false, "Retired managed Issue route")
	if desktop {
		f.StringVar(&o.Target, "target", "", "Existing wsl:NAME installation")
		f.BoolVar(&o.Consent, "consent-open-target", false, "Consent to opening WSL")
	} else {
		f.StringVar(&o.Identity, "linux-target-identity", "", "Expected machine")
		f.StringVar(&o.Architecture, "linux-target-arch", "", "Expected architecture")
		f.StringVar(&o.Installation, "linux-target-installation", "", "Expected pool")
	}
	if f.Parse(args) != nil || f.NArg() != 0 || !repoPattern.MatchString(o.Repo) ||
		o.Version != "latest" && !versionPattern.MatchString(o.Version) {
		return o, errors.New("INVALID_UPGRADE_ARGUMENTS")
	}
	if *retired {
		return o, errors.New("MANAGED_ARTIFACT_ROUTE_RETIRED_USE_NATIVE_UPGRADE")
	}
	return o, nil
}

func (o upgradeOptions) arguments() []string {
	apply := "--apply=false"
	if o.Apply {
		apply = "--apply=true"
	}
	return []string{"upgrade", "--repo", o.Repo, "--version", o.Version, apply}
}

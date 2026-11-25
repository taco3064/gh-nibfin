package op

import (
	"context"
	"sort"

	"github.com/taco3064/gh-nibfin/internal/runners"
)

// RunnerHostInfo is what init needs to know about this host's Runners.
type RunnerHostInfo struct {
	// Describe names the host.
	Describe string `json:"describe"`
	// Stopped is true when the podman machine is stopped and was not
	// started; the other fields are then empty.
	Stopped bool `json:"stopped"`
	// Repo is HOST/OWNER/REPO of the configured Runners, "" when none.
	Repo string `json:"repo,omitempty"`
	// Providers are the sorted providers with a configured Runner.
	Providers []string `json:"providers"`
}

// HostRunners reads this host's Runners. A stopped machine is reported as
// Stopped unless start is set; then it is started first, through the
// autostart entry when it is present (so the VM does not depend on this
// process), otherwise directly. It never registers an autostart entry.
func HostRunners(ctx context.Context, start bool) (RunnerHostInfo, error) {
	info := RunnerHostInfo{Providers: []string{}}
	h, err := detectHost(ctx)
	if err != nil {
		return info, err
	}
	info.Describe = h.Describe()
	if h.NeedsStart() {
		if !start {
			info.Stopped = true
			return info, nil
		}
		as, err := h.Autostart(ctx)
		if err != nil {
			return info, autostartError(ctx, err)
		}
		if as.Present {
			err = h.StartViaAutostart(ctx)
		} else {
			err = h.Start(ctx)
		}
		if err != nil {
			if ctx.Err() != nil {
				return info, Errorf(CodeCancelled, "cancelled")
			}
			return info, hostError(err)
		}
	}
	inv, err := listRunners(ctx, h)
	if err != nil {
		return info, err
	}
	repo, err := runners.Repo(inv.Runners)
	if err != nil {
		return info, Wrap(CodeRunnerRepoConflict, err)
	}
	info.Repo = repo
	seen := map[string]bool{}
	for _, r := range inv.Runners {
		if r.Configured && !seen[r.Provider] {
			seen[r.Provider] = true
			info.Providers = append(info.Providers, r.Provider)
		}
	}
	sort.Strings(info.Providers)
	return info, nil
}

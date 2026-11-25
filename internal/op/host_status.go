package op

import (
	"context"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/host"
	"github.com/taco3064/gh-nibfin/internal/runners"
)

// HostReport is the runner-host part of status. Each part carries its own
// error; reading it never fails as a whole.
type HostReport struct {
	Host      HostInfo         `json:"host"`
	Memory    MemoryReport     `json:"memory"`
	Runners   RunnersReport    `json:"runners"`
	Providers []ProviderReport `json:"providers"`
	Autostart AutostartReport  `json:"autostart"`
}

// AutostartReport is the entry that starts the podman machine at logon (a
// Task Scheduler task on Windows, a LaunchAgent on macOS). Kind is empty on
// the local Linux host, which needs none.
type AutostartReport struct {
	Kind    string `json:"kind"`
	Name    string `json:"name,omitempty"`
	Present bool   `json:"present"`
	// Current is true when the entry is the one runner add registers.
	Current bool   `json:"current"`
	Error   *Error `json:"error,omitempty"`
}

// HostInfo is the runner host itself.
type HostInfo struct {
	// Describe names the host, as in the other commands' results.
	Describe string `json:"describe,omitempty"`
	// Machine is the podman machine name; empty for the local Linux host.
	Machine string `json:"machine,omitempty"`
	VMType  string `json:"vmType,omitempty"`
	Running bool   `json:"running"`
	Error   *Error `json:"error,omitempty"`
}

// MemoryReport is the runner host's memory from /proc/meminfo, in kB.
type MemoryReport struct {
	TotalKB     int64  `json:"totalKB"`
	AvailableKB int64  `json:"availableKB"`
	Error       *Error `json:"error,omitempty"`
}

// RunnersReport is every runner slot on the host, joined with GitHub's list
// of the repository the runners serve.
type RunnersReport struct {
	Repo    string        `json:"repo,omitempty"`
	Runners []RunnerState `json:"runners"`
	// GitHubError is set when GitHub's runner list could not be read; the
	// local fields are still reported.
	GitHubError *Error `json:"githubError,omitempty"`
	// GitHubNeedsAdmin is set when GitHub's runner list (online/busy) was
	// not read because the gh login is not admin of Repo; the local fields
	// are reported.
	GitHubNeedsAdmin bool   `json:"githubNeedsAdmin,omitempty"`
	Error            *Error `json:"error,omitempty"`
}

// ProviderReport is one provider's login on the host.
type ProviderReport struct {
	Provider string `json:"provider"`
	LoggedIn bool   `json:"loggedIn"`
	Error    *Error `json:"error,omitempty"`
}

// ReadHost reads the runner host for status. It only reads: a stopped
// machine is reported, not started; the login check never pulls an image;
// no volume is created.
func ReadHost(ctx context.Context) HostReport {
	rep := HostReport{Runners: RunnersReport{Runners: []RunnerState{}}, Providers: []ProviderReport{}}
	h, err := detectHost(ctx)
	if err != nil {
		e := AsError(err)
		rep.Host.Error, rep.Memory.Error, rep.Runners.Error, rep.Autostart.Error = e, e, e, e
		for _, p := range providerTable {
			rep.Providers = append(rep.Providers, ProviderReport{Provider: p.name, Error: e})
		}
		return rep
	}
	rep.Host.Describe = h.Describe()
	rep.Host.Running = !h.NeedsStart()
	if hh, ok := h.(*host.Host); ok {
		rep.Host.Machine, rep.Host.VMType = hh.Machine, hh.VMType
	}
	// The entry is read from the host scheduler or launchd, also on a
	// stopped machine.
	if as, err := h.Autostart(ctx); err != nil {
		rep.Autostart = AutostartReport{Kind: as.Kind, Name: as.Name, Error: AsError(autostartError(ctx, err))}
	} else {
		rep.Autostart = AutostartReport{Kind: as.Kind, Name: as.Name, Present: as.Present, Current: as.Current}
	}
	if !rep.Host.Running {
		e := AsError(Errorf(CodeMachineStopped, "%s is stopped; status does not start it", h.Describe()))
		rep.Memory.Error, rep.Runners.Error = e, e
		for _, p := range providerTable {
			rep.Providers = append(rep.Providers, ProviderReport{Provider: p.name, Error: e})
		}
		return rep
	}

	if pr, err := probeHost(ctx, h, CodeRunnerInstallFailed); err != nil {
		rep.Memory.Error = AsError(err)
	} else {
		rep.Memory.TotalKB, rep.Memory.AvailableKB = pr.memKB, pr.memAvailKB
	}
	rep.Runners = readRunners(ctx, h)
	for _, p := range providerTable {
		rep.Providers = append(rep.Providers, readProvider(ctx, h, p))
	}
	return rep
}

func readRunners(ctx context.Context, h runnerHost) RunnersReport {
	rep := RunnersReport{Runners: []RunnerState{}}
	inv, err := listRunners(ctx, h)
	if err != nil {
		rep.Error = AsError(err)
		return rep
	}
	rep.Runners = joinRunners(inv.Runners, nil)
	repo, err := runners.Repo(inv.Runners)
	if err != nil {
		rep.Error = AsError(Wrap(CodeRunnerRepoConflict, err))
		return rep
	}
	rep.Repo = repo
	if repo == "" {
		return rep
	}
	gh, err := newGitHub(repoHost(repo))
	var perms repoPerms
	if err == nil {
		perms, err = gh.RepoPermissions(ctx, repo)
	}
	if err != nil {
		rep.GitHubError = AsError(err)
		return rep
	}
	// GitHub's runner list needs admin; a collaborator's runners are
	// reported from the host only.
	if !perms.Admin {
		rep.GitHubNeedsAdmin = true
		return rep
	}
	list, err := gh.ListRunners(ctx, repo)
	if err != nil {
		rep.GitHubError = AsError(err)
		return rep
	}
	rep.Runners = joinRunners(inv.Runners, list)
	return rep
}

func readProvider(ctx context.Context, h runnerHost, p provider) ProviderReport {
	rep := ProviderReport{Provider: p.name}
	has, err := volumeExists(ctx, h, p.volume())
	if err == nil && has {
		rep.LoggedIn, err = runCheck(ctx, h, p, statusCheckArgs(p))
	}
	if err != nil {
		rep.Error = AsError(err)
	}
	return rep
}

// joinRunners joins the host's runners with GitHub's list by runner name,
// which GitHub compares without case.
func joinRunners(rs []runners.Runner, list []ghRunner) []RunnerState {
	listed := map[string]ghRunner{}
	for _, g := range list {
		listed[strings.ToLower(g.Name)] = g
	}
	out := make([]RunnerState, 0, len(rs))
	for _, r := range rs {
		st := RunnerState{Runner: r}
		if g, ok := listed[strings.ToLower(r.Name)]; ok && r.Name != "" {
			st.Listed, st.Status, st.Busy = true, g.Status, g.Busy
		}
		out = append(out, st)
	}
	return out
}

package cli

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

//go:embed overview-bootstrap.py
var overviewBootstrap string

const overviewResultLimit = 1024*1024 + desktopOutputLimit

var overviewStamp = regexp.MustCompile(`^[0-9:]{1,128}$`)

type wslOverviewObserver struct {
	mu       sync.Mutex
	ctx      context.Context
	process  Process
	subject  snapshot.Subject
	explicit string
	selected *wslOverviewTarget
	failure  string
}

type wslOverviewTarget struct {
	target  desktopTarget
	binding overviewBinding
}

func (o *wslOverviewObserver) Observe() snapshot.LocalObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failure != "" {
		return snapshot.UnavailableObservation(o.failure)
	}
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	p := overviewTransport{ctx: ctx, process: o.process}
	target := o.explicit
	if o.selected != nil {
		target = "wsl:" + o.selected.target.Name
	}
	current, code := selectOverviewTarget(p, o.subject.ID, target)
	if o.selected != nil && (code == "NO_RUNNING_WSL_TARGET" ||
		code == "" && current.binding != o.selected.binding) {
		o.failure = "TARGET_CHANGED_RESELECT"
		return snapshot.UnavailableObservation(o.failure)
	}
	if code != "" {
		// A failed read cannot establish a change. Refresh the same target.
		return snapshot.UnavailableObservation(code)
	}
	if o.selected == nil {
		o.selected = &current
	}
	token, err := p.Run("gh", []string{"auth", "token", "--hostname", "github.com"}, nil)
	if err != nil {
		return snapshot.UnavailableObservation("CREDENTIAL_UNAVAILABLE")
	}
	input, err := observationInput(token)
	if err != nil {
		return snapshot.UnavailableObservation("CREDENTIAL_UNAVAILABLE")
	}
	b := current.binding
	_, args := guestCommand(current.target, b.Entry, overviewBridgeCommand,
		"observe", strconv.FormatInt(o.subject.ID, 10), o.subject.Login,
		b.Identity, b.Architecture, b.Stamp)
	data, err := p.read(args, input, overviewResultLimit)
	if err != nil {
		return snapshot.UnavailableObservation("OBSERVATION_UNAVAILABLE")
	}
	// Supported nonzero denials are already sanitized by the native transport.
	if denial, ok := observationDenial(data); ok {
		return snapshot.DecodeLocalObservation(denial, o.subject)
	}
	var result overviewGuestResult
	if json.Unmarshal(data, &result) != nil {
		return snapshot.UnavailableObservation("INVALID_OBSERVATION")
	}
	value := snapshot.DecodeLocalObservation(result.Observation, o.subject)
	if value.Status == "observed" {
		host := &sharedHost{MemberID: o.subject.ID,
			AdoptedRepositoryIDs: result.AdoptedIDs}
		ids, partial := overviewAdoptedIDs(host, o.subject.ID)
		value.AdoptedIDs = ids
		if result.Partial || partial {
			value.Completeness = "partial"
		}
	}
	return value
}

func selectOverviewTarget(p overviewTransport, member int64, explicit string) (
	wslOverviewTarget, string,
) {
	var empty wslOverviewTarget
	targets, err := wslInventory(p)
	if err != nil {
		return empty, "WSL_INVENTORY_UNAVAILABLE"
	}
	var candidates []desktopTarget
	for _, t := range targets {
		if explicit != "" && explicit != "wsl:"+t.Name {
			continue
		}
		if t.Running && t.Version == 2 {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return empty, "NO_RUNNING_WSL_TARGET"
	}
	var verified []wslOverviewTarget
	for _, target := range candidates {
		binding, err := probeOverviewTarget(p, target, member)
		if err == nil {
			verified = append(verified, wslOverviewTarget{target, binding})
			if len(verified) == 2 {
				return empty, "SELECT_TARGET_WITH_TARGET_OPTION"
			}
		}
	}
	if p.ctx.Err() != nil {
		return empty, "WSL_DISCOVERY_UNAVAILABLE"
	}
	if len(verified) == 0 {
		return empty, "GUEST_CLI_OR_ADOPTION_UNAVAILABLE"
	}
	if len(verified) != 1 {
		return empty, "SELECT_TARGET_WITH_TARGET_OPTION"
	}
	return verified[0], ""
}

func probeOverviewTarget(p overviewTransport, target desktopTarget, member int64) (
	overviewBinding, error,
) {
	invalid := errors.New("GUEST_CLI_OR_ADOPTION_UNAVAILABLE")
	var binding overviewBinding
	// Bootstrap filesystem reads establish executable trust before any CLI runs.
	// Python is a fixed system entry; no private input is supplied to this step.
	data, err := guestQuery(p, target, "/usr/bin/python3",
		"-I", "-B", "-c", overviewBootstrap)
	var resolved struct {
		Entry string `json:"entry"`
	}
	if err != nil || json.Unmarshal(data, &resolved) != nil ||
		!validOverviewEntry(resolved.Entry) {
		return binding, invalid
	}
	data, err = guestQuery(p, target, resolved.Entry, overviewBridgeCommand,
		"probe", strconv.FormatInt(member, 10))
	if err != nil || json.Unmarshal(data, &binding) != nil || binding.Schema != 1 ||
		binding.MemberID != member || !machineIdentity.MatchString(binding.Identity) ||
		(binding.Architecture != "amd64" && binding.Architecture != "arm64") ||
		binding.Entry != resolved.Entry || !overviewStamp.MatchString(binding.Stamp) {
		return overviewBinding{}, invalid
	}
	return binding, nil
}

func validOverviewEntry(entry string) bool {
	return len(entry) <= 4096 && strings.HasPrefix(entry, "/") &&
		path.Clean(entry) == entry && !strings.ContainsAny(entry, "\x00\r\n") &&
		!strings.HasPrefix(entry, "/tmp/") && !strings.HasPrefix(entry, "/var/tmp/")
}

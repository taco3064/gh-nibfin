package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

const overviewBridgeCommand = "__overview-bridge"

type overviewBinding struct {
	Schema       int    `json:"schema"`
	Identity     string `json:"identity"`
	Architecture string `json:"architecture"`
	Entry        string `json:"entry"`
	Stamp        string `json:"stamp"`
	MemberID     int64  `json:"member_id"`
}

type overviewGuestResult struct {
	Observation json.RawMessage `json:"observation"`
	AdoptedIDs  []int64         `json:"adopted_ids"`
	Partial     bool            `json:"partial"`
}

type overviewGuest struct {
	process    Process
	binding    overviewBinding
	node, core string
}

func (a App) runOverviewBridge(args []string, out io.Writer) error {
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return executeOverviewBridge(ctx, a.Process, args, out, osOverviewInput(),
		loadOverviewGuest)
}

// The loader verifies the installed files, independently of the caller's probe.
func executeOverviewBridge(ctx context.Context, process Process, args []string,
	out io.Writer, input io.Reader,
	load func() (overviewBinding, string, string, error)) error {
	fail := errors.New("GUEST_OVERVIEW_UNAVAILABLE")
	if len(args) != 2 && len(args) != 6 ||
		len(args) == 2 && args[0] != "probe" ||
		len(args) == 6 && args[0] != "observe" {
		return fail
	}
	member, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || member <= 0 || process == nil {
		return fail
	}
	if ctx == nil {
		ctx = context.Background()
	}
	binding, node, core, err := load()
	if err != nil || binding.MemberID != 0 {
		return fail
	}
	p := overviewTransport{ctx: ctx, process: process}
	help, err := p.Run(node, []string{core, "help"}, nil)
	if err != nil {
		return fail
	}
	commands, err := observationCommands(help)
	if err != nil || !overviewCommand(commands, "inspect-shared-host") {
		return fail
	}
	data, err := p.Run(node, []string{core, "inspect-shared-host"}, nil)
	if err != nil || len(data) > 256*1024 {
		return fail
	}
	host, err := decodeSharedHost(data)
	if err != nil || host.MemberID != member {
		return fail
	}
	binding.MemberID = member
	if args[0] == "probe" {
		return json.NewEncoder(out).Encode(binding)
	}
	// All expectations are fixed fields, never a path, program or Core selector.
	if args[3] != binding.Identity || args[4] != binding.Architecture ||
		args[5] != binding.Stamp || !dashboardLogin.MatchString(args[2]) {
		return fail
	}
	return (overviewGuest{process: p, binding: binding, node: node, core: core}).
		observe(args[2], input, out)
}

func (g overviewGuest) observe(login string, input io.Reader,
	out io.Writer) error {
	fail := errors.New("GUEST_OVERVIEW_UNAVAILABLE")
	data, err := io.ReadAll(io.LimitReader(input, observationInputLimit+1))
	var private struct {
		Token string `json:"token"`
	}
	if err != nil || len(data) > observationInputLimit ||
		json.Unmarshal(data, &private) != nil {
		return fail
	}
	// Reject extra fields and trailing input; forward only the shared private shape.
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields) != 1 || fields["token"] == nil {
		return fail
	}
	body, err := observationInput([]byte(private.Token))
	if err != nil {
		return fail
	}
	data, err = g.process.Run(g.node, []string{g.core, "observe-local-work"}, body)
	if err != nil {
		return fail
	}
	value := snapshot.DecodeLocalObservation(data,
		snapshot.Subject{ID: g.binding.MemberID, Login: login})
	// Encode only sanitized shared observations, not arbitrary guest diagnostics.
	observation, _ := json.Marshal(value)
	data, err = g.process.Run(g.node, []string{g.core, "inspect-shared-host"}, nil)
	var host *sharedHost
	if err == nil && len(data) <= 256*1024 {
		host, err = decodeSharedHost(data)
	}
	ids, partial := overviewAdoptedIDs(host, g.binding.MemberID)
	partial = partial || err != nil
	return json.NewEncoder(out).Encode(overviewGuestResult{
		Observation: observation, AdoptedIDs: ids, Partial: partial})
}

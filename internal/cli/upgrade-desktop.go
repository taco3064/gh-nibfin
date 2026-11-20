package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func (a App) routeUpgrade(args []string, host string, out io.Writer) error {
	if host == "windows" {
		return a.runDesktopUpgrade(args, out)
	}
	return a.runUpgrade(args, out)
}

func (a App) runDesktopUpgrade(args []string, out io.Writer) error {
	o, err := parseUpgradeOptions(args, true)
	if err != nil {
		return err
	}
	if o.Target != "" && (!strings.HasPrefix(o.Target, "wsl:") ||
		!guestName.MatchString(strings.TrimPrefix(o.Target, "wsl:"))) {
		return errors.New("UPGRADE_REQUIRES_EXPLICIT_WSL_TARGET")
	}
	if a.Process == nil {
		return errors.New("WSL_TOOL_UNAVAILABLE")
	}
	ctx, stop := upgradeContext(a.Context)
	defer stop()
	p := upgradeDesktopTransport{ctx: ctx, process: a.Process}
	targets, err := discoverUpgradeTargets(p, o.Repo)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	preference := a.readUpgradePreferences()[strings.ToLower(o.Repo)]
	selected, err := selectUpgradeTarget(targets, o.Target, preference)
	if err != nil {
		for _, t := range targets {
			fmt.Fprintf(out, "wsl:%s: %s %s\n", t.target.Name,
				t.observed.Adoption, t.gap)
		}
		return err
	}
	key := "wsl:" + selected.target.Name
	recovery := append([]string{"gh", "nibfin"}, o.arguments()...)
	recovery = append(recovery, "--target", key)
	// JSON argv preserves preview/apply intent and names containing spaces.
	if json.NewEncoder(out).Encode(map[string]any{"resume_argv": recovery}) != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if !selected.target.Running {
		if !o.Apply || !o.Consent {
			return &CommandError{
				Code: "TARGET_OPEN_CONSENT_REQUIRED_USE_CONSENT_OPEN_TARGET", Exit: 2,
			}
		}
		// Opening executes only true, never an upgrade or host provisioning.
		if _, err := guestQuery(p, selected.target, "/usr/bin/true"); err != nil {
			return errors.New("GUEST_OPEN_UNAVAILABLE")
		}
		targets, err = discoverUpgradeTargets(p, o.Repo)
		if err != nil {
			return err
		}
		selected, err = selectUpgradeTarget(targets, key, upgradePreference{})
		if err != nil {
			return err
		}
	}
	if selected.gap != "" {
		return errors.New(selected.gap)
	}
	if !eligibleUpgradeTarget(selected) {
		return errors.New("REQUESTED_REPOSITORY_NOT_ADOPTED_USE_INIT")
	}
	// Revalidate a remembered stopped target after opening unless reselected.
	if o.Target == "" && preference.Target == key &&
		preference.Observation.Installation != "" &&
		!sameUpgradeObservation(preference.Observation, selected.observed) {
		return errors.New("REMEMBERED_INSTALLATION_CHANGED_SELECT_TARGET")
	}
	current, err := observeUpgradeTarget(p, selected.target, o.Repo)
	if err != nil {
		return err
	}
	if !eligibleUpgradeTarget(current) ||
		!sameUpgradeObservation(selected.observed, current.observed) ||
		selected.entry != current.entry {
		return errors.New("GUEST_TARGET_CHANGED")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	terminal, ok := a.Process.(UpgradeDesktopTerminal)
	if !ok {
		return errors.New("GUEST_UPGRADE_TERMINAL_UNAVAILABLE")
	}
	if o.Apply {
		if err := a.rememberUpgradeTarget(o.Repo, current); err != nil {
			// A preference grants no authority and records no operation state.
			if _, err := fmt.Fprintln(out,
				"Target preference unavailable; retry with --target "+key); err != nil {
				return errors.New("OUTPUT_UNAVAILABLE")
			}
		}
	}
	command := []string{"/usr/bin/python3", "-I", "-B", "-c", upgradeExitScript,
		current.entry}
	command = append(command, o.arguments()...)
	command = append(command, "--linux-target-identity", current.observed.Identity,
		"--linux-target-arch", current.observed.Architecture,
		"--linux-target-installation", current.observed.Installation)
	tool, argv := guestCommand(current.target, command...)
	return terminal.EnterUpgradeGuest(ctx, tool, argv, out)
}

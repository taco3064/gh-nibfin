package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

type desktopOptions struct {
	Repo    string
	Target  string
	Consent bool
	Apply   bool
	Forward []string
}

func parseDesktopOptions(args []string) (desktopOptions, error) {
	var o desktopOptions
	invalid := errors.New("INVALID_DESKTOP_ARGUMENTS_USE_HELP")
	if len(args) == 0 {
		return o, invalid
	}
	o.Forward = []string{args[0]}
	seen := map[string]bool{}
	explicitRunnerOptions := false
	for i := 1; i < len(args); i++ {
		key, value, assigned := strings.Cut(args[i], "=")
		if seen[key] {
			return o, invalid
		}
		seen[key] = true
		switch key {
		case "--runner-directory", "--handoff-directory", "--directory", "--quick":
			return o, errors.New("DESKTOP_PATH_OPTIONS_USE_GUEST_LINUX_CLI")
		case "--repo", "--target", "--execution-repo", "--runner-name", "--agent":
			if !assigned {
				i++
				if i == len(args) {
					return o, invalid
				}
				value = args[i]
			}
			if key == "--agent" && (args[0] != "init" ||
				validateAgentOption(value, true) != nil) {
				return o, invalid
			}
			if key == "--target" {
				o.Target = value
				continue
			}
			if key == "--repo" {
				o.Repo = value
			}
			if key == "--runner-name" && value != "" {
				explicitRunnerOptions = true
			}
			o.Forward = append(o.Forward, key, value)
		case "--consent-open-target", "--apply", "--consent-managed-login",
			"--install-shared-service", "--start-shared-services", "--prepare-shared-wake":
			if assigned && value != "true" && value != "false" {
				return o, invalid
			}
			enabled := !assigned || value == "true"
			if key == "--consent-open-target" {
				o.Consent = enabled
				continue
			}
			if key == "--apply" {
				o.Apply = enabled
			}
			if enabled && (key == "--install-shared-service" ||
				key == "--start-shared-services" || key == "--prepare-shared-wake") {
				explicitRunnerOptions = true
			}
			o.Forward = append(o.Forward, args[i])
		default:
			return o, invalid
		}
	}
	if seen["--agent"] && explicitRunnerOptions {
		return o, errors.New("AGENT_SELECTION_REQUIRES_MANAGED_INIT")
	}
	if !repoPattern.MatchString(o.Repo) || len(o.Repo) > 200 {
		return o, invalid
	}
	if args[0] == "preflight" && (o.Apply || o.Consent) {
		return o, invalid
	}
	return o, nil
}

func chooseDesktopTarget(targets []desktopTarget, explicit string) (int, error) {
	var eligible, adopted, stopped []int
	for i, t := range targets {
		if explicit != "" && t.Provider+":"+t.Name == explicit {
			if t.Status == "incompatible" {
				return -1, errors.New(t.Gap)
			}
			return i, nil
		}
		if t.Status == "eligible" {
			eligible = append(eligible, i)
			if t.Adopted {
				adopted = append(adopted, i)
			}
		} else if !t.Running && t.Status != "incompatible" {
			stopped = append(stopped, i)
		}
	}
	if explicit != "" {
		return -1, errors.New("SELECTED_TARGET_NOT_FOUND")
	}
	if len(adopted) > 0 {
		eligible = adopted
	}
	if len(eligible) == 0 {
		eligible = stopped
	}
	if len(eligible) == 1 {
		return eligible[0], nil
	}
	if len(eligible) > 1 {
		return -1, errors.New("SELECT_TARGET_WITH_TARGET_OPTION")
	}
	return -1, errors.New("NO_ELIGIBLE_LINUX_TARGET")
}

func desktopInteractive() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (a App) runDesktop(args []string, host string, out io.Writer) error {
	o, err := parseDesktopOptions(args)
	if err != nil {
		return err
	}
	targets, discoveryErr := discoverDesktop(a.Process, host, o.Repo)
	index, selectionErr := chooseDesktopTarget(targets, o.Target)
	plan := map[string]any{"protected_execution": "unverified", "targets": targets,
		"guidance": desktopGuidance(host, o.Repo), "status": "preview"}
	if discoveryErr != nil {
		plan["gap"] = discoveryErr.Error()
	}
	if selectionErr != nil {
		plan["selection"] = selectionErr.Error()
	}
	if index >= 0 {
		plan["selected_target"] = targets[index].Provider + ":" + targets[index].Name
		plan["handoff"] = desktopResume(targets[index], o)
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if encoder.Encode(plan) != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if args[0] == "preflight" {
		return &CommandError{Code: "PREFLIGHT_PROTECTED_EXECUTION_UNVERIFIED", Exit: 2}
	}
	if !o.Apply {
		return nil
	}
	if discoveryErr != nil {
		return discoveryErr
	}
	reader := bufio.NewReader(io.LimitReader(os.Stdin, 4096))
	if index < 0 && selectionErr != nil &&
		selectionErr.Error() == "SELECT_TARGET_WITH_TARGET_OPTION" && desktopInteractive() {
		fmt.Fprintln(out, "Enter the provider:name target shown above:")
		choice, _ := reader.ReadString('\n')
		index, selectionErr = chooseDesktopTarget(targets, strings.TrimSpace(choice))
		if selectionErr == nil {
			selected := map[string]any{
				"selected_target":     targets[index].Provider + ":" + targets[index].Name,
				"handoff":             desktopResume(targets[index], o),
				"protected_execution": "unverified",
			}
			if encoder.Encode(selected) != nil {
				return errors.New("OUTPUT_UNAVAILABLE")
			}
		}
	}
	if selectionErr != nil {
		return selectionErr
	}
	target := targets[index]
	if !o.Consent {
		if !desktopInteractive() {
			return errors.New("TARGET_CONSENT_REQUIRED_USE_TARGET_AND_CONSENT_OPEN_TARGET")
		}
		fmt.Fprintln(out, "Open this guest and delegate init? Type yes to consent:")
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(answer) != "yes" {
			return errors.New("TARGET_OPEN_NOT_CONSENTED")
		}
	}
	return a.handoffDesktop(target, o, host, out)
}

func (a App) handoffDesktop(target desktopTarget, o desktopOptions,
	host string, out io.Writer) error {
	terminal, ok := a.Process.(DesktopTerminal)
	if !ok {
		return errors.New("GUEST_TERMINAL_UNAVAILABLE_USE_SHOWN_RESUME")
	}
	// Inventory and any already-observed machine identity must still match.
	current, err := recheckDesktop(a.Process, target, host, o.Repo)
	if err != nil {
		return err
	}
	if !current.Running {
		if current.Provider == "multipass" {
			_, err = desktopQuery(a.Process, "multipass", "start", current.Name)
		} else {
			_, err = guestQuery(a.Process, current, "true")
		}
		if err != nil {
			return errors.New("GUEST_OPEN_UNAVAILABLE_USE_SHOWN_RESUME")
		}
	}
	current, err = recheckDesktop(a.Process, target, host, o.Repo)
	if err != nil {
		return err
	}
	if current.Status != "eligible" || current.Identity == "" || current.Linux == nil {
		return errors.New("GUEST_UNVERIFIED_USE_SHOWN_INSTALL_AND_PREFLIGHT")
	}
	// A final fresh observation binds the arguments to the guest just inspected.
	final, err := recheckDesktop(a.Process, current, host, o.Repo)
	if err != nil {
		return err
	}
	if final.Status != "eligible" || final.Linux == nil ||
		final.Linux.Architecture != current.Linux.Architecture {
		return errors.New("GUEST_TARGET_CHANGED")
	}
	command := append([]string{"gh", "nibfin"}, o.Forward...)
	command = append(command, "--linux-target-identity", final.Identity,
		"--linux-target-arch", final.Linux.Architecture)
	tool, argv := guestCommand(final, command...)
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := terminal.EnterGuest(ctx, tool, argv, out); err != nil {
		return errors.New("GUEST_DISCONNECTED_READ_BACK_REQUIRED_USE_SHOWN_RESUME")
	}
	// A successful CLI exit still does not certify protected execution.
	return nil
}

func recheckDesktop(p Process, old desktopTarget, host, repo string) (
	desktopTarget, error,
) {
	targets, err := discoverDesktop(p, host, repo)
	if err != nil {
		return desktopTarget{}, err
	}
	for _, t := range targets {
		if t.Provider != old.Provider || t.Name != old.Name {
			continue
		}
		if t.Status == "incompatible" || t.Version != old.Version ||
			old.Identity != "" && t.Identity != old.Identity {
			return desktopTarget{}, errors.New("GUEST_TARGET_CHANGED")
		}
		if old.Linux != nil && old.Linux.Architecture != "" &&
			(t.Linux == nil || old.Linux.Architecture != t.Linux.Architecture) {
			return desktopTarget{}, errors.New("GUEST_TARGET_CHANGED")
		}
		return t, nil
	}
	return desktopTarget{}, errors.New("GUEST_TARGET_CHANGED")
}

func desktopEntryHost() string { return runtime.GOOS }

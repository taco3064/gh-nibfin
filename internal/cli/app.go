package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type App struct {
	agents         *agentOperations
	handoffCheck   func() error
	upgrade        *upgradeCommandOperations
	Context        context.Context
	StartDashboard DashboardStart
	OpenDashboard  func(context.Context, string) error
	API            API
	Process        Process
	Runner         RunnerHost
	Download       RunnerDownload
	Clock          func() time.Time
}

type CommandError struct {
	Code string
	Exit int
}

func (e *CommandError) Error() string { return e.Code }

// Version is set by the local release producer. Source builds remain development.
var Version = "development"

func (a App) Run(args []string, out io.Writer) error {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		_, err := fmt.Fprintln(out, "gh-nibfin "+Version)
		return err
	}
	if len(args) == 0 || len(args) == 1 &&
		(args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, "gh nibfin: manual Bootstrap candidate\n"+
			dashboardUsage+"\n"+
			"Version: gh nibfin version (or --version)\n"+
			"Usage: gh nibfin init --repo OWNER/REPO [--agent codex|claude] [--apply]\n"+
			"Fresh managed init defaults to Codex; existing pools retain their Agent.\n"+
			"Agent availability requires verified Core composition; Claude is fixture-only.\n"+
			"Upgrade: gh nibfin upgrade --repo OWNER/REPO [--version VERSION] [--apply]\n"+
			"Default: preview only. Plain init --apply opens a PR; never merges it.\n"+
			"Shared execution: init --repo OWNER/REPO --execution-repo OWNER/REPO.\n"+
			"Shared host setup: init --repo OWNER/REPO --apply --start-shared-services.\n"+
			"Desktop targets: --target PROVIDER:NAME; --consent-open-target to open.\n"+
			"Windows sparse VHD is experimental: --reclamation enable (default) or unchanged.\n"+
			"Windows apply: --consent-experimental-sparse or --reclamation unchanged.\n"+
			"Desktop preview discovers existing WSL 2 or Multipass Ubuntu guests.\n"+
			"Optional Runner setup: --runner-directory PATH --runner-name NAME.\n"+
			"Member handoff: init --quick request/issue/register/confirm/reconcile\n"+
			"Recovery: init --quick inspect/cancel; see README for the full sequence.\n"+
			"Use --handoff-directory PATH with each step and --apply to execute.\n"+
			"Management issues/confirms; members use their own GitHub login.\n"+
			"Rebuild: runner rebuild --repo OWNER/REPO --agent codex|claude\n"+
			"Rebuild previews by default; --apply --consent-rebuild applies or resumes.\n"+
			"Rebuild requires local admin and GitHub authorization for adopted repos.\n"+
			"Only Core activation confirms replacement release; retry uncertain results.\n"+
			"Runner: create/register/list/unregister/remove --repo OWNER/REPO\n"+
			"Mutations require --directory ABSOLUTE_PATH --apply; create also --name.\n"+
			"Inspect: gh nibfin preflight --repo OWNER/REPO [--directory RUNNER_PATH]")
		return err
	}
	if args[0] == overviewBridgeCommand {
		return a.runOverviewBridge(args[1:], out)
	}
	if args[0] == "dashboard" {
		return a.runDashboard(args[1:], out)
	}
	if args[0] == "upgrade" {
		return a.runUpgrade(args[1:], out)
	}
	if args[0] == "runner" {
		return a.runRunner(args[1:], out)
	}
	if desktopHost() && (args[0] == "init" || args[0] == "preflight") {
		return a.runDesktop(args, desktopEntryHost(), out)
	}
	if args[0] == "preflight" {
		return a.runPreflight(args[1:], out)
	}
	if args[0] != "init" {
		return errors.New("UNKNOWN_COMMAND_USE_HELP")
	}
	return a.runInit(args[1:], out)
}

func (a App) requireHandoff() error {
	if a.handoffCheck != nil {
		return a.handoffCheck()
	}
	return nil
}

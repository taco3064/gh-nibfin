package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

type App struct {
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
			"Usage: gh nibfin init --repo OWNER/REPO [--apply]\n"+
			"Upgrade: gh nibfin upgrade --repo OWNER/REPO [--version VERSION] [--apply]\n"+
			"Default: preview only. Plain init --apply opens a PR; never merges it.\n"+
			"Shared execution: init --repo OWNER/REPO --execution-repo OWNER/REPO.\n"+
			"Optional Runner setup: --runner-directory PATH --runner-name NAME.\n"+
			"Member handoff: init --quick request/issue/register/confirm/reconcile\n"+
			"Recovery: init --quick inspect/cancel; see README for the full sequence.\n"+
			"Use --handoff-directory PATH with each step and --apply to execute.\n"+
			"Management issues/confirms; members use their own GitHub login.\n"+
			"Runner: create/register/list/unregister/remove --repo OWNER/REPO\n"+
			"Mutations require --directory ABSOLUTE_PATH --apply; create also --name.\n"+
			"Inspect: gh nibfin preflight --repo OWNER/REPO [--directory RUNNER_PATH]")
		return err
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
	if args[0] == "preflight" {
		return a.runPreflight(args[1:], out)
	}
	if args[0] != "init" {
		return errors.New("UNKNOWN_COMMAND_USE_HELP")
	}
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "Public OWNER/REPO")
	apply := flags.Bool("apply", false, "Create reviewed installation PR and labels")
	runnerPath := flags.String("runner-directory", "", "Private Runner directory")
	runnerName := flags.String("runner-name", "", "Name of the repository Runner")
	executionRepo := flags.String("execution-repo", "", "Shared execution repository")
	quick := addQuickFlags(flags)
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *name == "" {
		return errors.New("INVALID_INIT_ARGUMENTS")
	}
	if quick.used() {
		if *executionRepo != "" {
			return errors.New("SHARED_EXECUTION_QUICK_OPTIONS_CONFLICT")
		}
		return a.runQuick(*name, *runnerPath, *runnerName, *apply, quick, out)
	}
	if *executionRepo != "" && (*runnerPath != "" || *runnerName != "") {
		return errors.New("SHARED_EXECUTION_LEGACY_RUNNER_OPTIONS_CONFLICT")
	}
	var host *sharedHost
	defaultShared := false
	if *runnerPath == "" && *runnerName == "" {
		var err error
		host, err = discoverSharedHost(a.Process)
		if err != nil {
			return err
		}
		if host != nil {
			if *executionRepo != "" && *executionRepo != host.Repository {
				return errors.New("SHARED_EXECUTION_HOST_CONFLICT")
			}
			if err := verifySharedHost(a.API, *host); err != nil {
				return err
			}
			*executionRepo = host.Repository
		} else if *executionRepo == "" && nativeSharedHostSupported() {
			*executionRepo = *name
			defaultShared = true
		}
	}
	plan, err := planInitWithExecution(a.API, *name, *executionRepo)
	if defaultShared && errors.Is(err, errCoreSharedDestinationUnsupported) {
		// Older producer pins retain their existing installation contract.
		plan, err = planInit(a.API, *name)
	}
	if err != nil {
		return err
	}
	var runner *initRunner
	if *runnerPath != "" || *runnerName != "" {
		runner, err = a.planInitRunner(plan, *runnerPath, *runnerName)
		if err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	preview := map[string]any{"status": "preview", "plan": plan,
		"protected_execution": "unverified"}
	if runner != nil {
		preview["runner_setup"] = runner
	}
	if host != nil {
		preview["shared_runners"] = host.Runners
		preview["shared_host_adoption"] = host.adoptionStatus(plan.Repository.ID)
	} else if plan.Execution != nil && plan.Execution.RepositoryID == plan.Repository.ID {
		preview["planned_shared_runner_count"] = 2
	}
	if err := encoder.Encode(preview); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if !*apply {
		return nil
	}
	cache, err := installCoreCache(a.API, a.Process)
	if err != nil {
		return err
	}
	if err := encoder.Encode(map[string]any{"status": "candidate_source_verified",
		"core_cache": cache, "host_adoption": "unverified"}); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if host != nil {
		if err := recheckSharedHost(a.Process, a.API, *host); err != nil {
			return err
		}
	}
	result, err := applyInit(a.API, plan)
	if err == nil && host != nil {
		result.HostAdoption = host.adoptionStatus(plan.Repository.ID)
		if result.Code == "SHARED_HOST_ADOPTION_REQUIRED" &&
			result.HostAdoption == "observed" {
			result.Code = "SHARED_PROTECTED_EXECUTION_UNVERIFIED"
		}
	}
	if err != nil {
		result.Code = err.Error()
		_ = encoder.Encode(result)
		return &CommandError{Code: "INSTALLATION_INCOMPLETE_READ_BACK_REQUIRED", Exit: 2}
	}
	if err := encoder.Encode(result); err != nil {
		return &CommandError{Code: "OUTPUT_UNAVAILABLE_AFTER_INSTALLATION", Exit: 2}
	}
	if result.Status == "pending" {
		return &CommandError{Code: result.Code, Exit: 2}
	}
	if runner != nil {
		return a.applyInitRunner(plan, *runner, out)
	}
	return nil
}

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
	installService := flags.Bool("install-shared-service", false,
		"Install the verified shared Runner unit without starting it")
	prepareWake := flags.Bool("prepare-shared-wake", false,
		"Prepare the verified shared Wake destination without activating it")
	quick := addQuickFlags(flags)
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *name == "" {
		return errors.New("INVALID_INIT_ARGUMENTS")
	}
	if quick.used() {
		if *executionRepo != "" || *installService || *prepareWake {
			return errors.New("SHARED_EXECUTION_QUICK_OPTIONS_CONFLICT")
		}
		return a.runQuick(*name, *runnerPath, *runnerName, *apply, quick, out)
	}
	if *executionRepo != "" && (*runnerPath != "" || *runnerName != "") {
		return errors.New("SHARED_EXECUTION_LEGACY_RUNNER_OPTIONS_CONFLICT")
	}
	host, err := discoverSharedHost(a.Process)
	legacyHost := errors.Is(err, errLegacySharedHost)
	if err != nil && (!legacyHost || *executionRepo != "" || *installService ||
		*prepareWake || *runnerPath != "" || *runnerName != "") {
		return err
	}
	if *installService && (host == nil || *runnerPath != "" || *runnerName != "") {
		return errors.New("SHARED_SERVICE_REQUIRES_ADOPTED_HOST")
	}
	if *prepareWake && host == nil {
		return errors.New("SHARED_WAKE_REQUIRES_ADOPTED_HOST")
	}
	defaultShared := false
	var registrations []sharedRegistration
	if host != nil {
		registrations, err = host.verifyInit(a.API, *executionRepo, *runnerPath, *runnerName)
		if err != nil {
			return err
		}
		*executionRepo = host.Repository
	} else if *executionRepo == "" && *runnerPath == "" && *runnerName == "" &&
		!legacyHost && nativeSharedHostSupported() {
		*executionRepo = *name
		defaultShared = true
	}
	plan, err := planInitWithExecution(a.API, *name, *executionRepo)
	if defaultShared && errors.Is(err, errCoreSharedDestinationUnsupported) {
		// Avoid installing legacy callers on a fresh shared host. Those files
		// would conflict with the later installation of the shared candidate.
		return &CommandError{Code: "PINNED_CORE_SHARED_DESTINATION_UNSUPPORTED", Exit: 2}
	}
	if err != nil {
		return err
	}
	if *installService && (plan.Execution == nil ||
		plan.Repository.ID != plan.Execution.RepositoryID) {
		return errors.New("SHARED_SERVICE_REQUIRES_EXECUTION_REPOSITORY")
	}
	if *prepareWake && host.adoptionStatus(plan.Repository.ID) != "observed" {
		return errors.New("SHARED_WAKE_REQUIRES_ADOPTED_REPOSITORY")
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
	if legacyHost {
		preview["shared_host_adoption"] = "installed_core_upgrade_required"
	}
	if runner != nil {
		preview["runner_setup"] = runner
	}
	if host != nil {
		preview["shared_runners"] = registrations
		preview["shared_host_adoption"] = host.adoptionStatus(plan.Repository.ID)
		if *installService {
			preview["shared_service_template"] = "planned"
		}
	} else if plan.Execution != nil && plan.Execution.RepositoryID == plan.Repository.ID {
		preview["planned_shared_runner_count"] = 2
	}
	if *prepareWake {
		preview["shared_wake_destination"] = "planned"
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
	if err == nil && host == nil && nativeSharedHostSupported() &&
		plan.Execution != nil && plan.Execution.RepositoryID == plan.Repository.ID &&
		result.Code == "SHARED_HOST_ADOPTION_REQUIRED" {
		result.RunnerPackages, err = a.stageSharedRunnerPackages(plan)
		if err == nil {
			result.RunnerSlots, err = a.registerInitSharedSlots(plan, result.RunnerPackages)
		}
	}
	if err == nil && host != nil {
		result.HostAdoption = host.adoptionStatus(plan.Repository.ID)
		if result.Code == "SHARED_HOST_ADOPTION_REQUIRED" &&
			result.HostAdoption == "observed" {
			result.Code = "SHARED_PROTECTED_EXECUTION_UNVERIFIED"
		}
		if *installService && result.Code == "SHARED_PROTECTED_EXECUTION_UNVERIFIED" {
			result.ServiceTemplate, err = installSharedService(a.Process)
		}
		if err == nil && *prepareWake &&
			result.Code == "SHARED_PROTECTED_EXECUTION_UNVERIFIED" {
			result.WakeDestination, err = prepareSharedWakeDestination(a.API,
				plan.Repository.FullName, *plan.Execution)
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

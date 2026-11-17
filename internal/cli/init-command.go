package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
)

func (a App) runInit(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "Public OWNER/REPO")
	agent := flags.String("agent", "", "Managed Agent: codex or claude")
	apply := flags.Bool("apply", false, "Create reviewed installation PR and labels")
	runnerPath := flags.String("runner-directory", "", "Private Runner directory")
	runnerName := flags.String("runner-name", "", "Name of the repository Runner")
	executionRepo := flags.String("execution-repo", "", "Shared execution repository")
	installService := flags.Bool("install-shared-service", false,
		"Install the verified shared Runner unit without starting it")
	startServices := flags.Bool("start-shared-services", false,
		"Install and start the two adopted shared Runner services")
	prepareWake := flags.Bool("prepare-shared-wake", false,
		"Prepare the verified shared Wake destination without activating it")
	consentManaged := flags.Bool("consent-managed-login", false,
		"Authorize the selected GitHub login and dedicated managed host setup")
	guardHandoff := a.linuxHandoffGuard(flags)
	a.handoffCheck = guardHandoff
	quick := addQuickFlags(flags)
	if flags.Parse(args) != nil || flags.NArg() != 0 || *name == "" {
		return errors.New("INVALID_INIT_ARGUMENTS")
	}
	explicitAgent := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "agent" {
			explicitAgent = true
		}
	})
	if err := validateAgentOption(*agent, explicitAgent); err != nil {
		return err
	}
	if explicitAgent && (quick.used() || *runnerPath != "" ||
		*runnerName != "" || *installService || *startServices || *prepareWake) {
		return errors.New("AGENT_SELECTION_REQUIRES_MANAGED_INIT")
	}
	if err := guardHandoff(); err != nil {
		return err
	}
	if *startServices {
		*installService = true
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
	if *installService && (host == nil && !*startServices ||
		*runnerPath != "" || *runnerName != "") {
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
		!legacyHost && nativeLinuxTarget() {
		*executionRepo = *name
		defaultShared = true
	}
	_, native := a.Process.(NativeProcess)
	recipe := "issue"
	if native && nativeLinuxTarget() && *executionRepo != "" &&
		(defaultShared || host != nil) && !*installService && !*prepareWake {
		recipe = "issue-managed"
	}
	plan, err := planInitRecipe(a.API, *name, *executionRepo, recipe)
	if defaultShared && errors.Is(err, errCoreSharedDestinationUnsupported) {
		// Avoid installing legacy callers on a fresh shared host. Those files
		// would conflict with the later installation of the shared candidate.
		return &CommandError{Code: "PINNED_CORE_SHARED_DESTINATION_UNSUPPORTED", Exit: 2}
	}
	if err != nil {
		return err
	}
	if explicitAgent && (legacyHost || plan.Execution == nil || !nativeLinuxTarget()) {
		return errors.New("AGENT_SELECTION_REQUIRES_MANAGED_INIT")
	}
	if plan.Execution != nil && nativeLinuxTarget() &&
		(explicitAgent || native || a.agents != nil) {
		var installed *agentBinding
		if host != nil {
			installed = host.Agent
		} else {
			installed, err = managedIntentAgent(plan)
			if err != nil {
				return err
			}
			if installed == nil && managedIntentAvailable(plan.Repository) &&
				explicitAgent && *agent != "codex" {
				return errors.New("AGENT_MISMATCH_USE_RUNNER_REBUILD_WITH_AGENT")
			}
		}
		if host != nil && installed == nil {
			return errors.New("INSTALLED_AGENT_READBACK_REQUIRED")
		}
		binding, err := selectAgent(*agent, explicitAgent, installed, a.agentCapability)
		if err != nil {
			return err
		}
		plan.Agent = &binding
	}
	if *installService && (plan.Execution == nil ||
		plan.Repository.ID != plan.Execution.RepositoryID) {
		return errors.New("SHARED_SERVICE_REQUIRES_EXECUTION_REPOSITORY")
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
	if nativeLinuxTarget() && plan.Execution != nil {
		preview["linux_target"] = managedTargetPreview(a.API, a.Process)
	}
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
	unlock, err := lockSharedInit(plan)
	if err != nil {
		return err
	}
	defer unlock()
	if err := guardHandoff(); err != nil {
		return err
	}
	managedPeer := host != nil && len(plan.Files) > 0
	managedResume := native && nativeLinuxTarget() &&
		managedIntentAvailable(plan.Repository)
	managed := native && nativeLinuxTarget() && plan.Execution != nil &&
		(defaultShared || managedPeer || managedResume || (*consentManaged && host != nil)) &&
		!*installService && !*prepareWake && runner == nil
	if managed {
		return a.runManagedInit(plan, *consentManaged, out)
	}
	if *consentManaged {
		return errors.New("MANAGED_INSTALL_OPTIONS_CONFLICT")
	}
	cacheProcess := a.Process
	if *startServices {
		cacheProcess, err = prepareSharedNode(a.API, cacheProcess)
		if err != nil {
			return err
		}
	}
	cache, err := installCoreCache(a.API, cacheProcess)
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
	if err := guardHandoff(); err != nil {
		return err
	}
	result, err := applyInit(a.API, plan)
	if err == nil && host == nil && nativeLinuxTarget() &&
		plan.Execution != nil && plan.Execution.RepositoryID == plan.Repository.ID &&
		result.Code == "SHARED_HOST_ADOPTION_REQUIRED" {
		result.RunnerPackages, err = a.stageSharedRunnerPackages(plan)
		if err == nil {
			result.RunnerSlots, err = a.registerInitSharedSlots(plan, result.RunnerPackages)
		}
		if err == nil && *startServices {
			result.CoreDeployment, err = deploySharedCore(a.API, a.Process)
		}
		if err == nil && *startServices {
			host, err = adoptInitialSharedHost(a.Process, plan, result.RunnerSlots)
			registrations = result.RunnerSlots
		}
	}
	if err == nil && host != nil {
		peer := plan.Execution != nil && plan.Repository.ID != plan.Execution.RepositoryID
		result.HostAdoption = host.adoptionStatus(plan.Repository.ID)
		if result.Code == "SHARED_HOST_ADOPTION_REQUIRED" &&
			result.HostAdoption == "required" && peer {
			err = requireInactiveSharedWake(a.API, plan.Repository.FullName)
			if err == nil {
				err = adoptSharedPeer(a.Process, *host, plan)
			}
			if err == nil {
				result.HostAdoption = "observed"
			}
		}
		if result.Code == "SHARED_HOST_ADOPTION_REQUIRED" &&
			result.HostAdoption == "observed" {
			result.Code = "SHARED_PROTECTED_EXECUTION_UNVERIFIED"
		}
		if err == nil && *installService &&
			result.Code == "SHARED_PROTECTED_EXECUTION_UNVERIFIED" {
			var sources []string
			sources, err = a.readStagedSharedSlots(plan, registrations)
			if err == nil {
				result.ServiceTemplate, err = installSharedService(a.Process, sources,
					*startServices)
			}
			if err == nil {
				err = retireStagedRegistration(sources)
			}
			if err == nil && *startServices {
				result.RunnerSlots, err = readStartedSharedRunners(a.Context, a.API, *host)
			}
		}
		if err == nil && (*prepareWake || peer) &&
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

package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (a App) rebuildOperations() rebuildOperations {
	var selected *upgradeSource
	var migration nativeMigrationOperations
	var execution SharedExecution
	host := a.managedHostOperations()
	host.activate = a.activateReplacementNative
	// Without the owner's credential for this run, a Claude setup refuses.
	host.setupAgent = rebuildAgentSetup(host.setupAgent, nil, nil)
	host.adopt = func(plan InitPlan, runners []sharedRegistration) (*sharedHost, error) {
		pins, err := currentRepositoryProducers(a.API, plan.Repository.FullName)
		if err != nil {
			return nil, err
		}
		return adoptInitialSharedHostWithPins(a.Process, plan, runners, pins)
	}
	host.adoptPeer = func(shared sharedHost, plan InitPlan) error {
		pins, err := currentRepositoryProducers(a.API, plan.Repository.FullName)
		if err != nil {
			return err
		}
		return adoptSharedPeerWithAction(a.Process, shared, plan, pins.Action.Commit)
	}
	return rebuildOperations{
		snapshot:   func() (rebuildSnapshot, error) { return readRebuildSnapshot(a.Process) },
		compatible: a.requireRebuildProducers,
		capability: a.agentCapability,
		maintenance: func() error {
			host, err := discoverSharedHost(a.Process)
			if err != nil || host == nil {
				return errors.New("REBUILD_ADOPTED_MANAGED_POOL_REQUIRED")
			}
			execution = host.SharedExecution
			target, err := a.selectManagedNativeSource(*host, "latest")
			if err != nil {
				return err
			}
			selected = &target
			if err := a.prepareCoreMaintenance(target.Producers); err != nil {
				return err
			}
			migration, err = a.preparedNativeMigrationOperations(target)
			return err
		},
		source: func(pins Producers) error {
			if err := a.maintainNativeCoreSource(pins, execution); err != nil {
				return err
			}
			var err error
			migration, err = a.preparedNativeMigrationOperations(*selected)
			return err
		},
		core: func(operation string) ([]byte, error) {
			// Advertised commands are compared, never executed as input.
			if operation != "retire-shared-host" &&
				operation != "activate-shared-replacement" &&
				operation != "complete-installed-lifecycle" {
				return nil, errors.New("INVALID_REBUILD_CORE_OPERATION")
			}
			return runInstalledSharedAdmin(rebuildResultProcess(a.Process), nil, operation)
		},
		migrate: func(host sharedHost) error {
			_, _, err := migrateNativeConsumers(a.API, host, "latest",
				migration, false)
			return err
		},
		plan: func(target preparationRepository, intent *rebuildIntent) (InitPlan, error) {
			repo, user, err := repository(a.API, target.Repository, true)
			if err != nil || repo.ID != target.ID || user.ID != intent.Old.MemberID {
				return InitPlan{}, errors.New("REBUILD_REPOSITORY_AUTHORIZATION_REQUIRED")
			}
			plan, err := planCurrentInitRecipe(a.API, repo.FullName, intent.Old.Repository,
				"issue")
			if err != nil && strings.HasPrefix(err.Error(), "EXISTING_FILE_CONFLICT:") {
				// Preview recognizes a migratable artifact installation. Apply runs
				// migration before it obtains any destructive retirement authority.
				plan, err = planCurrentInitRecipe(a.API, repo.FullName, intent.Old.Repository,
					"issue-managed")
			}
			if err != nil || len(plan.Files) != 0 || plan.Execution == nil ||
				*plan.Execution != intent.Old.SharedExecution {
				return plan, errors.New("REBUILD_VERIFIED_MANAGED_INSTALLATION_REQUIRED")
			}
			plan.Agent = &intent.Agent
			return plan, nil
		},
		prepare: func(plan InitPlan) error {
			provider := "codex"
			if plan.Agent != nil {
				provider = plan.Agent.Provider
			}
			return prepareReplacementResources(a.Process, provider)
		},
		stage:    a.stageReplacementSlots,
		packages: a.verifyReplacementPackages,
		register: a.registerReplacementSlots,
		host:     host,
	}
}

func prepareReplacementResources(process Process, provider string) error {
	node, _, err := trustedSharedCore()
	if err != nil {
		return err
	}
	const entry = "/opt/nibfin/core/bin/nibfin-prepare-shared-host.mjs"
	const env = "/usr/bin/env"
	for _, path := range []string{entry, env} {
		if err := requireRootOwnedHostPath(path); err != nil {
			return err
		}
	}
	args := append([]string{"-i", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", node, entry},
		replacementPreparationArguments(provider)...)
	output, err := runSharedAdministrator(process, env, args, nil)
	if err != nil {
		return errors.New("REBUILD_RESOURCE_PREPARATION_UNVERIFIED")
	}
	return decodeReplacementPreparation(output, provider)
}

// Claude sends Core's advertised Claude vector, checked before any retirement.
func replacementPreparationArguments(provider string) []string {
	if provider == "claude" {
		return slices.Clone(claudePreparationArguments)
	}
	return []string{"--consent-resource-preparation"}
}

func decodeReplacementPreparation(output []byte, provider string) error {
	var result struct {
		Status        string `json:"status"`
		Code          string `json:"code"`
		Profile       string `json:"profile"`
		Adoption      *bool  `json:"adoption"`
		ServicesReady *bool  `json:"servicesReady"`
		Images        struct {
			Agent       string `json:"agent"`
			Hooks       string `json:"hooks"`
			ClaudeAgent string `json:"claudeAgent"`
		} `json:"images"`
	}
	if len(output) > 16384 || json.Unmarshal(output, &result) != nil ||
		result.Status != "prepared" || result.Code != "MANAGED_RESOURCES_PREPARED" ||
		result.Profile != "/etc/nibfin/shared-host-profile.json" ||
		result.Adoption == nil || *result.Adoption ||
		result.ServicesReady == nil || *result.ServicesReady {
		return errors.New("REBUILD_RESOURCE_PREPARATION_UNVERIFIED")
	}
	images := []string{result.Images.Agent, result.Images.Hooks}
	if provider == "claude" {
		// Codex keeps ignoring this key; a Claude result must carry it.
		images = append(images, result.Images.ClaudeAgent)
	}
	for _, image := range images {
		if !strings.HasPrefix(image, "sha256:") ||
			!digestPattern.MatchString(strings.TrimPrefix(image, "sha256:")) {
			return errors.New("REBUILD_RESOURCE_PREPARATION_UNVERIFIED")
		}
	}
	return nil
}

func replacementPackageParent(plan InitPlan, app RunnerApplication,
	id string) (string, error) {
	if !rebuildID.MatchString(id) {
		return "", errors.New("REBUILD_RETIREMENT_ID_REQUIRED")
	}
	parent, err := sharedSlotsCacheDirectory(plan, app)
	if err != nil {
		return "", err
	}
	// A new operation namespace avoids confusing old installer registrations with
	// fresh ones. Its identity is Core's retirement ID, never a source version.
	parent = filepath.Join(parent, "replacement-"+id)
	if err := os.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err := requirePrivateRunnerPaths([]string{parent}); err != nil {
		return "", err
	}
	return parent, nil
}

func (a App) stageReplacementSlots(plan InitPlan, i rebuildIntent) ([]string, error) {
	target, err := trustedSharedTarget()
	if err != nil {
		return nil, err
	}
	app, err := selectSharedRunner(a.API, target)
	if err != nil {
		return nil, err
	}
	parent, err := replacementPackageParent(plan, app, i.RetirementID)
	if err != nil {
		return nil, err
	}
	paths, err := a.stageRunnerSlots(plan, parent, app)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if err := verifySharedRunnerExecutables(path, target); err != nil {
			return nil, err
		}
	}
	if len(i.Paths) != 0 && !slices.Equal(i.Paths, paths) {
		return nil, errors.New("REBUILD_STAGING_IDENTITY_CHANGED")
	}
	return paths, nil
}

func removePrivateRebuildIntent(path string) error {
	file, err := privateInstallFile(path, os.O_RDONLY)
	if err != nil {
		return errors.New("REBUILD_INTENT_CLEANUP_REQUIRED")
	}
	file.Close()
	if err := os.Remove(path); err != nil {
		return errors.New("REBUILD_INTENT_CLEANUP_REQUIRED")
	}
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func (a App) verifyReplacementPackages(plan InitPlan, i rebuildIntent) error {
	target, err := trustedSharedTarget()
	if err != nil {
		return err
	}
	app, err := selectSharedRunner(a.API, target)
	if err != nil {
		return err
	}
	parent, err := replacementPackageParent(plan, app, i.RetirementID)
	if err != nil {
		return err
	}
	if len(i.Paths) != 2 || len(i.Fresh) != 2 {
		return errors.New("REBUILD_STAGING_IDENTITY_CHANGED")
	}
	for n, path := range i.Paths {
		name := sharedRunnerSlotName(plan.Repository.ID, plan.Member.ID, n+1)
		if path != filepath.Join(parent, name) {
			return errors.New("REBUILD_STAGING_IDENTITY_CHANGED")
		}
		record, err := readRunnerRecord(path, plan.Repository, plan.Member)
		if err != nil || record.State != "registered" || record.Application != app ||
			record.RunnerID != i.Fresh[n].ID || record.Name != i.Fresh[n].Name ||
			record.Name != name || localRunnerMatches(path, record) != nil {
			return errors.New("REBUILD_STAGED_REGISTRATION_CHANGED")
		}
		if err := verifyRunnerFiles(path, app); err != nil {
			return err
		}
	}
	return nil
}

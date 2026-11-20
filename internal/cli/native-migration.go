package cli

import (
	"encoding/json"
	"errors"
	"slices"
)

type nativeMigrationOperations struct {
	target        func(sharedHost, string) (upgradeSource, error)
	selected      *upgradeSource
	sourcePending bool
	bindings      func() (relayAdoptions, error)
	installed     func(InitPlan, Producers) (*sharedHost, []sharedRegistration, error)
	state         func(InitPlan) (*nativeWakeSetup, func() error, func(), error)
	settled       func(InitPlan) error
	refresh       func(InitPlan, sharedHost) error
	retire        func() ([]byte, error)
	complete      func() ([]byte, error)
}

type nativeMigrationResult struct {
	Repository string      `json:"repository"`
	Status     string      `json:"status"`
	Review     string      `json:"review,omitempty"`
	Proposal   *InitResult `json:"proposal,omitempty"`
}

type nativeSourceRequired struct{ Producers Producers }

func (*nativeSourceRequired) Error() string {
	return "NATIVE_INSTALL_HOST_SOURCE_REQUIRES_UPGRADE"
}

// Enumerate actual adoptions, not historical relay configuration or its journal.
// Native peers are still observed; an unfinished peer keeps the shared route.
func migrateNativeConsumers(api API, host sharedHost, version string,
	ops nativeMigrationOperations, complete bool) ([]nativeMigrationResult,
	*nativeContinuation, error) {
	adopted, err := ops.bindings()
	if err != nil {
		return nil, nil, err
	}
	if err := nativeMigrationBindings(adopted, host); err != nil {
		return nil, nil, err
	}
	if ops.selected == nil {
		var target upgradeSource
		if ops.target != nil {
			target, err = ops.target(host, version)
		} else {
			target, err = resolveUpgrade(api, version)
		}
		if err != nil {
			return nil, nil, err
		}
		ops.selected = &target
	}
	results := []nativeMigrationResult{}
	pending := false
	var source *nativeSourceRequired
	for _, origin := range adopted.Origins {
		result, err := migrateNativeConsumer(api, host, origin.Repository, version, ops)
		var needed *nativeSourceRequired
		if errors.As(err, &needed) {
			if source != nil && !sameProducerSources(source.Producers, needed.Producers) {
				return results, nil, errors.New("NATIVE_MIGRATION_SOURCE_INTENTS_DIFFER")
			}
			source = needed
		} else if err != nil {
			return results, nil, err
		}
		results = append(results, result)
		pending = pending || result.Status == "pending"
	}
	if pending {
		return results, nil, &CommandError{Code: "NATIVE_MIGRATION_PR_PENDING", Exit: 2}
	}
	if source != nil {
		return results, nil, source
	}
	current, err := ops.bindings()
	if err != nil || nativeMigrationBindings(current, host) != nil {
		return results, nil, errors.New("NATIVE_MIGRATION_ADOPTIONS_CHANGED")
	}
	output, callErr := ops.retire()
	var retirement struct {
		Version int    `json:"schemaVersion"`
		Status  string `json:"status"`
	}
	if json.Unmarshal(output, &retirement) != nil || retirement.Version != 1 ||
		retirement.Status != "retired" || callErr != nil {
		return results, nil, pendingCoreOperation(output, "NATIVE_MIGRATION_RETIRE_PENDING")
	}
	if !complete {
		return results, nil, nil
	}
	output, callErr = ops.complete()
	continuation, err := decodeNativeContinuation(output, callErr)
	if err != nil {
		return results, nil, err
	}
	if continuation.Status == "pending" {
		return results, &continuation,
			&CommandError{Code: "NATIVE_CONTINUATION_PENDING", Exit: 2}
	}
	return results, &continuation, nil
}

func nativeMigrationBindings(adopted relayAdoptions, host sharedHost) error {
	if adopted.MemberID != host.MemberID ||
		len(adopted.Origins) != len(host.AdoptedRepositoryIDs) {
		return errors.New("NATIVE_MIGRATION_ADOPTIONS_CHANGED")
	}
	seen := map[int64]bool{}
	for _, origin := range adopted.Origins {
		if origin.RepositoryID <= 0 || seen[origin.RepositoryID] ||
			!slices.Contains(host.AdoptedRepositoryIDs, origin.RepositoryID) ||
			!repoPattern.MatchString(origin.Repository) ||
			origin.Event.RepositoryID != origin.RepositoryID ||
			origin.Event.Repository != origin.Repository ||
			origin.Event.MemberID != host.MemberID {
			return errors.New("NATIVE_MIGRATION_ADOPTIONS_CHANGED")
		}
		seen[origin.RepositoryID] = true
	}
	return nil
}

func nativeMigrationPlan(api API, host sharedHost,
	upgrade upgradePlan) (InitPlan, error) {
	repo, user, err := repository(api, upgrade.Repository.FullName, true)
	if err != nil {
		return InitPlan{}, err
	}
	if repo.ID != upgrade.Repository.ID || user.ID != host.MemberID ||
		upgrade.MemberID != host.MemberID || host.adoptionStatus(repo.ID) != "observed" {
		return InitPlan{}, errors.New("NATIVE_MIGRATION_IDENTITY_CHANGED")
	}
	plan := InitPlan{NativeManaged: true, IssueRecipe: "issue", Repository: repo,
		Member: user, Head: upgrade.Base, Execution: &host.SharedExecution,
		Files: []Change{}, Labels: []string{}, Agent: host.Agent}
	if err := nativeWakeAuthority(api, plan); err != nil {
		return plan, err
	}
	return plan, nil
}

func migrateNativeConsumer(api API, host sharedHost, name, version string,
	ops nativeMigrationOperations) (nativeMigrationResult, error) {
	result := nativeMigrationResult{Repository: name, Status: "pending"}
	upgrade, err := migrationUpgradePlan(api, name, version, ops)
	if err != nil {
		return result, err
	}
	if err := nativeTargetStillCurrent(api, upgrade); err != nil {
		return result, err
	}
	plan, err := nativeMigrationPlan(api, host, upgrade)
	if err != nil {
		return result, err
	}
	state, save, unlock, err := ops.state(plan)
	if err != nil {
		return result, err
	}
	defer unlock()
	if state == nil || save == nil {
		return result, errors.New("NATIVE_MIGRATION_STATE_UNAVAILABLE")
	}
	if err := prepareNativeConsumerWake(api, host, version, ops, plan, upgrade,
		state, save); err != nil {
		return result, err
	}
	if len(upgrade.Changes) != 0 {
		proposal, err := applyUpgrade(api, upgrade, version)
		result.Proposal = &proposal
		return result, err
	}
	// Main is authoritative, including a lost PR response or missing local record.
	// Only the same installed sources can be refreshed without host deployment.
	adopted, err := ops.bindings()
	if err != nil {
		return result, err
	}
	if !nativeCoreSourcesMatch(adopted.Pin, upgrade.Target.Producers.Core) ||
		ops.sourcePending {
		result.Status = "source_pending"
		return result, &nativeSourceRequired{upgrade.Target.Producers}
	}
	if err := ops.refresh(plan, host); err != nil {
		return result, err
	}
	installed, runners, err := ops.installed(plan, upgrade.Target.Producers)
	if err != nil || !sameMigrationPool(host, installed) ||
		!sharedRunnersOnline(runners) {
		return result, errors.New("NATIVE_MIGRATION_INSTALLATION_REQUIRES_READBACK")
	}
	if err := observeNativeMigrationSecret(api, host, plan, version, ops,
		state, save); err != nil {
		return result, err
	}
	if err := setManagedPool(api, name); err != nil {
		return result, err
	}
	if ops.settled != nil {
		if err := ops.settled(plan); err != nil {
			return result, err
		}
	}
	result.Status = "ready"
	return result, nil
}

func prepareNativeConsumerWake(api API, host sharedHost, version string,
	ops nativeMigrationOperations, plan InitPlan, upgrade upgradePlan,
	state *nativeWakeSetup, save func() error) error {
	recipes, err := upgradeRecipes(api, upgrade.Current)
	if err != nil {
		return err
	}
	body, err := installedUpgradeFile(api, upgrade, managedIssueWorkflow)
	if err != nil {
		return err
	}
	switch recognizeIssueRecipe(upgrade.Current, plan.Member.ID, recipes, body) {
	case "issue-managed":
		publisher, ok := api.(nativeSecretPublisher)
		if !ok {
			return errors.New("NATIVE_WAKE_SECRET_PUBLICATION_UNAVAILABLE")
		}
		route := func() error {
			return observeMigrationArtifact(api, host, plan, version, ops)
		}
		return prepareNativeWakeWithRoute(api, plan, state, save,
			publisher.publishNativeWakeSecret, route)
	case "issue":
		return prepareMigrationNativeWake(api, host, plan, version, ops, state, save)
	default:
		return errors.New("NATIVE_MIGRATION_CALLER_NOT_RECOGNIZED")
	}
}

// Full recognized source, adopted native identities and the same destination
// authorize credential preparation while the existing artifact route stays live.
func observeMigrationArtifact(api API, host sharedHost, plan InitPlan, version string,
	ops nativeMigrationOperations) error {
	adopted, err := ops.bindings()
	if err != nil {
		return err
	}
	if err := nativeMigrationBindings(adopted, host); err != nil {
		return err
	}
	current, err := migrationUpgradePlan(api, plan.Repository.FullName, version, ops)
	if err != nil {
		return err
	}
	if _, err := nativeMigrationPlan(api, host, current); err != nil {
		return err
	}
	recipes, err := upgradeRecipes(api, current.Current)
	if err != nil {
		return err
	}
	expected := templatesForRecipe(current.Current, plan.Member.ID, recipes,
		"issue-managed")[managedIssueWorkflow]
	for _, origin := range adopted.Origins {
		if origin.RepositoryID != plan.Repository.ID {
			continue
		}
		event := origin.Event
		if event.WorkflowPath != managedIssueWorkflow || event.WorkflowID <= 0 ||
			event.Reusable.Repository != current.Current.Core.Repository ||
			event.Reusable.Path != ".github/workflows/issue.yml" ||
			event.Reusable.Commit != current.Current.Core.Commit {
			return errors.New("NATIVE_MIGRATION_ARTIFACT_BINDING_CHANGED")
		}
		binding := relayOrigin{origin.Repository, origin.RepositoryID,
			current.Repository.DefaultBranch, relayWorkflow{event.WorkflowID,
				event.WorkflowPath, event.SourceCommit, event.WorkflowBlob}}
		if err := verifyRelayOrigin(api, binding, expected); err != nil {
			return err
		}
		present, err := readSharedWakeDestination(api, plan.Repository.FullName,
			*plan.Execution)
		if err != nil {
			return err
		}
		if !present {
			return errors.New("NATIVE_MIGRATION_EXISTING_DESTINATION_REQUIRED")
		}
		_, _, err = managedPool(api, plan.Repository.FullName)
		return err
	}
	return errors.New("NATIVE_MIGRATION_TARGET_NOT_ADOPTED")
}

func migrationUpgradePlan(api API, name, version string,
	ops nativeMigrationOperations) (upgradePlan, error) {
	if ops.selected != nil {
		return planNativeUpgradeTarget(api, name, version, *ops.selected)
	}
	return planNativeUpgrade(api, name, version)
}

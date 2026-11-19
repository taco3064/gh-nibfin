package cli

import (
	"errors"
	"slices"
)

func sameMigrationPool(expected sharedHost, actual *sharedHost) bool {
	if !expected.samePool(actual) ||
		len(expected.AdoptedRepositoryIDs) != len(actual.AdoptedRepositoryIDs) {
		return false
	}
	for _, id := range expected.AdoptedRepositoryIDs {
		if !slices.Contains(actual.AdoptedRepositoryIDs, id) {
			return false
		}
	}
	return true
}

func observeMigrationNative(api API, host sharedHost, plan InitPlan, version string,
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
	if current.issueRecipe != "issue" {
		return errors.New("NATIVE_MIGRATION_CALLER_NOT_RECOGNIZED")
	}
	observed, err := nativeMigrationPlan(api, host, current)
	if err != nil {
		return err
	}
	installed, runners, err := ops.installed(observed, current.Current)
	if err != nil {
		return err
	}
	if !sameMigrationPool(host, installed) ||
		!migrationRegistrationsMatch(*installed, runners) {
		return errors.New("NATIVE_MIGRATION_INSTALLATION_REQUIRES_READBACK")
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

func migrationRegistrationsMatch(host sharedHost, runners []sharedRegistration) bool {
	if len(host.Runners) == 0 || len(host.Runners) != len(runners) {
		return false
	}
	for index, registered := range host.Runners {
		if runners[index].ID != registered.ID || runners[index].Name != registered.Name {
			return false
		}
	}
	return true
}

func prepareMigrationNativeWake(api API, host sharedHost, plan InitPlan,
	version string, ops nativeMigrationOperations, state *nativeWakeSetup,
	save func() error) error {
	_, present, err := nativeWakeSecretMetadata(api, plan.Repository.FullName)
	if err != nil {
		return err
	}
	route := func() error { return observeMigrationNative(api, host, plan, version, ops) }
	if err := route(); err != nil {
		return err
	}
	if present && (state.SecretState == "" || state.SecretState == "confirmed" ||
		state.SecretState == "observed") {
		return observeNativeMigrationSecret(api, host, plan, version, ops, state, save)
	}
	if !present {
		// A successful current read proves this resource is unfinished. The
		// complete native repair observation authorizes its normal upsert again,
		// irrespective of an obsolete local publication state.
		state.SecretState, state.Secret = "pending", nativeSecretMetadata{}
		if err := save(); err != nil {
			return err
		}
	}
	publisher, ok := api.(nativeSecretPublisher)
	if !ok {
		return errors.New("NATIVE_WAKE_SECRET_PUBLICATION_UNAVAILABLE")
	}
	return prepareNativeWakeWithRoute(api, plan, state, save,
		publisher.publishNativeWakeSecret, route)
}

// Current complete native configuration supersedes stale local metadata. This
// records presence only, never credential equality, validity or write ownership.
func observeNativeMigrationSecret(api API, host sharedHost, plan InitPlan,
	version string, ops nativeMigrationOperations, state *nativeWakeSetup,
	save func() error) error {
	if err := observeMigrationNative(api, host, plan, version, ops); err != nil {
		return err
	}
	secret, present, err := nativeWakeSecretMetadata(api, plan.Repository.FullName)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("NATIVE_WAKE_SECRET_REQUIRES_READBACK")
	}
	state.SecretState, state.Secret = "observed", secret
	return save()
}

package cli

import "errors"

// This is read-only installation evidence, shared by recovery and activation.
// Secret metadata remains configuration evidence, never ownership or equality.
func readNativeInstallation(api API, plan InitPlan,
	discover func() (*sharedHost, error)) (*sharedHost, []sharedRegistration, error) {
	if err := nativeWakeAuthority(api, plan); err != nil {
		return nil, nil, err
	}
	current, err := planInitRecipe(api, plan.Repository.FullName,
		plan.Execution.Repository, "issue")
	if err != nil {
		return nil, nil, err
	}
	if len(current.Files) != 0 || current.Repository.ID != plan.Repository.ID ||
		current.Repository.DefaultBranch != plan.Repository.DefaultBranch ||
		current.Member.ID != plan.Member.ID ||
		!sameSharedExecution(current.Execution, plan.Execution) {
		return nil, nil, errors.New("NATIVE_WAKE_INSTALLATION_CHANGED")
	}
	if discover == nil {
		return nil, nil, errors.New("NATIVE_WAKE_HOST_OBSERVATION_UNAVAILABLE")
	}
	host, err := discover()
	if err != nil || host == nil {
		return nil, nil, err
	}
	if host.MemberID != plan.Member.ID || host.SharedExecution != *plan.Execution ||
		host.adoptionStatus(plan.Repository.ID) != "observed" {
		return nil, nil, errors.New("NATIVE_WAKE_HOST_BINDING_CHANGED")
	}
	runners, err := verifySharedHost(api, *host)
	if err != nil {
		return nil, nil, err
	}
	present, err := readSharedWakeDestination(api, plan.Repository.FullName,
		*plan.Execution)
	if err != nil || !present {
		return nil, nil, err
	}
	if _, _, err := managedPool(api, plan.Repository.FullName); err != nil {
		return nil, nil, err
	}
	return host, runners, nil
}

func recoverNativeConfiguration(api API, plan InitPlan,
	discover func() (*sharedHost, error), state *nativeWakeSetup, save func() error) error {
	host, _, err := readNativeInstallation(api, plan, discover)
	if err != nil || host == nil {
		return err
	}
	secret, present, err := nativeWakeSecretMetadata(api, plan.Repository.FullName)
	if err != nil || !present {
		return err
	}
	// Reuse the observed installed configuration; do not attribute a previous
	// secret write, enroll a same-named secret alone, or assert credential validity.
	state.SecretState, state.Secret = "observed", secret
	return save()
}

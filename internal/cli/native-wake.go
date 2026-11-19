package cli

import (
	"errors"
	"strings"
)

const nativeWakeSecret = "NIBFIN_WAKE_TOKEN"

type nativeSecretMetadata struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Nonsecret progress distinguishes successful writes from observed installation
// configuration. Metadata never establishes credential equality or validity.
type nativeWakeSetup struct {
	SecretState string               `json:"secret_state"`
	Secret      nativeSecretMetadata `json:"secret"`
}

type nativeSecretPublisher interface {
	publishNativeWakeSecret(string) error
}

func nativeWakeAuthority(api API, plan InitPlan) error {
	if plan.Execution == nil || initIssueRecipe(plan) != "issue" {
		return errors.New("NATIVE_WAKE_PLAN_REQUIRED")
	}
	for _, expected := range []SharedExecution{
		{plan.Repository.FullName, plan.Repository.ID}, *plan.Execution,
	} {
		repo, user, err := repository(api, expected.Repository, true)
		if err != nil {
			return err
		}
		if repo.ID != expected.RepositoryID || user.ID != plan.Member.ID ||
			!strings.EqualFold(user.Login, plan.Member.Login) {
			return errors.New("NATIVE_WAKE_IDENTITY_CHANGED")
		}
	}
	return nil
}

func nativeWakeSecretMetadata(api API, repo string) (nativeSecretMetadata, bool, error) {
	var secret nativeSecretMetadata
	err := api.Call("GET", "repos/"+repo+"/actions/secrets/"+nativeWakeSecret,
		nil, &secret)
	if err != nil && err.Error() == "NOT_FOUND" {
		return secret, false, nil
	}
	if err != nil {
		return secret, false, err
	}
	if secret.Name != nativeWakeSecret || secret.CreatedAt == "" || secret.UpdatedAt == "" {
		return secret, false, errors.New("NATIVE_WAKE_SECRET_OBSERVATION_INVALID")
	}
	return secret, true, nil
}

func prepareNativeWake(api API, plan InitPlan, state *nativeWakeSetup,
	save func() error, publish func(string) error) error {
	return prepareNativeWakeWithRoute(api, plan, state, save, publish, func() error {
		return requireInactiveSharedWake(api, plan.Repository.FullName)
	})
}

// Both installation and migration use the same durable provider write protocol.
// The caller supplies code-owned route observation, never a user bypass flag.
func prepareNativeWakeWithRoute(api API, plan InitPlan, state *nativeWakeSetup,
	save func() error, publish func(string) error, route func() error) error {
	if err := nativeWakeAuthority(api, plan); err != nil {
		return err
	}
	secret, exists, err := nativeWakeSecretMetadata(api, plan.Repository.FullName)
	if err != nil {
		return err
	}
	if state.SecretState == "confirmed" || state.SecretState == "observed" {
		if !exists || state.Secret != secret {
			return errors.New("NATIVE_WAKE_SECRET_CHANGED")
		}
	} else if state.SecretState == "published" {
		// The successful publication is already durable. Retry only observation,
		// never turn an unsuccessful metadata read into another upsert.
		return observePublishedNativeWake(api, plan, state, save, secret, exists)
	} else {
		if exists && state.SecretState != "pending" {
			return errors.New("NATIVE_WAKE_EXISTING_SECRET_CONFLICT")
		}
		if state.SecretState != "" && state.SecretState != "pending" {
			return errors.New("NATIVE_WAKE_SETUP_INVALID")
		}
		if route == nil {
			return errors.New("NATIVE_WAKE_ROUTE_OBSERVATION_REQUIRED")
		}
		if err := route(); err != nil {
			return err
		}
		if _, err := prepareSharedWakeDestination(api, plan.Repository.FullName,
			*plan.Execution); err != nil {
			return err
		}
		// The private, repository/member-bound intent authorizes this unfinished
		// setup. Revalidate its disabled route and target, then use GitHub's normal
		// upsert again. Only this attempt's successful response confirms it;
		// metadata never attributes the previous write or proves secret equality.
		state.SecretState = "pending"
		if err := save(); err != nil {
			return err
		}
		if err := nativeWakeAuthority(api, plan); err != nil {
			return err
		}
		if err := route(); err != nil {
			return err
		}
		if err := publish(plan.Repository.FullName); err != nil {
			return errors.New("NATIVE_WAKE_SECRET_WRITE_UNKNOWN")
		}
		state.SecretState = "published"
		if err := save(); err != nil {
			return err
		}
		secret, exists, err = nativeWakeSecretMetadata(api, plan.Repository.FullName)
		if err != nil {
			return err
		}
		return observePublishedNativeWake(api, plan, state, save, secret, exists)
	}
	_, err = prepareSharedWakeDestination(api, plan.Repository.FullName, *plan.Execution)
	return err
}

func observePublishedNativeWake(api API, plan InitPlan, state *nativeWakeSetup,
	save func() error, secret nativeSecretMetadata, exists bool) error {
	if !exists {
		return errors.New("NATIVE_WAKE_SECRET_CHANGED")
	}
	state.SecretState, state.Secret = "confirmed", secret
	if err := save(); err != nil {
		return err
	}
	_, err := prepareSharedWakeDestination(api, plan.Repository.FullName, *plan.Execution)
	return err
}

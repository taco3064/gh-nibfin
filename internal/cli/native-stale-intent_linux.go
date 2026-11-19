package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func (a App) reuseObservedNativeIntent(plan InitPlan, path string,
	ops managedHostOperations) (*managedInstallIntent, error) {
	replacement := managedInstallIntent{Plan: plan, Pins: Pins(), Native: &nativeWakeSetup{}}
	err := recoverNativeConfiguration(a.API, plan, ops.discover, replacement.Native,
		func() error { return saveManagedIntent(path, replacement) })
	if err != nil || replacement.Native.SecretState != "observed" {
		return nil, err
	}
	return &replacement, nil
}

func (a App) resumeStaleNativeIntent(ctx context.Context, plan InitPlan, token []byte,
	out io.Writer, ops managedHostOperations, path string,
	intent managedInstallIntent) error {
	if len(plan.Files) == 0 {
		replacement, err := a.reuseObservedNativeIntent(plan, path, ops)
		if err != nil {
			return err
		}
		if replacement != nil {
			return a.continueNativeManagedInit(ctx, replacement.Plan, token, out, ops,
				path, *replacement)
		}
	}
	// A private historical record cannot authorize a different root package.
	// It can retain this authorized operation's original provider/PR work.
	pr, err := observeInstallIntent(a.API, &intent)
	if err != nil {
		return err
	}
	if pr.Number != 0 {
		if err := verifyInstallPR(a.API, &intent, pr); err != nil {
			return err
		}
		if pr.Merged {
			return a.continueCompletedNativePR(ctx, plan, token, out, ops, path, intent)
		}
	}
	if err := verifyProducers(a.API, intent.Pins); err != nil {
		return err
	}
	if pr.Number == 0 {
		original, err := planInitRecipeWithPins(a.API, plan.Repository.FullName,
			plan.Execution.Repository, "issue", intent.Pins)
		if err != nil {
			return err
		}
		original.Agent, original.NativeManaged = intent.Plan.Agent, true
		if !validManagedIntentIdentity(intent, original) {
			return errors.New("MANAGED_INSTALL_INTENT_CHANGED")
		}
		intent.Plan, intent.PR, intent.Head = original, 0, ""
	}
	if intent.Native == nil {
		intent.Native = &nativeWakeSetup{}
	}
	publisher, ok := a.API.(nativeSecretPublisher)
	if !ok {
		return errors.New("NATIVE_WAKE_SECRET_PUBLICATION_UNAVAILABLE")
	}
	save := func() error { return saveManagedIntent(path, intent) }
	if err := save(); err != nil {
		return err
	}
	if err := prepareNativeWake(a.API, intent.Plan, intent.Native, save,
		publisher.publishNativeWakeSecret); err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	if err := waitManagedInstall(ctx, a.API, &intent, save, func(url string) error {
		return encoder.Encode(InitResult{Status: "pending",
			Code: "MANAGED_INSTALL_PR_READ_BACK", PR: url})
	}); err != nil {
		return err
	}
	return a.continueCompletedNativePR(ctx, plan, token, out, ops, path, intent)
}

func (a App) continueCompletedNativePR(ctx context.Context, plan InitPlan, token []byte,
	out io.Writer, ops managedHostOperations, path string,
	intent managedInstallIntent) error {
	current, err := planInitRecipe(a.API, plan.Repository.FullName,
		plan.Execution.Repository, "issue")
	if err != nil {
		if strings.Contains(err.Error(), "EXISTING_FILE_CONFLICT") ||
			strings.Contains(err.Error(), "REQUIRES_EXPLICIT_UPGRADE") {
			return &CommandError{Code: "NATIVE_INSTALL_SOURCE_TRANSITION_REQUIRES_UPGRADE",
				Exit: 2}
		}
		return err
	}
	current.Agent, current.NativeManaged = plan.Agent, true
	if len(current.Files) == 0 {
		replacement, err := a.reuseObservedNativeIntent(current, path, ops)
		if err != nil {
			return err
		}
		if replacement != nil {
			return a.continueNativeManagedInit(ctx, replacement.Plan, token, out, ops,
				path, *replacement)
		}
		if sameProducerSources(intent.Pins, Pins()) {
			// The original PR is confirmed and current callers/source agree. Only
			// retrieval metadata changed; enter the unfinished host stage using the
			// current authorized plan, never old metadata as root authorization.
			replacement := managedInstallIntent{Plan: current, Pins: Pins(), Native: intent.Native}
			if err := saveManagedIntent(path, replacement); err != nil {
				return err
			}
			return a.continueNativeManagedInit(ctx, current, token, out, ops, path, replacement)
		}
	}
	return &CommandError{Code: "NATIVE_INSTALL_HOST_SOURCE_REQUIRES_UPGRADE", Exit: 2}
}

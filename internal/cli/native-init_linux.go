package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
)

func (a App) resumeNativeManagedInit(ctx context.Context, plan InitPlan, token []byte,
	out io.Writer, ops managedHostOperations) error {
	if err := a.requireHandoff(); err != nil {
		return err
	}
	if plan.Execution == nil || initIssueRecipe(plan) != "issue" {
		return errors.New("NATIVE_WAKE_PLAN_REQUIRED")
	}
	current, err := refreshNativeInitPlan(a.API, plan)
	if err != nil {
		return err
	}
	if current.Member.ID != plan.Member.ID || current.Repository.ID != plan.Repository.ID ||
		!sameSharedExecution(current.Execution, plan.Execution) {
		return errors.New("MANAGED_INSTALL_PLAN_REQUIRES_READBACK")
	}
	current.Agent, current.NativeManaged = plan.Agent, true
	path, err := managedIntentPath(current.Repository)
	if err != nil {
		return err
	}
	unlock, err := installIntentLock(path)
	if err != nil {
		return err
	}
	defer unlock()
	intent, err := loadManagedIntent(path, current)
	if err != nil {
		return err
	}
	if !intent.Plan.NativeManaged || initIssueRecipe(intent.Plan) != "issue" {
		return errors.New("NATIVE_WAKE_EXISTING_INSTALL_REQUIRES_MIGRATION")
	}
	if !sameProducerSources(intent.Pins, managedPlanProducers(current)) {
		return a.resumeStaleNativeIntent(ctx, current, token, out, ops, path, intent)
	}
	return a.continueNativeManagedInit(ctx, current, token, out, ops, path, intent)
}

// The outer invocation owns admission and its single installation lock. Normal
// work and refreshed stale intent continue inside that same admitted operation.
func (a App) continueNativeManagedInit(ctx context.Context, current InitPlan,
	token []byte, out io.Writer, ops managedHostOperations, path string,
	intent managedInstallIntent) error {
	plan := current
	var err error
	if intent.Native == nil {
		intent.Native = &nativeWakeSetup{}
		if len(current.Files) == 0 {
			if err := recoverNativeConfiguration(a.API, current, ops.discover,
				intent.Native, func() error { return saveManagedIntent(path, intent) }); err != nil {
				return err
			}
		}
	}
	publisher, ok := a.API.(nativeSecretPublisher)
	if !ok {
		return errors.New("NATIVE_WAKE_SECRET_PUBLICATION_UNAVAILABLE")
	}
	save := func() error { return saveManagedIntent(path, intent) }
	if err := prepareNativeWake(a.API, current, intent.Native, save,
		publisher.publishNativeWakeSecret); err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	if len(current.Files) != 0 {
		if err := waitManagedInstall(ctx, a.API, &intent, save, func(url string) error {
			return encoder.Encode(InitResult{Status: "pending",
				Code: "MANAGED_INSTALL_PR_READ_BACK", PR: url})
		}); err != nil {
			return err
		}
		current, err = refreshNativeInitPlan(a.API, plan)
		if err != nil || len(current.Files) != 0 ||
			current.Repository.ID != plan.Repository.ID || current.Member.ID != plan.Member.ID ||
			!sameSharedExecution(current.Execution, plan.Execution) {
			return errors.New("MANAGED_INSTALL_PLAN_REQUIRES_READBACK")
		}
		current.Agent, current.NativeManaged = plan.Agent, true
	}
	activate := ops.native
	if activate != nil {
		ops.native = func(ready InitPlan, runners []sharedRegistration) (
			nativeContinuation, error) {
			if err := prepareNativeWake(a.API, ready, intent.Native, save,
				publisher.publishNativeWakeSecret); err != nil {
				return nativeContinuation{}, err
			}
			return activate(ready, runners)
		}
	}
	return a.finishVerifiedManagedInit(ctx, current, token, encoder, ops)
}

func (a App) activateNativeManagedRoute(plan InitPlan,
	runners []sharedRegistration) (nativeContinuation, error) {
	return activateNativeRoute(a.API, plan, runners, nativeActivationOperations{
		discover: func() (*sharedHost, error) { return discoverSharedHost(a.Process) },
		recheck: func(host sharedHost) error {
			return recheckSharedHost(a.Process, a.API, host)
		},
		complete: func() ([]byte, error) {
			return runInstalledSharedAdmin(rebuildResultProcess(a.Process), nil,
				"complete-installed-lifecycle")
		},
	})
}

type nativeActivationOperations struct {
	discover func() (*sharedHost, error)
	recheck  func(sharedHost) error
	complete func() ([]byte, error)
}

func activateNativeRoute(api API, plan InitPlan, runners []sharedRegistration,
	ops nativeActivationOperations) (nativeContinuation, error) {
	if !sharedRunnersOnline(runners) {
		return nativeContinuation{}, errors.New("NATIVE_WAKE_RUNNERS_UNVERIFIED")
	}
	if ops.discover == nil || ops.recheck == nil || ops.complete == nil {
		return nativeContinuation{}, errors.New("NATIVE_WAKE_ACTIVATION_UNAVAILABLE")
	}
	host, observed, err := readNativeInstallation(api, plan, ops.discover)
	if err != nil {
		return nativeContinuation{}, err
	}
	if host == nil || !sharedRunnersOnline(observed) || len(observed) != len(runners) {
		return nativeContinuation{}, errors.New("NATIVE_WAKE_HOST_BINDING_CHANGED")
	}
	for index, runner := range runners {
		if runner.ID != host.Runners[index].ID || runner.Name != host.Runners[index].Name {
			return nativeContinuation{}, errors.New("NATIVE_WAKE_RUNNERS_CHANGED")
		}
	}
	if err := ops.recheck(*host); err != nil {
		return nativeContinuation{}, err
	}
	_, present, err := nativeWakeSecretMetadata(api, plan.Repository.FullName)
	if err != nil || !present {
		return nativeContinuation{}, errors.New("NATIVE_WAKE_SECRET_REQUIRES_READBACK")
	}
	if err := setManagedPool(api, plan.Repository.FullName); err != nil {
		return nativeContinuation{}, err
	}
	output, err := ops.complete()
	result, err := decodeNativeContinuation(output, err)
	if err != nil {
		return nativeContinuation{Status: "pending", Results: []json.RawMessage{},
			Code: "NATIVE_CONTINUATION_REQUIRES_READBACK"}, nil
	}
	return result, nil
}

func publishNativeManagedRoute(encoder installResultEncoder, plan InitPlan,
	runners []sharedRegistration, ops managedHostOperations) error {
	if ops.native == nil {
		return errors.New("NATIVE_WAKE_ACTIVATION_UNAVAILABLE")
	}
	continuation, err := ops.native(plan, runners)
	if err != nil {
		return err
	}
	result := struct {
		Status       string               `json:"status"`
		Code         string               `json:"code"`
		Wake         string               `json:"wake"`
		Credential   string               `json:"credential"`
		Repository   string               `json:"repository"`
		Execution    *SharedExecution     `json:"execution"`
		Runners      []sharedRegistration `json:"runners"`
		Continuation nativeContinuation   `json:"continuation"`
	}{"completed", "NATIVE_ROUTING_CONFIGURED", "configured", "configured_unverified",
		plan.Repository.FullName, plan.Execution, runners, continuation}
	if err := encoder.Encode(result); err != nil {
		return err
	}
	if continuation.Status == "pending" {
		return &CommandError{Code: "NATIVE_CONTINUATION_PENDING", Exit: 2}
	}
	return nil
}

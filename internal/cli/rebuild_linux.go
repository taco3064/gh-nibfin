package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"slices"
)

type rebuildOperations struct {
	snapshot    func() (rebuildSnapshot, error)
	compatible  func(string) error
	capability  func(string) (agentBinding, error)
	core        func(string) ([]byte, error)
	migrate     func(sharedHost) error
	maintenance func() error
	source      func(Producers) error
	plan        func(preparationRepository, *rebuildIntent) (InitPlan, error)
	prepare     func(InitPlan) error
	packages    func(InitPlan, rebuildIntent) error
	stage       func(InitPlan, rebuildIntent) ([]string, error)
	register    func(InitPlan, rebuildIntent, func([]sharedRegistration) error) error
	host        managedHostOperations
	// The kind of the owner's Claude credential for this run; never the value.
	credentialKind string
}

func (a App) runRebuild(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("runner rebuild", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "An adopted public OWNER/REPO")
	agent := flags.String("agent", "", "Requested managed Agent: codex or claude")
	apply := flags.Bool("apply", false, "Apply or resume this pool replacement")
	consent := flags.Bool("consent-rebuild", false,
		"Authorize administrator retirement and replacement of both shared slots")
	if flags.Parse(args) != nil || flags.NArg() != 0 ||
		!repoPattern.MatchString(*name) || validateAgentOption(*agent, true) != nil {
		return errors.New("INVALID_REBUILD_ARGUMENTS_REQUIRE_REPO_AND_AGENT")
	}
	if !nativeSharedHostSupported() {
		return errors.New("REBUILD_REQUIRES_MANAGED_LINUX")
	}
	repo, user, err := repository(a.API, *name, true)
	if err != nil {
		return err
	}
	ops := a.rebuildOperations()
	intent, unmet, err := previewRebuild(repo, user, *agent, ops)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	if err := encoder.Encode(map[string]any{
		"status": "preview", "command": "rebuild", "pool": intent.Old,
		"requested_agent": intent.Agent, "repositories": intent.Repositories,
		"unmet_prerequisites": unmet, "administrator_consent_required": true,
		"activation": "unverified",
	}); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if !*apply {
		return nil
	}
	if !*consent {
		return errors.New("REBUILD_REQUIRES_EXPLICIT_ADMINISTRATOR_CONSENT")
	}
	if len(unmet) != 0 {
		return errors.New("REBUILD_PREREQUISITES_UNMET")
	}
	if os.Getuid() == 0 {
		return errors.New("REBUILD_REQUIRES_INSTALLING_USER")
	}
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	api, err := selectedLogin(ctx, user)
	if err != nil {
		return err
	}
	defer clear(api.token)
	a.API = api
	a.Process = onboardingProcess{ctx: ctx, process: NativeProcess{}}
	ops = a.rebuildOperations()
	// A released replacement only resumes Core's lifecycle and needs no credential.
	if *agent == "claude" && !rebuildContinuationOnly(ops) {
		credential, err := promptRebuildClaudeCredential()
		if err != nil {
			return err
		}
		defer credential.clear()
		ops.bindClaudeCredential(a.Process, credential)
	}
	return a.applyRebuild(repo, user, *agent, api.token, encoder, ops)
}

func previewRebuild(repo Repository, user User, provider string,
	ops rebuildOperations) (rebuildIntent, []string, error) {
	var empty rebuildIntent
	snapshot, err := ops.snapshot()
	if err != nil || !validRebuildSnapshot(snapshot) {
		return empty, nil, errors.New("REBUILD_ADOPTED_MANAGED_POOL_REQUIRED")
	}
	path, err := sharedMutationPath()
	if err != nil {
		return empty, nil, err
	}
	intent, err := readRebuildIntent(path)
	if err != nil {
		return empty, nil, err
	}
	if intent == nil {
		if r := snapshot.Retirement; r != nil &&
			(r.Release != "released" || sameRebuildPool(r.Pool, snapshot.Host)) {
			return empty, nil, errors.New("REBUILD_ORIGINAL_DURABLE_INTENT_REQUIRED")
		}
		intent = &rebuildIntent{Version: 1, Old: *snapshot.Host,
			Repositories: snapshot.Repositories}
	} else if reconcileRebuildSnapshot(*intent, snapshot) != nil ||
		intent.Agent.Provider != provider {
		return empty, nil, errors.New("REBUILD_INTENT_CONFLICT")
	}
	if user.ID != intent.Old.MemberID ||
		!slices.Contains(intent.Repositories, preparationRepository{repo.FullName, repo.ID}) {
		return empty, nil, errors.New("REBUILD_REPOSITORY_MEMBER_CHANGED")
	}
	unmet := []string{}
	if releasedRebuild(*intent, snapshot) {
		return *intent, unmet, nil
	}
	binding, err := ops.capability(provider)
	if err != nil || !binding.valid() || binding.Provider != provider {
		unmet = append(unmet, "AGENT_COMPOSITION_UNAVAILABLE")
		// Preview still reports the requested provider without inventing a model.
		binding = agentBinding{Provider: provider}
	}
	if intent.Agent.valid() {
		// A saved Claude credential kind is intent, not probe output; keep it.
		if binding != intent.Agent.composition() {
			return empty, nil, errors.New("REBUILD_TARGET_BINDING_CHANGED")
		}
		binding.CredentialKind = intent.Agent.CredentialKind
	}
	intent.Agent = binding
	if ops.compatible(provider) != nil {
		unmet = append(unmet, "REBUILD_COMPATIBLE_VERIFIED_PRODUCERS_REQUIRED")
	}
	for _, target := range intent.Repositories {
		if _, err := ops.plan(target, intent); err != nil {
			unmet = append(unmet, "REBUILD_REPOSITORY_AUTHORIZATION_OR_INSTALL_REQUIRED")
			break
		}
	}
	return *intent, unmet, nil
}

func (a App) applyRebuild(repo Repository, user User, provider string, token []byte,
	encoder installResultEncoder, ops rebuildOperations) error {
	if ops.maintenance != nil && !rebuildContinuationOnly(ops) {
		if err := ops.maintenance(); err != nil {
			encodeCoreOperationFailure(encoder, err)
			return err
		}
	}
	err := a.applyRebuildAdmitted(repo, user, provider, token, encoder, ops)
	var source *nativeSourceRequired
	if !errors.As(err, &source) || ops.source == nil {
		return err
	}
	if err := ops.source(source.Producers); err != nil {
		encodeCoreOperationFailure(encoder, err)
		return err
	}
	return a.applyRebuildAdmitted(repo, user, provider, token, encoder, ops)
}

func rebuildContinuationOnly(ops rebuildOperations) bool {
	path, err := sharedMutationPath()
	if err != nil {
		return false
	}
	intent, err := readRebuildIntent(path)
	if err != nil || intent == nil {
		return false
	}
	snapshot, err := ops.snapshot()
	return err == nil && reconcileRebuildSnapshot(*intent, snapshot) == nil &&
		releasedRebuild(*intent, snapshot)
}

func (a App) applyRebuildAdmitted(repo Repository, user User, provider string,
	token []byte, encoder installResultEncoder, ops rebuildOperations) error {
	unlock, err := sharedMutationLock()
	if err != nil {
		return err
	}
	defer unlock()
	intent, unmet, err := previewRebuild(repo, user, provider, ops)
	if err != nil {
		return err
	}
	if len(unmet) != 0 {
		return errors.New("REBUILD_PREREQUISITES_UNMET")
	}
	if err := bindRebuildCredentialKind(&intent, ops.credentialKind); err != nil {
		return err
	}
	path, err := sharedMutationPath()
	if err != nil {
		return err
	}
	save := func() error { return saveRebuildIntent(path, intent) }
	if err := save(); err != nil {
		return err
	}
	if err := a.resumeRebuild(&intent, token, ops, save, encoder); err != nil {
		var source *nativeSourceRequired
		if errors.As(err, &source) {
			return err
		}
		_ = encoder.Encode(map[string]any{"status": "pending",
			"code":         "REBUILD_INCOMPLETE_RESUME_SAME_COMMAND",
			"retirementId": intent.RetirementID, "activation": "unverified",
			"cause": coreOperationCause(err)})
		var command *CommandError
		if errors.As(err, &command) {
			return err
		}
		return &CommandError{Code: err.Error(), Exit: 2}
	}
	// Intent is retained through successful activation and output. Removing it
	// restores ordinary init only after that exact fixed operation has succeeded.
	return removePrivateRebuildIntent(path)
}

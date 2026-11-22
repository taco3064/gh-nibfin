package cli

import "errors"

// rebuildAgentSetup routes a Claude plan to its own credential transfer.
// Every other plan keeps the shared managed setup unchanged.
func rebuildAgentSetup(codex func(InitPlan, []byte) error, admin managedCredentialAdmin,
	credential *claudeCredential) func(InitPlan, []byte) error {
	return func(plan InitPlan, token []byte) error {
		if plan.Agent == nil || plan.Agent.Provider != "claude" {
			return codex(plan, token)
		}
		if credential == nil || admin == nil {
			return errors.New("REBUILD_CLAUDE_CREDENTIAL_REQUIRED_RERUN_SAME_COMMAND")
		}
		return transferClaudeCredential(admin, plan, token, *credential)
	}
}

// bindClaudeCredential gives this run's Claude setup the owner's credential.
// Its kind enters the rebuild intent; the value never leaves process memory
// except through Core's private credential pipe.
func (ops *rebuildOperations) bindClaudeCredential(process Process,
	credential *claudeCredential) {
	ops.credentialKind = credential.kind
	admin := func(input []byte, operation string) ([]byte, error) {
		// Core's pending result is retained so disconnect can be resumed.
		return runInstalledSharedAdmin(rebuildResultProcess(process), input,
			"managed-credentials", operation)
	}
	ops.host.setupAgent = rebuildAgentSetup(ops.host.setupAgent, admin, credential)
}

// bindRebuildCredentialKind records the Claude credential kind in the intent.
// A resumed rebuild must use a credential of the kind it started with.
func bindRebuildCredentialKind(intent *rebuildIntent, kind string) error {
	if kind == "" {
		return nil
	}
	if intent.Agent.Provider != "claude" || !knownClaudeCredentialKind(kind) ||
		intent.Agent.CredentialKind != "" && intent.Agent.CredentialKind != kind {
		return errors.New("REBUILD_CLAUDE_CREDENTIAL_KIND_CHANGED")
	}
	intent.Agent.CredentialKind = kind
	return nil
}

// promptRebuildClaudeCredential reads the credential from the controlling
// terminal only. Without one it refuses instead of reading standard input.
func promptRebuildClaudeCredential() (*claudeCredential, error) {
	tty, restore, err := openHiddenTerminal()
	if err != nil {
		return nil, err
	}
	defer restore()
	return promptClaudeCredential(tty)
}

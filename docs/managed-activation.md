# Managed native Issue wake on Linux

Fresh `gh nibfin init --repo OWNER/REPO` previews Core's native `issue`
caller and two shared Runner slots on a compatible Linux host. `--apply`
uses the explicitly selected existing GitHub CLI login after consent
(`--consent-managed-login` supplies explicit consent). The installation PR
still follows native review and merge. Users do not create, copy or paste a
Wake token.

Before publishing the installation PR, init verifies the selected account,
target and execution repository identities, repository administration access
and Actions configuration. If the stored OAuth login lacks workflow
permission, supported `gh auth refresh` requests authorization only when that
same account is active; init then captures and verifies the selected login
again. A different active account is never silently switched.

Init prepares `NIBFIN_SHARED_RUNNER_DESTINATION` without repointing an existing
destination, and configures `NIBFIN_WAKE_TOKEN` using GitHub CLI's encrypted
Actions secret setup. The selected credential goes through private standard
input, never command arguments, credential files or result records. Existing
unrelated secrets cause a conflict. Secret metadata alone never proves
credential equality or validity.

After merged caller/source verification, init preserves the existing host
adoption and two-slot readiness process. Core's managed repository admission
applies to the native installation. Init enables the shared pool only after
host identity, adoption and both online Runner identities are verified.
Fresh setup installs no notification timer or artifact-polling service.

## Installation and continuation results

`NATIVE_ROUTING_CONFIGURED` reports `installation: ready` independently of
`continuation`. Credential configuration remains `configured_unverified`:
metadata is not evidence of a successful live dispatch or protected delivery.

Once host and caller readiness are observed, CLI invokes Core's fixed installed
administrator operation `complete-installed-lifecycle` with no inputs. Core
owns current Issue discovery, Pause/dependency admission and reuse of confirmed
continuation effects. CLI does not scan Issues or maintain a work queue.

Continuation statuses are `dispatched`, `paused` and `pending`. `dispatched`
means Core completed its current scan and confirmed any requested dispatches;
it does not mean protected delivery completed. `paused` preserves the pause and
is distinct from installation failure. A missing, malformed or lost response
leaves continuation pending (exit 2), while the result still reports the
observed installation readiness.

Rerun the same init after an interrupted PR wait, partial service start or
continuation response loss. It observes provider and host state, retains the
same installation PR and slots, and reuses confirmed configuration writes.
Core receives the same completion operation again and reconciles its effects.
A lost local installation record permits normal provider observation; it does
not authorize replacing an existing secret.
An already active native route with matching caller, destination, adopted host
and two online Runner identities can request completion without that record.
The credential remains `configured_unverified`; this recovery neither replaces
the secret nor claims to verify its contents.

## Conflicts and uncertain secret writes

`NATIVE_SECRET_CONFLICT` means an existing secret cannot be attributed to a
confirmed setup operation. Init preserves it. A changed destination, account,
repository identity or permissions also prevents unsafe activation.

`NATIVE_SECRET_EFFECT_UNKNOWN` means a write may have occurred without a
confirmed response or readback. The private intent stores only nonsecret
operation observations. Repeating init reads provider state first. A present
secret after an uncertain write remains pending; metadata cannot establish
its contents, and init never blindly replaces it. Reconcile the actual secret
operation before retrying; do not delete private state to bypass uncertainty.

## Existing installations and manual setup

Existing relay callers, peers, explicit service/Wake options and upgrade
consumers retain their existing behavior. This fresh-installation path does
not migrate, disable or retire their resources. Existing-installation and
upgrade/rebuild migration have separate ownership. Init refuses edited or
incompatible callers rather than overwriting them.

These controlled development checks do not certify a live installation,
provider dispatch or release. Independent Acceptance, native integration CI
and installation/E2E verification supply those later results.

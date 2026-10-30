# Current CLI integration status

This source snapshot is incomplete and must not be used as a ready installation.
The original seven commands remain the product boundary. Upgrade and complete
recovery-host onboarding still require implementation; the prior retained test
results do not cover this snapshot. The planned localhost Dashboard is unchanged.

## Runtime and responsibility owners

The selected runtime is the Nibfin PR255 integration candidate at
`bda12320ab60a54d6145763e5950dcf88fa26f8c`; exact archive, manifest, component and
dependency pins are in [the current handoff](handoffs/current/README.md).
Historical N01/N15/N17 loaders remain for retained compatibility fixtures.

| Responsibility | Maintained owner and CLI adapter |
| --- | --- |
| Member binding | Core `runners.mjs` / `runner-lifecycle.mjs`, public adapter for controller `runner-binding.mjs`; registration/list call `observeRunners` and Core member-label identity functions. |
| Runner operations | Core `runner-lifecycle.mjs`, public adapter for controller `runner-operations.mjs`; local CLI register/unregister/remove retain per-instance operation state and process results. No delivery authority is acquired. |
| Docker/container security | Core `docker-security.mjs`, selected Linux backend and `egress.mjs`; create supplies constructor inputs and consumes the complete readiness report. CLI does not implement Docker security predicates. |
| Rootless hardening | Packaged Core `docs/rootless-hardening.md`, `runnerConfiguration` and `preflightRunner`; CLI supplies actual execution account and private resource locations. |
| Installation, recovery and admission | Core installation/transition, workflow, Rescue and job-admission modules; CLI projects immutable managed files and supplies bounded filesystem, provider and service adapters. |

Official Runner acquisition is pinned to version **2.338.2**, source commit
`d1eb12d4b115c301201de27d667284e1a80f2ab5`, archive
`actions-runner-linux-x64-2.338.2.tar.gz`, SHA-256
`baebf204307bac6193480852732f0aba2ae97b6c5597c244f4d5558390147c40`.
Registration compares the archive and all extracted official application bytes
before requesting a token. Both registration routes use `--disableupdate`, verify
`disableUpdate: true` in the official local settings before enabling a service,
and retain the complete distribution identity in their private binding. Admission
activation checks that setting again. This preserves the Worker identity pinned
by the Hook; upgrades must reconcile work and update the application and policy
together. Existing older installations are preserved and rejected rather than
overwritten. Disabling self-update also requires timely maintained upgrades:
[GitHub stops assigning work to outdated Runners](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/customize-runners#runner-software-updates-on-self-hosted-runners).
Argument fixtures do not certify a real service installation.

## Owner and member contexts

Owner initialization uses `gh nibfin init --repo OWNER/REPO`, optionally
`--auto-register`; `--check` observes managed files and the existing control-host
notifier without installing/starting services, logging in, registering a Runner or
executing the aborting Hook. Modified managed files are preserved. An unconfirmed
write retains the original private intent; repeat setup reconciles those exact
bytes before doing another write.

Initialization now selects the maintained native host rather than importing a
user-authored JavaScript composition. For an explicit shared pool, it stages the
verified Core, client inputs and exactly two pinned Runner applications under the
account's private control root. The second application is extracted from the
same authenticated archive; registration state and model credentials are never
copied between Runners. `--check` performs no acquisition or filesystem writes.
Existing staged bytes are verified before reuse, and modified files are preserved.

This initial native preparation does not yet install the control service, register
Runners or reconcile original work. It reports `staged`, not readiness. Until
preparation establishes all host capabilities, init returns blocked before any
repository mutation. A package acquisition or source fixture never establishes
delivery, recovery or actual shared Runner operation.

New Codex logins use an owned disposable login home. The client process group
must terminate before only its exact auth file is exclusively published into the
member login space; temporary helper links are not carried into that space.
Existing credentials and session data are preserved. Unresolved login operations
retain one private intent and cannot silently launch a replacement login.

### Shared personal-repository pool input

Owner `init` selects repository membership with `--pool-repos OWNER/ONE,OWNER/TWO`
and independent physical capacity with `--runner-count N` (default 2, range 1–128).
Ten projects can share six slots. Each project keeps its own stage, delivery and
Rescue workflows and native Actions logs. `--execution-repo` is rejected: the
previous central-source design does not satisfy this product contract. Existing
version 1 protected records require reconciliation and are never silently rewritten.
The CLI checks each public repository's identity, ownership and administration
permission. This selection itself does not register or authorize a Runner.

The private installation record retains the descriptor. Repeat `init` and
`--check` reread all identities and reject transfers, replacements or changed
membership. The repository runtime file only describes the pool: it cannot
select or replace this protected local configuration. Switching an existing
installation into a pool also requires the reconciled transition boundary.

The legacy notifier-only adapter is retained as an explicit compatibility API;
it is no longer the default installation path. Native preparation currently stops
before repository writes or service activation. Complete control-service assembly,
repository-local ephemeral registration, shared capacity and original-work handover
remain incomplete. Source capacity fixtures do not establish that live adapter.
Acquisition doubles in the installed-consumer tests establish staging and ordering
only; they do not certify real Runner services or a live delivery.

### Managed host state and transition retention

New host compositions can omit executable `records` callbacks. The installer
allocates one private file store for the complete protected repository set, and
the notifier opens that same store after restart. Existing external record
owners are retained; mixed ownership, missing/corrupt state and changed identities
are rejected rather than replaced with empty records. A new store starts paused.
The protected lifecycle owner must supply the real work checkpoint; initialization
never clears a pause or fabricates an accepted conversation or receipt.

The maintained Core owns Rescue record structure, content identities and pin
transition decisions. The CLI adapter supplies durable atomic file replacement,
private permissions, compare-and-swap under cross-process exclusion, and native
filesystem subscriptions. Notifications use no polling, cron or idle Actions.
The user service uses systemd readiness notification and a bounded startup
deadline: it becomes active only after every notifier has started successfully.
Runtime selection, original work and transition intents are retained together.
An unresolved transition survives restart and cannot automatically repeat its
write. Selection can switch only after a trusted coordinator verifies the complete
staged runtime and terminated service boundary; rollback preserves original work.

`upgrade [--check|--apply|--rollback]` now invokes the maintained coordinator.
The native installer must first retain `upgrade-plan.json` under the account's
private `~/.local/share/nibfin/control` root. Its version-1 content identity binds
the current/next installations and inventories for exactly `runtime`, `runner-1`
and `runner-2`. Each resource has current/next file or contained-link inventories;
file entries bind relative path, mode and SHA-256, and links also bind their exact
relative target. Paths and service names are derived by the client. The plan
cannot select executable JavaScript, commands, another account or arbitrary paths.
The native installer authenticates distribution sources before producing this
record. A content hash alone confers no source authenticity or work authorization.

Checks do not stage packages, start services or change state. Apply requires a
reconciled, terminated, inactive and explicitly paused work boundary; Owner holds
and unknown writes still block. It stops and observes all three services, switches
verified generations, atomically selects complete pins, and reads back the actual
services and unchanged work. An uncertain outcome leaves services stopped with
the original journal. Rollback restores that journal's original generations and
pins without clearing pause, pending candidates, sessions or receipts. Kernel
leases release on coordinator death; the persistent lock file is never deleted
to force a retry. The journal and its ancestors cannot be generation resources.

Installed tests cover real file/CAS transitions with explicit service doubles.
A separate local probe exercised real user-systemd process switches and rollback;
its disposable workers were not official Runner binaries. Native host composition,
automatic staging and actual coordinated Runner upgrade/registration proof remain
incomplete. The notifier-only installation is never treated as that native host.

A separate assigned member uses their own OS account, stored `gh auth login`, and
model login. They do not receive the Owner's model credentials or Runner
administration permission merely through Write access. Repository ownership
(personal or Organization), registration level (`--repo` or `--org`) and the
Owner's automatic-registration policy remain distinct. Private repositories are
outside scope.

Creation selects one Agent and a preprovisioned immutable image:

```sh
gh nibfin runner create --name member --agent codex --image sha256:IMAGE_DIGEST --count 2
```

The image value must be an actual reviewed 64-digit digest. The rootless socket
default is `/run/user/EXECUTION_UID/docker.sock`; `--socket` selects another
supported rootless socket. Core validates the account, image, tools, private
resource layout and isolated model/dependency network configuration. The Linux
backend is the current integration target. Windows/WSL delegation and concrete
platform/model combinations remain Unverified; no distribution or developer
filesystem layout is assumed.

The same create journey performs member login in its private environment. Codex
uses its login interaction with a dedicated `CODEX_HOME`. The Claude/Gemini
adapters use their packaged API-key-file contract and a hidden terminal prompt.
No login is copied from the Owner or another environment. Missing terminal/login,
tools, isolation or required npm/Playwright/model destinations cannot publish an
environment. Failed readiness retains the exact allocated IDs and member login;
resume the same name, Agent, count, image and account. An unresolved publication
or cleanup must be reconciled from the retained private record before retrying.
Disposable probes/gateways are removed by Core before creation is published.

Registration selects existing instances only. Manual registration uses the
management identity's official setup. The automatic path requires Owner opt-in
and the protected packaged issuer; retrieval/decryption stays bound to its exact
request, Run and Attempt. The CLI adds only an exact request-correlated `run-name`
to the packaged request workflow. This consumer adaptation does not replace the
issuer or grant administration to the applicant.

Before service enablement, registration installs the exact packaged admission
Hook outside participant/Runner workspaces, a private instance policy and the two
service environment variables. It checks actual service environment activation,
source/policy and Worker identity. Org-level policies additionally require a
bounded exact same-Org public repository set. No failure is represented as a
safe enabled service. Unregister/remove stop the affected service before removing
its private policy; unrelated registrations and the Agent environment survive
unregister. Member remove needs no Owner approval and reports remote residue
separately. Complete integration/upgrade regression proof is still pending.

## Evidence boundary

Current installed-package tests exercise actual Core constructors, readiness,
installation planning, immutable loading and the unchanged aborting Hook. Platform
and GitHub transports are simulated and labeled. Root-only checks cannot prove
the required non-root user-systemd lifecycle. Fresh matching retained regression,
complete upgrade/recovery assertions, independent Acceptance, hooks and applicable
CI remain required. No live-provider, release, model, production cutover or
Linux/WSL certification (#48/#49) is claimed.
# Explicit runtime selection

`runner create` accepts `--runtime-profile PROFILE` for Codex. The identifier must
exist in the verified producer's `linuxCodexProfiles` catalog. It is retained in
the original creation intent and environment record and supplied to the actual
backend; resume cannot change it. Omitting the flag preserves the original
profile and does not infer support from a newer installed Docker version.

The reviewed `linux-amd64-codex-node22-docker29-v1` tuple requires both Docker
client/server 29.9.1 and the exact supported Node 22 image metadata. Unknown
profiles, client/server mismatch and a missing explicit selection remain blocked.
These installed-producer transport fixtures do not establish a native host,
model login or actual Runner/service installation verdict.


The following source-admission layout describes the historical centralized v1
prototype. It must not be activated by the v2 official onboarding path.
Shared source admission reads all caller/callee files at one authenticated main
commit. Pool membership and the runtime/actor-policy digest must match the
protected local Owner installation; public runtime JSON cannot select membership.
Each generated logical stage gets execute/compute policies on the physical source
registration, with its fixed reusable delivery callee pinned as a dependency.
The real Hook rechecks that callee at the actual Run commit before Job steps.
No new physical Runner registrations are created by policy planning. These checks
are source and installed-process evidence; production activation remains pending.

## Native control process

The shared-pool `init` path now installs and starts the maintained non-root
`nibfin-control.service` after verifying its private Core/client runtime and the
configured number of official Runner distributions. `init --check` only observes it. The service
uses a kernel lease, systemd notification readiness, and a mode-0600 Unix socket;
there is no public listener, user-authored JavaScript binding, additional setup
command, or private controller dependency. Each startup rechecks the full runtime
inventory before announcing process readiness. Existing service definitions and
original work are preserved on mismatches or interrupted installation.

This bootstrap currently exposes status only. Its response explicitly reports
`native-delivery-composition-pending`; it cannot execute events or declare the
host ready. Runner registration and retained-work reconciliation remain separate
holds. Neither starting this service nor passing source tests certifies a live
development execution or enables repository workflows.

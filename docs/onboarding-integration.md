# Current CLI integration status

This source snapshot is incomplete and must not be used as a ready installation.
The original seven commands remain the product boundary. Upgrade and complete
recovery-host onboarding still require implementation; the prior retained test
results do not cover this snapshot. The planned localhost Dashboard is unchanged.

## Runtime and responsibility owners

The single production runtime is the Nibfin PR251 public source package at
`f411c96bb2336ae271c8ebaa1c5bd17b1bb8f252`; exact archive, manifest, component and
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

Initialization currently requires an existing, privately configured public Core
host composition. Automatic bootstrap of that existing host, webhook/backfill and
all workflow deployment capabilities is not complete. This limitation is not an
additional mandatory end-user command or a second controller installation. A
successful source plan or repository-file fixture is not host readiness.

### Shared personal-repository pool input

Owner `init` can select a finite pool with `--execution-repo OWNER/SOURCE` and
`--pool-repos OWNER/SOURCE,OWNER/SECOND,OWNER/THIRD`. Both options are required
together and the initialized repository must be a member. The CLI reads each
actual public repository, its stable ID, personal Owner ID and administration
permission, then delegates descriptor validation to the installed Core. No
Runner is created or registered by this selection.

The private installation record retains the descriptor. Repeat `init` and
`--check` reread all identities and reject transfers, replacements or changed
membership. The repository runtime file only describes the pool: it cannot
select or replace this protected local configuration. Switching an existing
installation into a pool also requires the reconciled transition boundary.

The existing notifier-only host does not implement shared execution. Its check
returns a specific blocked diagnostic, and pooled init stops before repository
writes or service installation. Automatic protected-host installation, actual
cross-repository routing and two-instance registration remain incomplete. The
installed-consumer fixture uses a synthetic host and proves only descriptor
construction, retention and forwarding, not real shared Runner operation.

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

These are host storage/selection capabilities, not a complete `upgrade` command or
automatic executable deployment. Package staging, actual service coordination and
the remaining first-party host composition still require integration. Filesystem,
restart and child-process tests exercise real storage; the transition verifier in
those tests is explicitly a fixture and does not certify a runtime/service upgrade.

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


Shared source admission reads all caller/callee files at one authenticated main
commit. Pool membership and the runtime/actor-policy digest must match the
protected local Owner installation; public runtime JSON cannot select membership.
Each generated logical stage gets execute/compute policies on the physical source
registration, with its fixed reusable delivery callee pinned as a dependency.
The real Hook rechecks that callee at the actual Run commit before Job steps.
No new physical Runner registrations are created by policy planning. These checks
are source and installed-process evidence; production activation remains pending.

# gh-nibfin

Local GitHub CLI extension with help, version diagnostics and seven bounded
command paths. Runner creation implements local allocation and diagnostics through
the Core boundary described below. Manual Runner registration is implemented for
the Linux user-systemd process boundary. Read-only runner listing, environment-retaining unregister and environment removal
are implemented at that local boundary; init/upgrade return exit status 2.
This is not a complete Nibfin runtime or a supported-platform claim.

Public repositories owned by personal accounts or Organizations are in product
scope. Repo ownership, Repository/Organization Runner registration level and
Owner-enabled automatic registration are separate dimensions. Private repositories
are outside scope. Identity syntax and routing labels confer no registration,
execution permission or Owner authorization.

## Prerequisites and local checks

Installation prerequisites are Node.js 22.x, npm 10.x, a POSIX `/bin/sh` interpreter,
Git, and a GitHub CLI version with `gh extension install`/`remove` support. The root
`gh-nibfin` is executable and resolves `src/cli.mjs` relative to its own location.
No installed gh, login, Agent credential or network is needed for local checks.
Registration tests install the exact local N15 and JOSE archives with npm 10.x,
offline and with scripts disabled, in disposable space. Historical N01 checks
remain dependency-free.

```sh
./gh-nibfin --help
./gh-nibfin --version
python3 tooling/check.py
```

The reproducible local check additionally requires Linux, Python 3 and libseccomp.
It fails if it cannot enforce networking denial. The inherited syscall filter
rejects socket creation and network operations, probes the denial, and runs Node
and child processes with a minimal credential-free environment. Unix socketpair
IPC remains available for Node's process plumbing. `node --test
test/*.test.mjs` runs the same tests without this network-isolation proof.
Neither command is actual GitHub CLI installation verification.

The seven paths have these responsibilities:

| Path | Responsibility (init/upgrade pending; local lifecycle paths implemented) |
| --- | --- |
| init | Initialize repository workflows/labels and optional automatic registration. |
| runner create | Create one fixed-Agent environment with a positive integer total of unregistered instances; default 2. |
| runner register | Register existing instances, without silently adding instances. |
| runner unregister | Unregister one instance while retaining its environment and other registrations. |
| runner list | Show local environment/registration/service state without secrets; remote state requires an actual authorized read. |
| runner remove | Member-controlled environment removal; report failed remote cleanup. |
| upgrade | Update Nibfin with version, compatibility and authorization checks. |

Help/version are options, not additional commands. Unknown options, extra tokens
and incomplete paths fail with usage guidance. Baseline refusals perform no
initialization, authentication, network access, service operation or registration.
Implemented lifecycle parameters are described below; init/upgrade execution belongs to later Issues.

## Verified Core boundary

The read-only [N01 handoff](docs/handoffs/n01/README.md) is the initial local
integration input, not a formal release. The approved producer is nibfin #84 /
PR #136, commit `0a2e276ad5b37d94b485bbcf8c7288743b9d158f`, package
`@nibfin/core@0.1.0`. Its 3,980-byte archive has raw-byte SHA-256
`956b12fb80620e18576212d371989b0448cf39878533b813e8cba838a40c1759`.

`src/core.mjs` pins archive and reviewed provenance bytes, checks the exact four
regular files and per-file identities, bounded gzip/tar structure and package
metadata before writing or executing anything from the package. It rejects
changed content, traversal, links, duplicates, extra/missing files and unsafe
metadata. There is no floating latest, download, cache fallback or install script.
A dependency-free installer writes only verified bytes into a fresh external
`node_modules/@nibfin/core`, then a fresh bridge imports by public package name.
The approved package has no dependencies or install scripts. This preserves
package exports resolution without invoking npm or trusting caller dependencies.
Temporary state is removed on success and write/import/callback failures.

The maintained consumer entry exports `withCore`; for example, from this checkout:

```sh
node --input-type=module -e "import {withCore} from './src/cli.mjs'; await withCore(core => console.log(core.memberRunnerLabel('42')))"
```

This uses actual producer exports, not consumer replicas or an eighth product
command. `--version` also verifies and imports Core. Help and refusals need only
Node 22.x. On verification failure, restore the exact reviewed handoff from the
applicable immutable CLI artifact and retry; do not edit pins or use another
archive. Missing Node requires installing Node 22.x. Temporary-directory failure
requires a writable external temporary directory. No automatic repair downloads
or credential reads occur.

## Later real installation procedure — not performed

The later [N15 observation handoff](docs/handoffs/n15/README.md) supplies the
immutable producer interface and accepted package needed by runner registration
in #21. Registration now uses the explicit `withRegistrationCore` loader in
`src/core.mjs`. Historical `withCore`, `--version` and create capability checks
retain N01. Both handoffs are immutable and share the version string 0.1.0;
their commit/archive identities differ. N15 has lifecycle descriptors but no
native Linux backend, so its presence does not establish create readiness.

[GitHub's extension layout](https://docs.github.com/en/github-cli/github-cli/creating-github-cli-extensions)
requires the executable at the repository root with the repository name.
[Official installation/removal](https://docs.github.com/en/github-cli/github-cli/using-github-cli-extensions)
is owned by #27 on Linux and #28 on supported Windows-hosted WSL. Both consume
the applicable initial immutable CLI artifact through their existing release
dependency. Real installation and platform verification are **not yet performed**.
No measured gh version or successful installation transcript is claimed here.

In an authorized disposable suite environment, first obtain and verify that
immutable CLI artifact using its release identity/digest. Extract it as a directory
named `gh-nibfin`, preserving the executable mode. Set `cli_artifact` below to its
absolute extracted path. Use a disposable OS user/container so existing user
extensions, credentials and Agent login stores cannot be read or removed. Supply
only the suite's authorized gh authentication if the installed gh requires it;
never reuse Agent credentials or print tokens.

```sh
# Run as the disposable user. cli_artifact is the verified extracted directory.
scratch=$(mktemp -d)
export HOME="$scratch/home" GH_CONFIG_DIR="$scratch/gh" XDG_DATA_HOME="$scratch/data"
export XDG_CONFIG_HOME="$scratch/config" XDG_CACHE_HOME="$scratch/cache"
mkdir -p "$HOME" "$GH_CONFIG_DIR" "$XDG_DATA_HOME" "$scratch/other"
cd "$cli_artifact"
gh extension install .
cd "$scratch/other"
gh nibfin --help
gh nibfin --version
gh extension list
gh extension remove nibfin
gh extension list
# Assert nibfin is absent from the final list and installed extension directory;
# confirm the immutable source artifact is unchanged, then remove disposable state.
rm -rf "$scratch"
```

Capture OS/WSL host and guest versions, architecture, actual Node/npm/Git/gh
versions, artifact/producer identities, executable mode, command statuses/output,
installed extension resolution from the unrelated directory, removal read-back
and cleanup observations. On failure retain sanitized diagnostics, perform the
same removal/read-back and disposable-user cleanup, and record failure honestly.
These suites prove installation only within their tested configurations; local
module/process tests do not establish full product or platform support.

## Runner creation: local command boundary

```sh
gh nibfin runner create --name local-codex --agent codex --count 4
```

Count is the total for this one environment, not per Repo/Org: omitted means 2;
1 through 128 are supported. Larger values fail rather than truncate. Names must
start with a lowercase letter, contain only lowercase letters, digits or hyphens,
and be at most 48 characters. Agent is `codex`, `claude` or `gemini`; these names
select identity, not a claim that every native backend/platform is installed.
The reviewed interface here is Linux/Codex: Claude/Gemini creation reports a
missing matching adapter rather than invoking a Codex backend for another Agent.
Options occur once; unknown options, extra operands and missing values fail.

Storage is `~/.local/share/nibfin/environments/NAME`, using the OS account
home returned by `userInfo()` rather than an inherited `HOME` override, with private directories (0700) and an `environment.json` record (0600).
Each `instance-N` contains separate `workspace`, `registration` and `service`
directories. UUID local identities are not GitHub Runner IDs. All instances refer
to the one environment `login` directory; another environment has another login
space. Records explicitly say `unregistered` and `not-installed`. Creation does
not register, install/start services, start models, or require a target Repo,
Issue, existing Runner ID or Owner Grant. It conveys no delivery authorization.

Existing names cannot be overwritten, resized or switched to another Agent.
Choose another name for another Agent/account. Symlinks and non-directory storage
components are rejected. Allocation reserves the name exclusively; only the
current attempt's newly reserved directory is removed after failure. A record is
published by renaming a private pending file after prerequisites pass. Readers
must require `environment.json`; directories without it are incomplete attempts.
After successful rollback retry normally. An incomplete-cleanup diagnostic names
the environment: inspect it under the storage root and preserve any login data
before manually removing residual resources. Existing environments and unrelated
files are never rollback targets. Host administrators remain trusted; this layout
is not a new isolation platform or protection against hostile host mutation.

The command checks Git/gh availability, gh auth status and the stable GitHub user
ID using the actual execution account's gh configuration. Actions tokens, provider
keys, alternate inherited Agent login homes and Node injection settings are not
passed to diagnostic subprocesses. It never logs in for the member. Run it as the
intended service account; an interactive user's login does not establish service,
container or WSL login. Backend readiness receives that execution account and the
environment-specific login selection. Backend errors and unavailable/unknown
readiness fail safely with tool/login/isolation recovery guidance. Raw provider
responses, tokens and login contents are neither printed nor persisted. HTTP
401/403 in gh failures are sanitized diagnostics, not authorization grants.

The creation path still selects **the pinned N01 identity package**.
It lacks `createAgentEnvironments` and `createLinuxCodexBackend`; create therefore
reports the missing capability after gh checks and publishes no environment.
A reviewed immutable later package must enter through the maintained loader before
native creation can succeed; there is no download, floating pin or fake-ready
fallback. Registration selects N15 separately and does not manufacture a backend. Core owns
descriptor validation and backend login/isolation logic; the CLI calls `describe`
and `readiness`, never backend model lifecycle methods to allocate instances.

`python3 tooling/check.py` discovers all retained test files and enforces network
denial with a minimal credential-free environment. Command tests use an explicit
imported `main` dependency boundary with local Core/process contract doubles,
including an executable Node harness from a checkout path containing spaces.
There is no product flag/environment toggle that enables these doubles. Tests
inspect real disposable filesystem records and exercise default/1/4/invalid
counts, separation, collisions, symlinks, process diagnostics, unavailable Core,
partial publication and rollback failures. These are local injected observations,
not actual Agent login, model execution, native isolation or installed-platform
success. #27/#28 retain actual installed-environment/service/login lifecycle proof.
The supplied upstream API anchor is nibfin commit
`7d3b8f9261cdf61f0d29d7176fa51a5317830f5c`; an attempted read of its source and
lifecycle documentation returned HTTP 403 in this execution. No claim of inspecting
those source bytes or measuring their backend behavior is made.


## Manual registration of existing instances

```sh
gh nibfin runner register --runner local-codex --repo example/project
# Alternatively, Organization-level registration with its own permissions:
gh nibfin runner register --runner local-codex --org example --group 'Build runners' --method manual
```

`--runner` selects an existing G03 environment, not a newly allocated Runner.
Each invocation selects its first unregistered instance in persisted descriptor
order. A second invocation selects the next instance, possibly for another target;
count, Agent and shared login paths never change. A private `binding.json` in each
instance's registration directory records lifecycle state separately from the
immutable environment descriptor. Exhaustion fails. Pending/unknown bindings block
further registration until reconciled; no `--replace`, implicit instance expansion
or replacement by matching name/Label is used. Options must occur once; repo/org
are exclusive, group is Org-only, and omitted method means manual. Explicit auto
is rejected; this command never enables automatic-registration policy.

Both personal and Organization-owned **public repositories** use `--repo`.
`--org` is distinct and verifies Organization/Runner/group identity only; it
neither invents a target repository nor establishes repository/workflow access.
Repository registration uses the N15 public `observeRunners` export and requires a
complete match of actual stable target/owner and local Runner/member identities.
Known registered bindings for that member in other complete local environments
are validated and included; conflicting local IDs stop before token issuance.
Offline and online-busy observations remain separate from unavailable evidence.
Registration/service readiness is not Issue execution, Owner Grant or merge permission.

Before operating, prepare each selected instance's existing `registration/`
directory with a separately extracted, version-pinned official
[actions/runner distribution](https://github.com/actions/runner/releases), including
executable `config.sh` and `bin/runsvc.sh`, and its normal bin/externals dependencies.
Use a release appropriate to that host/architecture, verify its official checksum,
and record its exact version/digest in the authorized live suite. Do not reuse
another instance's `.runner`/`.credentials`, share application directories, or
preconfigure a Runner. This command never downloads or upgrades an application.
The local executable fixtures use Node 22 and fixture contract revision 1; no
official Runner application version was executed or certified here.

The implemented service boundary requires **non-root Linux**, an existing user
systemd manager for the actual execution account, and writable private G03 paths.
`systemctl --user show-environment` checks availability before token issuance.
After configuration, the command copies the prepared official `bin/runsvc.sh`
exclusively to `runsvc.sh`. The per-instance unit invokes that entry point, as
required for [custom Runner services](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/configure-the-application#customizing-the-self-hosted-runner-service).
The command links/enables/starts that unit and checks `is-active`; it does not
request sudo, install a system service, configure lingering, or infer boot/session
availability. The administrator must arrange the user manager/session lifetime.
Missing executables, privilege/account mismatch or an unavailable manager fail;
Windows/macOS and Linux without that mechanism have no implemented service path
here. All actual host/service combinations remain **Unverified** until #27/#28.

Run as the intended service account with its own stored `gh auth login` and the
same stable member ID recorded at create. Inherited GH_TOKEN/GITHUB_TOKEN overrides
are removed; login is never automated. Login, Repo Write and Org membership do
not grant Runner management. The official scoped registration-token endpoint is
authoritative: Repository Administration write/read or Organization self-hosted
Runner management/read rights and applicable policy are required. Sanitized
401/authentication, 403/denied, 404/not-found-or-inaccessible and rate-limit errors
identify recovery needs; 404 is not proof the target does not exist. No automatic
retry of a token write occurs. Short-lived token/expiry are validated, passed only
to the selected configuration process and never written to binding records or
public diagnostics. Configuration is an argument array with bounded group
termination/cancellation; child output is discarded. Official registration
credentials remain private in that instance and are not rollback targets.

After any possibly effective operation, failure retains a pending/unknown binding
and any known Runner ID/service disposition. A publication failure may leave
`binding.json.pending`; keep it and `.runner`/`.credentials` for authorized manual
readback. Inspect the exact target/Runner ID, private service unit and official
Runner state; resolve unknown operations before attempting registration again.
Do not delete credentials/reservations merely to retry, infer identity from a
Label/name, or destroy another completed instance. There is no automatic rollback
or new reconciliation CLI command in this surface.

The N15 loader pins reviewed provenance, all 23 Core files, the 71,426-byte archive
SHA-256 `01f6b5fbd01ebbba37ea6723db7bdfff25f5cc44c68e832206974248e5717fef`
and genuine `jose@6.2.13` bytes/producer-lock SHA-512 integrity before execution.
It preserves structural archive rejection and imports only an isolated offline,
scripts-disabled installation; it never uses caller node_modules or `/upstream`.
Temporary installations are removed on success and failure. Installation problems
require intact archives and supported local tools, not remote resource provisioning.

Run `python3 tooling/check.py` for command, installed-consumer, provider,
configuration/service executable and failure tests under credential-free seccomp
network denial. Also run `node docs/handoffs/n15/verify.mjs` for the ten original
producer tests through an offline installed public package. Both use verifier-owned
disposable files/processes; no cache, fixture credential or generated package is a
candidate artifact. These checks prove local contracts only. Actual authorized
Repository registration/lifecycle belongs to #27/#28; multi-member integration
belongs to nibfin #122. Actual Org registration is separately permissioned and
unrun combinations remain Unverified. Independent Acceptance and Core lifecycle
gates still apply; neither local checks nor registration confer delivery authority.

### Read-only runner inventory

`gh nibfin runner list [--runner NAME] [--json]` inspects the execution account's
`~/.local/share/nibfin/environments`. NAME selects an environment, including all
its existing instances. No Agent login or execution is needed. Environments and
instances sort by local identity; this order is not dispatch priority. The private
`environment.json` supplies fixed Agent/account/instance identities; each instance's
`registration/binding.json` supersedes the initial descriptor lifecycle state.
Missing storage is empty; incomplete creation directories, corrupt/inaccessible
records, duplicate identities and unsafe paths produce an incomplete inventory.
The command never reads Agent login or Runner credential files or changes state.

JSON stdout contains exactly one object: `schemaVersion: 1`, `complete`,
`environments` and `diagnostics`. Each environment has `name`, `agent`, `githubId`,
`account` and `instances`. Each instance has `id`, `registration`, `local` and
`remote`. IDs remain decimal strings, including large IDs. Registered records
include member Label, registration level/ID, target coordinates/IDs and owner type,
platform, nullable group ID and a separately marked `historical` observation.
No absent login name is invented. Unknown/malformed registration is explicit.
Text exposes the same substantive fields. Unknown optional fields use null;
unknown remote status and Busy are null, never an implied offline or false.

`local.state` is active, inactive, not-installed or unknown. Current probes use
Linux user systemd `show` with a validated unit and execution-account runtime
directory. Missing tools, timeouts, unsupported platforms and account mismatch
are unknown with a safe reason. With no installed unit recorded, not-installed
means no service identity is recorded; stored active status is never proof.
No service is started, stopped or repaired.

Valid registered instances attempt fresh GET-only readback through the reviewed
N15 Core/provider using the execution account's stored `gh` login; inherited
Actions tokens and provider credentials are excluded. Repository registrations
work independently of personal/Organization ownership. Organization registrations
use official organization inventories and, when recorded, group membership;
without a target repository they make no repository/group access or execution
eligibility claim. Registration level and owner type imply no auto-registration
policy. Private repositories remain outside product scope.

`remote.state: verified` supplies fresh `status` (online/offline), boolean `busy`,
`source: github` and observation time. Inventory failure, denied access, absent
login, timeout or malformed/incomplete pagination remains unknown with a safe
reason and, for N15 repository observations, structured diagnostic codes/categories.
A missing/replaced ID is stale/mismatched registration, never offline; names or
Labels cannot substitute for its ID. Historical binding observations never become
fresh status. A running local process does not establish remote Online. Reads
across APIs are not atomic, and diagnostics grant no execution authorization.

Exit 0 means complete local enumeration, including explicitly unavailable optional
remote/service observations. Exit 2 means invalid arguments, missing named
environment or incomplete/untrustworthy local inventory. JSON errors use the same
schema and `complete: false`; any retained entries are partial evidence. No raw
provider/process error output is printed. Invalid flags are rejected before I/O.
Reads and subprocesses are bounded; readback has a 15-second deadline per scope.

Run `python3 tooling/check.py` with Node 22, npm 10 and Linux/libseccomp. Retained
list tests use disposable records, official-response fixtures and verifier-owned
service/gh executable fixtures. These prove local command behavior, not live GitHub,
installed systemd service, model login or cross-platform integration. Issues #27/#28
own real local/remote comparisons. No live registration/model credentials are
required for these diagnostics tests.

### Unregister one registration (local contract implementation)

```sh
gh nibfin runner unregister --runner demo --repo OWNER/REPO --instance INSTANCE_ID
gh nibfin runner unregister --runner demo --org ORG --instance INSTANCE_ID
```

Both target and environment are required. Omit `--instance` only when exactly one
registration matches that target; ambiguity never selects the first instance.
Repository runners work with personal or Organization-owned public repositories.
`--org` removes the Organization registration shared across its eligible repos;
using `--repo` never removes that shared runner or changes its access policies.

This implementation uses the existing Linux non-root user-systemd integration.
It checks the selected unit's source, stops/disables it and verifies termination
(inactive, no main PID and an empty tracked cgroup). A missing manager, unsafe
ownership, running descendants or unsupported observation prevents cleanup.
Agent login, environment descriptor, instance count, workspace, prepared official
Runner application and neighboring services/credentials remain intact. Only the
selected registration credentials, binding and owned service configuration detach.

The process account's stored `gh` login supplies official REST reads and exact-ID
Repo/Org DELETE; inherited `GH_TOKEN`/`GITHUB_TOKEN` do not override that login.
A complete authorized runner inventory establishes absence; offline status, a
DELETE acknowledgment or inaccessible 404 does not. Exit 0 requires termination,
local detachment, remote absence and published state. Exit 2 reports separate
local/service/remote dispositions and keeps private `registration/unregister.json`
with original IDs for reconciliation. A confirmed repeat says already unregistered.

Unknown privileged writes or active work retain registration resources after a
safe service stop. The pinned handoff has no durable runtime execution journal;
this CLI conservatively treats a nonempty workspace, Runner Worker history or
local execution/write-intent records as requiring runtime/operator reconciliation.
It never deletes that evidence or assumes a stopped process proves prior writes
finished. This boundary does not certify live lifecycle safety for used runners.

For incomplete removal, inspect private instance state and reconcile the original
Runner ID, service termination and any execution effects with the owning runtime
or authorized administrator. Do not delete a lock, pending file or residual merely
to permit reuse. Interrupted atomic publications and stale operation locks require
operator reconciliation. A prior uncertain DELETE is read back on re-entry and
never automatically resubmitted, even if still present; an authorized administrator
must resolve it. Remote denial/offline conditions still allow independently safe
local detachment. Residuals block registration; fully reconciled unregister retains
the existing capacity for registration again. No automatic model login, remote
provisioning or live Runner removal is part of local verification.

Retained unregister tests use disposable files, recording services and injected
official-shaped responses under `python3 tooling/check.py` (Node 22, Python 3,
libseccomp, executable scratch). They cover selection, Repo/Org scope, large IDs,
termination, uncertain writes/readback, cleanup/publication failures, preservation
and secret suppression. These are local contract observations, not real GitHub,
systemd or model lifecycle evidence; real lifecycle coverage belongs to #27/#28.

### Remove an entire local environment

```sh
gh nibfin runner remove --runner demo
# For an explicitly selected, noninteractive removal:
gh nibfin runner remove --runner demo --yes
```

`--runner` selects the whole environment, including every registered, unregistered
or partially unregistered instance. Without `--yes`, the confirmation lists the
selected environment, destructive local scope and recorded Repo/Org registrations.
Only affirmative yes proceeds; decline, EOF and noninteractive input cancel.
`--yes` skips confirmation only. Removing an Organization registration affects its
entire shared registration, not just one repository. Other environments, their
services and Agent logins, host-global gh/Agent logins and shared tools are retained.

No Repository Owner approval, working GitHub login or remote management permission
is needed for safe local exit. The implemented boundary is non-root Linux with an
observable user-systemd manager, owned real paths and matching execution account.
The command checks owned unit sources, no foreign drop-ins/stop propagation,
inactive state and empty managed process groups, then disables and removes owned
services. An unproved stop retains resources and returns exit 2. No cleanup script
from the environment is executed. Environment credentials, workspaces and prepared
Runner applications are deleted only after all instances are safely stopped.
No model is invoked or logged in. Windows/macOS and live service/provider/model
combinations remain Unverified here; #27/#28 own supported-profile lifecycle proof.

Local lifecycle exclusion and a removal marker prevent registration/reuse during
removal. Dead recorded process locks can be recovered; ambiguous, unowned or
incompletely written locks require operator reconciliation, not blind deletion.
Private removal state is saved at
`~/.local/share/nibfin/environments/.removals/NAME.json` before destructive effects.
The stopped directory is atomically moved to `.removals/NAME.resources` before
recursive deletion, so an interrupted deletion can be resumed by the same remove
command. A filesystem generation identity prevents a later invocation from deleting
a newly created environment with the same name. Use a new name when recreating an
environment; preserve the original residue until its reconciliation is complete.

Each instance reports service, local resources, remote registration and unresolved
effects separately. Remote deletion uses only validated recorded IDs, the actual
account's stored gh login and official Repo/Org endpoints through the pinned Core
provider. Inherited Actions tokens are excluded. Complete validated readback is
required to confirm absence; a DELETE acknowledgment, 404, timeout or inaccessible
inventory is insufficient. A previous uncertain DELETE is read back without replay.
One remote failure does not suppress the remaining targets or safe local removal.

Exit 0 requires complete local deletion, published state, confirmed remote absence
(or no registration) and no unresolved effects. Exit 2 can report `local=removed`
alongside remote/effect residue. Nonempty workspaces, Worker history and write-intent
records do not force continued local resource use when their necessary readback
coordinates can be preserved safely. The private residue retains source filename,
operation, repository, request ID and validated Issue/PR/Runner/run identifiers or
commit hashes; it never replays these operations. Tokens, raw logs and provider
payloads are excluded. Unknown fields, malformed records or missing required
coordinates cannot be reduced to an unresolved flag: services stop, but the
original operation evidence and environment remain retained with exit 2. The CLI
names those service evidence files. Have the owning runtime reconcile them before
retrying; do not delete the only copy to force cleanup. Publication failure also
retains source evidence. Captured identities remain available on repeated removal.
A stopped process or deleted registration does not prove prior privileged effects were revoked. Use the private record's exact
identities to reconcile with the owning runtime/authorized administrator; never
replay an unknown write or erase residue simply to obtain exit 0. Repeating remove
reads the same record and is distinguishable from an unknown environment name.

`test/remove.test.mjs` uses disposable multi-instance files, injected official
provider/service observations and bounded disposable process termination. Run
`python3 tooling/check.py` and `node docs/handoffs/n15/verify.mjs`; these are local
contract checks, not actual account, Runner, model or platform lifecycle evidence.

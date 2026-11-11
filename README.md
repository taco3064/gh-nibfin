# gh nibfin

GitHub CLI extension for Nibfin public-repository onboarding, manual
repository-level Runner management and environment inspection.

Manual [Bootstrap #1](https://github.com/taco3064/gh-nibfin/issues/1) is in progress.
The candidate implements previewed, PR-based initialization; no formal release
or protected delivery is claimed. Runner lifecycle commands are implemented as
candidate management; read-only preflight reports observed capabilities and gaps.
Core remains the sole owner of delivery policy, authorization and isolation.

Reviewed immutable candidates support the first authorized external E2E.
Formal Core/Action/CLI publication follows that E2E and Owner authorization.

## Build and use the candidate

Use the Go version pinned in `go.mod`. No third-party Go dependencies are used.
Clone this public repository and check out the reviewed full commit before
building; never silently follow `main` during an installed candidate execution.

```sh
go run ./scripts/check.go
go run ./scripts/staged
go vet ./...
go build -buildvcs=true -trimpath -o gh-nibfin .
gh extension install .
gh nibfin help
```

The staged test command selects packages affected by changes in the Git index.
CI uses the same selection with the pull request or push diff.

On Windows, build `gh-nibfin.exe` instead of `gh-nibfin`. GitHub CLI's native
[local-extension convention](https://docs.github.com/en/github-cli/github-cli/creating-github-cli-extensions)
uses the executable in this checkout. Keep that exact checkout while installed;
`gh extension remove nibfin` removes its registration. Do not replace an existing
user extension without inspecting it first.

```sh
gh nibfin init --repo OWNER/REPO
gh nibfin init --repo OWNER/REPO --apply
```

The first command only previews. `--apply` verifies and caches the approved Core
source, creates a separate installation branch/PR, reads back the actual workflow
contents and PR identity, and creates absent lifecycle label definitions. It does
not label Issues, overwrite unknown files, push the default branch or merge.
Exit 2 means installation still needs review/merge or its effect is unknown;
inspect the provider result before retrying.

Existing identical files and label names are preserved. Changed workflows/pins
are refused; this is initialization, not the deferred upgrade feature. An existing
installation branch is reported for inspection rather than blindly overwritten.
After authorized PR merge, unchanged initialization reads back without new writes.

To download, verify and register the official Runner as part of onboarding, add
`--runner-directory ABSOLUTE_PATH --runner-name NAME` to the init command.
Preview remains read-only. Apply waits for the installation PR to be merged:
rerun the same command after that merge to register the Runner. Existing
registrations are checked against the repository and local Runner settings.
An uncertain registration requires reconciliation before another attempt.

Registration is one step in host adoption. The result reports that adoption is
still required. Use the [Core host instructions][core-host] for the supported
Linux backend, including the administrator-owned service, Docker isolation and
approved credential setup, then run `gh nibfin preflight`. An actual authorized
delivery is required to establish end-to-end readiness.

[core-host]: https://github.com/taco3064/nibfin-core/blob/main/docs/host-launcher.md

Personal and Organization public owners share the scoped initialization API.
Both have complete positive/negative apply contracts, including PR/label read-back
and unchanged reruns. Real Personal candidate installation has been exercised;
live Organization installation and specific Organization policies are **Unverified**.
No Organization/Admin identity is required as an extra Phase 0 prerequisite.
Initialization needs repository/workflow writes; Runner management separately
requires the actual repository Admin rights described below.

## Sources and trust boundary

`internal/cli/producers.json` is the single candidate pin set. Initialization
checks public producer repositories and full commits, and compares the Action's
actual `core-source.json` to its approved Core pin. Before remote writes, it
downloads the fixed Core CI artifact, checks the native ZIP digest, archive digest
and pin, then uses Core's own verifier for the actual cache files and Manifest.
Expired or unavailable artifacts fail closed; there is no floating-main fallback.

The cache is user-owned candidate source, not `/opt/nibfin/core` or a trusted
runtime installation. Its temporary bootstrap verifier comes from the approved
archive; an existing cache cannot replace that verifier. The selected `node`
must match Core's own `.node-version`. For an fnm-managed installation, for example,
run the CLI inside the corresponding `fnm exec --using VERSION -- ...` environment.

Core's current host installer uses a directory replacement operation that returns
`EPERM` on Windows. CLI source caching instead reserves a new directory, safely
extracts the hash-verified archive and delegates content verification to Core.
This does not change the privileged Core backend or prove Windows execution safety.

Initialization reads Core's complete Issue-event and Dispatch caller recipes
from its immutable public source and renders only the fixed commit placeholder.
The CLI keeps no second copy of wake policy. The Action direct-stage caller uses
the stable current-user Runner label. All entries require separate administrator
adoption and native Core authorization. The CLI does not
issue grants, configure secrets, start Agents, select Assignees, schedule Runners
or advance delivery stages. Installing workflows does not prove authenticated
native Issue execution or the first external E2E. Unknown existing file contents
are still refused, including installations pinned to an earlier candidate.

## Verification status

Tests cover Personal/Organization public repositories, private/missing permission
denial, immutable producer mismatch, unknown workflow edits, unchanged installation,
PR/content read-back and unknown writes without retry. A separately enabled live
read test downloads the real approved archive, creates a disposable source cache
and runs the actual Core verifier. It does not register a Runner or create a grant.
CI distinguishes source-cache checks from protected host/Agent/E2E evidence.

Formal G07 publication follows the independently owned first external protected
E2E, compatible formal Core/Action Releases and separate Owner authorization.
The manual tracker remains open and has no Nibfin lifecycle labels.

## Optional member registration

Use `gh nibfin init --quick request/issue/register/confirm/reconcile` for an
opt-in, one-attempt handoff between an authenticated Repository Admin and member.
Only the short-lived repository registration capability crosses that boundary;
management credentials stay with the Admin. Independent management-origin
confirmation, GitHub member eligibility and native Runner read-back are required.
Host adoption and protected execution remain unverified. See the complete
[management/member sequence, expiry and recovery guide](docs/quick-registration.md).
Existing Admin onboarding and GitHub manual registration remain available.

## Manual Repository Runner management

These commands require the authenticated user's actual repository Admin rights,
including the applicable official Runner API permission. Ordinary members receive
a denial. The optional member handoff uses separate management operations.
Personal and Organization public repositories use the same repository-level API.

```text
gh nibfin runner create --repo OWNER/REPO --directory ABSOLUTE_PATH --name NAME
gh nibfin runner create --repo OWNER/REPO --directory ABSOLUTE_PATH --name NAME --apply
gh nibfin runner register --repo OWNER/REPO --directory ABSOLUTE_PATH --apply
gh nibfin runner list --repo OWNER/REPO
gh nibfin runner unregister --repo OWNER/REPO --directory ABSOLUTE_PATH --apply
gh nibfin runner remove --repo OWNER/REPO --directory ABSOLUTE_PATH --apply
```

Omit `--apply` to preview mutations. The parent directory must already exist;
the Runner directory must be a new private location outside the current worktree.
Use a parent controlled by the current OS user (for example below the user's home
directory). Shared-writable ancestors or Windows ACLs allowing unrelated writers
are rejected, including temporary directories exposed to other sandbox accounts.
Creation downloads the official platform package, verifies its native SHA-256,
extracts it with bounded paths/types/sizes and retains the archive for provenance.
No service or background Runner process is installed or started.

Registration rechecks the current official package identity and all packaged
files, requests one short-lived repository registration token and invokes only
the verified official `Runner.Listener configure`. The stable label derives from
the authenticated current GitHub User ID. Tokens are passed through the official
Runner's masked `ACTIONS_RUNNER_INPUT_TOKEN` environment input, never argv,
management JSON or CLI output. GitHub administration/model credentials and runtime
injection variables are not inherited by that child. See the
[official Runner implementation](https://github.com/actions/runner/blob/v2.338.2/src/Runner.Listener/CommandSettings.cs).
The official Runner also loads root `.env` at startup; that file is refused before
token issuance and every native configuration command, without deleting it.

Read-back checks the actual native Runner ID/name/member label and local `.runner`
identity. These are management results, **not Core admission or safe delivery**.
Core must independently verify Runner/job/member/workflow/OIDC and isolation.

`remove` deletes only the scoped, managed remote registration. It refuses busy or
online Runners; stop the official Runner locally first. It reads back both the
selected ID and current-repository listing, and retains local configuration/files.
`unregister` uses the official removal token and verifies remote absence plus
removal of known local configuration. After a confirmed remote-only removal, it
uses the official local-only removal command without requesting another token.
Existing `.service` configurations require official manual service management;
this CLI does not install, stop or remove OS services.

The private `.nibfin-runner.json` is a bounded installation/management record,
not an Issue lifecycle store or authority grant. An exclusive local lock and
Pending management state prevent duplicate/unknown configuration attempts.
Pending intent is synchronized before external effects: Unix uses file and
directory sync; Windows uses local NTFS write-through and FlushFileBuffers.
Unsupported persistence fails before issuing a token or changing registration.
These rely on the OS/storage flush contract, not a claim of power-cut testing.
See Microsoft's [write-through contract](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilea).
A timeout, partial local removal, unknown token request or unreadable native result
requires inspection; no automatic retry or stale-lock takeover is provided.
Unknown processes/configuration must be resolved through official administrative
inspection before manually changing that state. Do not delete active Runner files.

Package changes or official auto-updates may require official manual removal or
recreation from the current package. This candidate does not implement the
deferred general upgrade feature. Installed package files are retained for reuse
and provenance; dispose of them only after registration/service/process use is
retired and no recovery evidence remains necessary.

## Read-only environment inspection

```text
gh nibfin preflight --repo OWNER/REPO
gh nibfin preflight --repo OWNER/REPO --directory ABSOLUTE_RUNNER_PATH
```

The JSON report separates host OS/architecture, current repository Admin rights,
tool execution, Codex login presence, Docker engine observations and managed
official Runner package/runtime verification. It never prints raw tool output,
login status text, endpoint addresses or provider errors. `codex login status`
only establishes a recognized local login mode; it does not execute an Agent role
or prove network access, model entitlement or protected delivery.

Docker inspection refuses ambient endpoint/configuration overrides and remote or
unknown context endpoints. It queries only the selected local Unix socket or
Windows named pipe, without mounting it or creating containers. Linux engine,
Desktop, rootless and AppArmor reports are observations, never an isolation test.
Tool executables from a Git worktree or temporary directory are rejected.

The pinned Core candidate has no public read-only isolation-proof entry.
Consequently this command reports that precise limitation and exits **2** with
`protected_execution: unverified`, even when all observed prerequisites exist.
It does not reimplement Core admission, install services, inspect credential
files, or claim Windows/macOS/Linux protected support from a version command.
Real Runner/job/OIDC authorization, Agent roles and isolation need the separately
authorized protected execution and external E2E evidence before formal release.

The maintained [snapshot provider contract](docs/snapshot.md) exposes bounded,
read-only native GitHub facts to future Dashboard consumers through
`internal/snapshot`. Core verification remains explicitly Unknown because no
supported evidence reader is available. This provider adds no Dashboard command,
HTTP service or control permission.

# gh nibfin

GitHub CLI extension for Nibfin public-repository onboarding, manual
repository-level Runner management, safe upgrade proposals and environment inspection.

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
are refused by init; use the verified upgrade command below. An existing
installation branch is reported for inspection rather than blindly overwritten.
After authorized PR merge, unchanged initialization reads back without new writes.

With a compatible Core pin on Linux x64, plain init plans two shared Runner
slots. The first repository supplies the execution workflow. On the same
adopted host, init for a second repository verifies and reuses those slots.
Each repository keeps its own Issue, CI and project environment.
`preflight` reports observed registrations and their online state. This
candidate still requires administrator-managed host adoption and service
startup before it can execute protected jobs.
After the execution workflow has merged, Linux x64 init stages both official
Runner packages. The administrator can register a staged slot using
`gh nibfin runner register --shared --repo OWNER/REPO --directory PATH --apply`
under the intended Runner account, while the service is stopped. Supply its
authorized GitHub session for this invocation only. Keep the administrator's
GitHub login store outside the Runner account and clear the temporary credential
before starting the service.
With the packaged systemd template, prepare the first package in
`/var/lib/nibfin/runners/slot-1` and the second in
`/var/lib/nibfin/runners/slot-2` before registering. Those fixed directories
are bound to the corresponding slot names in the local registration record.
A repeated registration checks the original offline Runner ID.
After Core host adoption, run `init --apply --install-shared-service` for the
execution repository to install the verified unit. This uses the host
administrator's existing permission. Review the unit installation result
before enabling the two services.
After enrolling a peer repository on the host, run
`init --apply --prepare-shared-wake` for that repository to create the
verified destination variable while shared dispatch is disabled. Existing
different destinations require inspection. Manage the Wake secret and
activation separately after validating the host.

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

## Preview and propose an upgrade

```sh
gh nibfin upgrade --repo OWNER/REPO
gh nibfin upgrade --repo OWNER/REPO --version v1.2.3
gh nibfin upgrade --repo OWNER/REPO --version v1.2.3 --apply
```

Upgrade previews by default. It resolves an official stable release to a full CLI
source commit and verifies its producer tuple, archives and Core-owned recipes.
It recognizes the installed managed files at one exact base, preserves the
original member binding, and lists ordered additions, modifications and deletions.
The version above illustrates the syntax; it does not assert a published release.
Missing or inconsistent official metadata fails closed without writes.

Only explicit `--apply` can create a separate upgrade branch and PR. Native
review, CI, protections and merge still apply. This neither installs a CLI binary
nor updates a running Core host or Runner. Host adoption is a separate trusted
operation. See [upgrade usage and recovery](docs/upgrade.md) for supported layouts,
source verification, pending effects and the limits of local fixture evidence.

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
read-only native GitHub facts to Dashboard consumers through
`internal/snapshot`. Core verification remains explicitly Unknown because no
supported evidence reader is available. The provider adds no control permission.

## Local dashboard

Run `gh nibfin dashboard [--repo OWNER/REPO]` with your existing GitHub login.
Omitting `--repo` selects the current repository through GitHub CLI context; use
`--repo` when discovery is unavailable. Actual repository read access is required,
without push/admin rights. The command opens the local session in your browser
and stays in the foreground. Press Ctrl+C to stop; closing a tab does not stop it.
Shutdown waits within the existing collection and transport bounds. The printed
origin contains no session proof. See [the dashboard contract](docs/dashboard.md)
for session protection, snapshot limitations and shutdown bounds. Local source
tests do not certify native browsers, logged-in providers or released support.

## Local release artifacts

From the repository root, using the Go toolchain pinned in `go.mod`:

```sh
go run ./scripts/release --version v1.2.3
./dist/gh-nibfin_linux_amd64 version
```

The version is required and follows SemVer 2.0.0: `MAJOR.MINOR.PATCH`, optionally
followed by `-prerelease` and `+build` dot-separated identifiers. Numeric core and
numeric prerelease identifiers cannot have leading zeroes. One optional lowercase
`v` is removed. The normalized version is embedded and recorded in the manifest.
`gh nibfin version` and `gh nibfin --version` report the same value when invoked
as a local extension. Ordinary source builds report `gh-nibfin development`.
Version reporting does not authenticate or read providers.

The producer cross-builds this checkout with CGO disabled, `-trimpath`, and linker
flags `-s -w`. It creates only the following persistent output in `dist/`:

- `gh-nibfin_linux_amd64` and `gh-nibfin_linux_arm64`
- `gh-nibfin_darwin_amd64` and `gh-nibfin_darwin_arm64`
- `gh-nibfin_windows_amd64.exe` and `gh-nibfin_windows_arm64.exe`
- `manifest.json` and `SHA256SUMS`

`manifest.json` is an object with `version` (normalized string) and `targets`
(an array of exactly six objects). Each target contains `filename` (relative
basename), `os` (`linux`, `darwin`, or `windows`), `arch` (`amd64` or `arm64`),
`size` (bytes), and `sha256` (lowercase hexadecimal digest of executable bytes).
`SHA256SUMS` uses standard `digest`, two spaces, `filename` lines for each of the
six executables and then the manifest. On systems with sha256sum, verify using
`cd dist && sha256sum -c SHA256SUMS`.

Every invocation requires `dist` to be absent, including symlinks and empty
folders. Explicitly move or remove the previous output before another run; the
producer never overwrites unrelated files or mixes versions. There is no custom
output path. Builds and format validation occur in a temporary sibling directory;
it is removed on success and ordinary errors. Only a complete validated set is
renamed to `dist`. A terminated process may leave `.release-*` scratch directories;
remove these after confirming no producer is using them. Do not run simultaneous
producers in the same checkout.

For local installation, copy the executable for your host into a separate local
extension directory named `gh-nibfin` and name the executable `gh-nibfin`
(`gh-nibfin.exe` on Windows), then use `gh extension install .` there as described
above. Artifact production itself
never installs extensions, publishes releases, creates tags, or manages services.
Cross-compilation validates file formats and architectures; native macOS/Windows
execution and public availability require their own release verification.

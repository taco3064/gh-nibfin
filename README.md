# gh nibfin

GitHub CLI extension for Nibfin public-repository onboarding, manual
repository-level Runner management and environment inspection.

Manual [Bootstrap #1](https://github.com/taco3064/gh-nibfin/issues/1) is in progress.
The candidate implements previewed, PR-based initialization; no formal release
or protected delivery is claimed. Runner/preflight commands are still pending.
Core remains the sole owner of delivery policy, authorization and isolation.

Reviewed immutable candidates support the first authorized external E2E.
Formal Core/Action/CLI publication follows that E2E and Owner authorization.

## Build and use the candidate

Use the Go version pinned in `go.mod`. No third-party Go dependencies are used.
Clone this public repository and check out the reviewed full commit before
building; never silently follow `main` during an installed candidate execution.

```sh
go run ./scripts/check.go
go test ./...
go vet ./...
go build -buildvcs=true -trimpath -o gh-nibfin .
gh extension install .
gh nibfin help
```

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

Installed templates contain a pinned Core reusable wake caller and a pinned
Action direct-stage caller, with a stable current-user Runner label. Both require
separate administrator adoption and native Core authorization. The CLI does not
issue grants, configure secrets, start Agents, select Assignees, schedule Runners
or advance delivery stages. Merely installing workflows does not complete Core's
native Issue wake-up integration or first external E2E.

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

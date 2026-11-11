# Local dashboard service (G12)

`internal/dashboard.Start(ctx, Config{Repository: "owner/repo", Provider: provider})`
starts an optional, read-only service. Construct `provider` as
`snapshot.Provider{API: cli.GitHub{Process: cli.NativeProcess{}}}`. This reuses
`gh api --hostname github.com` with the current user's read access; it never calls
the installation push/admin gate. Repository selection is trusted startup
configuration. There is no browser repository selector or management operation.
The dashboard CLI remains G14; the packaged frontend is described below.

`URL()` returns the actual origin without credentials. Default binding is
`127.0.0.1:0`, so the OS selects an available port. `Config.Bind` accepts only a
literal loopback IP and canonical numeric port, including `[::1]:0`; it never
resolves hostnames. Startup failures return generic errors and own no resources.
Configuration is instance-local. No process-global session or snapshot cache exists.

## Local session handoff

The local caller deliberately opens `BootstrapURL()` directly in the browser.
Treat this return value as a secret: do not print it in ordinary logs, persist it,
put it in API queries, or route it through another website. The URL fragment is a
256-bit cryptographically random, per-instance session proof. It is not a GitHub
credential. Fragments are not sent in HTTP requests. No unauthenticated endpoint
returns this proof, credentials, or snapshot data.

The embedded shell takes the fragment into memory, immediately removes it with
`history.replaceState`, and waits for a user to select **Read snapshot**. Only
then does it issue `GET /api/snapshot` with `X-Nibfin-Session: <64 lowercase hex>`.
There are no cookies, browser storage, external scripts, redirects or automatic
credentialed API requests. Session URLs must be opened directly, avoiding redirect
services and browser URL synchronization. A foreign website cannot discover the
random fragment or complete the authenticated bootstrap automatically.

Tests or trusted callers may use `Config.Assets`, an explicit map of URL paths to
packaged bytes and content types, copied at startup. It must preserve the fragment
handoff, in-memory proof, deliberate user action, and contextual text escaping.
The service permits HTML, JavaScript and CSS content types only. It serves no
filesystem, directory listing, source files, fallback page or external dependency.
Use external packaged scripts; inline scripts and eval are blocked by CSP. Render
provider strings as text (the minimal shell uses `textContent`), never as HTML or
commands. Asset authors are trusted application code, not browser-supplied input.

Sessions expire 30 minutes after startup, without renewal. Expiry is checked before
collection and before disclosing a result. Reopen a new service for a new session.
`Close` and context cancellation invalidate proof; `BootstrapURL()` then returns
an empty string. `Now` and `Random` are test seams; leave them nil in production.

## HTTP surface and errors

Each request must have the exact bound Host, including port. An Origin header, if
present, must equal `URL()` exactly, with no trailing slash. Duplicate or malformed
Origin values, `null`, and foreign origins are forbidden. Fetch Metadata, when
present, must be `same-origin` or `none`. Forwarded headers confer no authority and
are ignored. No CORS permission or preflight grant exists. Requests without Origin
or browser metadata still need the full session proof on every snapshot read.
The service does not protect against a compromised local caller or browser.

Only exact `GET /api/snapshot` collects facts. All routes reject queries (even an
empty `?`), bodies, transfer encodings, encoded/ambiguous paths and absolute-form
request targets. Only exact allowlisted asset paths are served. There are no
writes, controls, shell requests or redirects. Session headers on assets are
rejected. Application errors have JSON shape `{"error":"<safe_code>"}`:

| Status | Safe code | Meaning |
| --- | --- | --- |
| 200 | none | Snapshot JSON or packaged asset; not proof of complete facts. |
| 400 | invalid_request | Query, body, ambiguous path or proof on an asset. |
| 401 | unauthorized | Missing, malformed, wrong or expired session proof. |
| 403 | forbidden | Host, Origin or Fetch Metadata rejection. |
| 404 | not_found | Unknown exact path; no filesystem fallback. |
| 405 | method_not_allowed | Only GET is allowed; no OPTIONS/CORS grant. |
| 503 | unavailable | Collection slot occupied or service stopping. |
| 500 | unavailable | Snapshot serialization failed; no diagnostic disclosure. |

Validation order is authority, service state, method, request shape, route and
session proof. Denials before collection perform no provider calls. Standard Go
HTTP parser/header/read/write deadline failures may close the connection or
return a native protocol error before application routing. The header budget is
8 KiB plus Go's parser allowance (4 KiB); read/header deadlines are five seconds,
idle connections fifteen seconds. All application responses use no-store,
no-referrer, nosniff, same-origin resource policy, frame denial and a restrictive
CSP. JSON serialization escapes markup; error bodies contain no raw diagnostics.

A 200 snapshot preserves schema version 1, nullability, independent statuses,
partial facts, unresolved references and Unknown/unverified Core claims from
[the snapshot contract](snapshot.md). Provider Denied, offline or unavailable
facts remain typed snapshot statuses, distinct from HTTP authentication failures.
The service calls `Snapshot.At(now)` before serialization, including for injected
collectors retaining observations. It never converts provider failures to empty
success, verified completion or permission to act. GitHub tokens, raw Agent logs,
bodies, provider errors and private host paths are not added to the projection.

## Resource bounds and lifecycle

One collection per instance runs synchronously; concurrent authenticated requests
receive 503, rather than queueing additional collectors. A disconnected browser
does not free that slot until collection actually returns. No collection is
abandoned in a background goroutine. The native provider has a two-minute
collection deadline plus at most one final two-minute transport call. Custom
`Collector` implementations must also return within `CollectionBound` (four
minutes), with equivalent per-call transport bounds. Custom clocks must advance
normally during production collection. A collector is trusted local code, not
an extension API for browser-controlled executables.

The socket write deadline is `RequestBound` (four minutes plus five seconds),
allowing that documented final-call overrun and bounded response writing without
streaming partial JSON. There is no claim of immediate API/process cancellation.
Call `Close()` after use, including on context cancellation. Close is concurrent,
idempotent, invalidates the session first, stops accepting connections and drains
active handlers with a `ShutdownBound` of four minutes plus ten seconds. It waits
for the synchronous collector; it does not claim shutdown finished while an
ordinary bounded collection is active. Violating the custom collector bound can
cause a generic shutdown error; arbitrary non-returning injected code cannot be
forcibly terminated by Go. `Close()` reports serving/shutdown failures safely.
No persistent resources, credentials, registrations or workers survive ordinary
shutdown. Callers should preserve shutdown errors rather than claim clean exit.

## Verification boundary

Local tests use real loopback listeners and official-API-shaped synthetic fixtures,
including failures, isolation, expiry, concurrent collection and in-flight stop.
They require no credentials. The existing affected-package CI selector discovers
`internal/dashboard` on Linux and Windows; no native dependency is added. Actual
Windows, real-provider and browser validation must be reported separately when
observed. This service is a consumer seam, not a release/support claim.

## Packaged dependency frontend (G13)

The default embedded assets now provide two views: Repository and Native dependency
graph. Filters and selected Issue details live inside the graph view. Select
**Read snapshot** to collect; the same button deliberately refreshes. A failed
refresh retains explicitly marked previous data. Neither filtering nor following a
dependency performs another request. The diagram is an optional scrollable overview;
the adjacent Issue cards expose the full directed relationships and keyboard
navigation, including dependencies hidden by filters and external references.

The frontend consumes schema version 1 only. Unknown additive fields are ignored.
Repository-qualified identities include provider IDs; external or unresolved
references never become collected nodes. Facts, independent collection status,
observation/expiry times and safe provenance remain visible. Empty, null, partial,
Denied and stale have distinct meanings. The browser re-evaluates expiry while the
page stays open, downgrading retained collection, dependency resolution and check
statuses. Invalid or contradictory timestamps cannot establish current data.
GitHub state, progress labels and repository activity establish no Core state.
Version 1 has no supported Core evidence reader or control eligibility/outcome
information, so the UI explicitly shows that limitation without offering controls.

Production resources are local packaged HTML, JavaScript modules and CSS. They
load under the existing CSP with no inline script or eval exception. Text uses DOM
text nodes with visible control-character notation. Links must match exact official
HTTPS GitHub resource identities and isolate new tabs from the opener. No cookie,
persistent browser storage, external assets or direct GitHub browser fetch is used.

G12's `Start` embeds and allowlists the maintained frontend by default; callers do
not need to supply `Config.Assets`. G14 owns CLI composition: construct the native
provider and service, deliberately open `BootstrapURL()` through its authorized
local handoff, keep that secret out of logs, and call `Close()` when finished.
`URL()` is the non-secret origin. This work adds no CLI command or control API.

### Reproducible rendered browser checks

Prerequisites: the Go version pinned in `go.mod`, Node.js 24 with npm, a supported
Chromium host with its system libraries, and access to the npm and Playwright browser
registries. No GitHub login, credential or administrator account is needed.

```sh
npm ci --ignore-scripts --no-audit --no-fund
npm run test:dashboard
```

The locked Playwright development dependency is never served to the browser. The
runner downloads Chromium into a unique temporary directory, builds the synthetic
collector host there, starts the actual Dashboard service with default assets, runs
one browser worker, and closes hosts and deletes downloads, results and binaries
on completion. Browser tests use the existing service clock seam with fixed
synthetic observations, plus Playwright's browser clock for expiry. Malformed
response cases use explicitly intercepted snapshot responses. These are fixture
results, not real-provider performance or live GitHub comparisons.

Before browser setup, the runner executes child-process regression checks with
`node --test internal/dashboard/browsertest/child.test.mjs`. Only an explicit
successful child exit passes; signal termination and spawn errors fail the runner.
The regression suite exercises a real signal-terminated child and a missing
executable through the same exit-status interpretation used by browser tests.

The existing affected-test entry installs the locked npm dependencies and invokes
the suite when Dashboard, snapshot, browser fixture/tooling, manifest/lockfile or
selector changes require it. A browser/setup failure fails that entry. In a native
checkout, run `go run ./scripts/staged`. In an isolated snapshot without Git metadata,
supply known paths instead, for example:

```sh
GOFLAGS=-buildvcs=false go run ./scripts/staged --paths \
  internal/dashboard/assets/index.html package.json
```

`--cache` retains the existing native cache selector behavior. Full Git-dependent
selection and VCS stamping require the native PR checkout. `scripts/check.go`
enforces complete-file size/line limits for Go, frontend and other program source
extensions, excludes dependency directories, and still enforces Go formatting and
the pinned toolchain. JSON/YAML data remain exempt.

The required local checks are the affected-test entry above,
`go run ./scripts/check.go`, `go vet ./...`, and an application build. Where no Git
metadata exists, use `GOFLAGS=-buildvcs=false` for Go package discovery/builds;
that does not establish native VCS-dependent verification. Browser coverage here
is Chromium on Linux with synthetic collectors, including 150 Issues, cycles,
qualified external references, keyboard/mobile interactions, hostile text/links,
independent statuses, expiry and safe errors. Native Windows, other browsers,
real GitHub comparison (G16), CLI launch/release integration (G14), PR-head CI and
supported-product verification remain separate and must be reported independently.
After local checks, remove the disposable `node_modules` directory when no longer
needed; retain the maintained manifests, fixtures and source.

Local execution observed on 2026-11-11: all 37 rendered tests passed with Playwright
1.56.1 and Chromium Headless Shell 141.0.7390.37 on Linux amd64, using Go 1.27.1
and Node.js 24.22.1. The affected selector also passed the four selected Go test
packages. This isolated snapshot has no usable Git metadata: automatic Git diff
selection and VCS stamping are Unverified here, with known changed paths and
`-buildvcs=false` used for local verification. Native PR-head CI must run its
normal Git-dependent checks in its own checkout.

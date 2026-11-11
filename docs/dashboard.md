# Local dashboard service (G12)

`internal/dashboard.Start(ctx, Config{Repository: "owner/repo", Provider: provider})`
starts an optional, read-only service. Construct `provider` as
`snapshot.Provider{API: cli.GitHub{Process: cli.NativeProcess{}}}`. This reuses
`gh api --hostname github.com` with the current user's read access; it never calls
the installation push/admin gate. Repository selection is trusted startup
configuration. There is no browser repository selector or management operation.
The dashboard CLI launch/integration remains G14 responsibility.

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

## Packaged frontend (G13)

G12 now serves the maintained Repository view and native dependency graph by
its default embedded assets. The graph has a directed SVG overview and equivalent
keyboard accessible textual node/edge list:
`dependent → blocker`. All nodes have native buttons; all edges have equivalent
text and blocker navigation in the selected node's details. The bounded node
region scrolls with keyboard or touch. Filters affect visibility only; dependency
text explicitly marks hidden nodes/references. Selection survives filtering.
External and unresolved endpoints stay references, with independent resolution
warnings. Repository PRs, head checks and runs stay repository activity.

The session script owns only memory-only bootstrap and deliberate collection.
`model.js` owns interpretation, identity, safe links and status downgrade rules;
`view.js` owns inert DOM presentation; `graph.js` owns deterministic visual layout;
`app.js` owns selection and filter state.
Retained observations are checked every 250 ms without a new request. Invalid
windows and expiry downgrade every displayed collection and resolution, including
PR checks; Denied remains Denied/incomplete. A failed refresh keeps the old facts
explicitly marked Previous data. The current provider has no supported Core
reader: completion, pause, execution, controls, requested/applied outcomes and
eligibility remain Unknown/unavailable regardless of native activity.

G14 should construct the existing service, deliberately open `BootstrapURL()`
without logging it, retain ownership of `Close()`, and use the default assets.
No new route, authorization mechanism or browser repository selection is needed.

### Rendered-browser verification

Use Go 1.27.1, Node.js 24 and the locked development dependency:

```sh
npm run test:dashboard
```

This command installs the locked development dependencies and Chromium headless
shell into one disposable temporary directory, runs the suite, and removes the
entire environment on success or failure. No tree-local dependency installation
is needed. Downloads use the permitted npm/browser registries. Native browser
system libraries must already be installed; the runner never installs OS packages.
For an existing local development installation, the equivalent explicit commands
are:

```sh
npm ci --ignore-scripts
PLAYWRIGHT_BROWSERS_PATH=/tmp/nibfin-browsers npx playwright install chromium --only-shell
PLAYWRIGHT_BROWSERS_PATH=/tmp/nibfin-browsers npm run test:dashboard:installed
```

The suite starts the actual service with default packaged assets and a synthetic
collector via `TestBrowserService`. Bootstrap passes through a private subprocess
pipe, not an artifact. Exceptional malformed snapshots/HTTP responses use browser
interception explicitly. The fixture and browser context use a deterministic
clock; 150 Issues include cycles, shared blockers and qualified external
references. Network assertions cover same-origin assets and the sole snapshot
read, header-only proof, deliberate loading, no interaction collections, no
storage and no executable provider markup. Bounded tests own and close the
browser/service and remove temporary snapshot files.

The existing affected-test entry selects the browser suite for Dashboard,
browser-test, manifest/lockfile and selector changes. Without a Git index, supply
changed paths directly, for example:

```sh
go run ./scripts/staged internal/dashboard/assets/app.js
```

That entry provisions the same disposable environment; browser failures propagate.
Native PR-head CI still owns Git-dependent selection
and platform checks. `go run ./scripts/check.go` checks complete Go, JS, MJS,
TS, HTML, CSS and shell source files against the source limits, excluding dependency
and distribution directories. JSON/YAML data remain exempt.

Local synthetic coverage establishes rendered Chromium on Linux only. Native
Windows, other browser engines, real GitHub comparison, CLI integration/release
and supported-product verification remain Unverified here and belong to their
existing authorized boundaries. Remove disposable browser downloads and
`node_modules` after verification; neither is a Candidate artifact.

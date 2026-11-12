# Local dashboard service (G12)

`internal/dashboard.Start(ctx, Config{Repository: "owner/repo", Provider: provider})`
starts an optional, read-only service. Construct `provider` as
`snapshot.Provider{API: cli.GitHub{Process: cli.NativeProcess{}}}`. This reuses
`gh api --hostname github.com` with the current user's read access; it never calls
the installation push/admin gate. Repository selection is trusted startup
configuration. There is no browser repository selector or management operation.
The CLI entry point is `gh nibfin dashboard [--repo OWNER/REPO]`. The default
packaged UI renders the repository summary and native Issue dependency graph.

`URL()` returns the actual origin without credentials. Default binding is
`127.0.0.1:0`, so the OS selects an available port. `Config.Bind` accepts only a
literal loopback IP and canonical numeric port, including `[::1]:0`; it never
resolves hostnames. Startup failures return generic errors and own no resources.
Configuration is instance-local. No process-global session or snapshot cache exists.

## Foreground CLI lifetime

Run `gh nibfin dashboard --repo OWNER/REPO` with your existing GitHub CLI login.
Without `--repo`, the command resolves the current repository using `gh repo view`;
if discovery fails, supply `--repo` explicitly. Startup requires a valid current
user and matching repository identity with read permission, without push/admin.
Help performs no login, service or browser operation.

The command starts one service and deliberately hands its fragment bootstrap URL
to the native browser opener. It prints only the credential-free origin and stop
instructions. Opening does not certify page rendering or snapshot availability.
Keep the command in the foreground; press Ctrl+C to stop. Closing a browser tab
does not stop the service. Interrupt and termination signals invalidate the session
and wait for the existing bounded shutdown, including active snapshot collection.
Browser handoff failure also closes the service. Startup, browser, serving and
shutdown errors are reported without raw process diagnostics or the secret URL.
Native browser/platform and logged-in provider support require actual external
verification; synthetic local tests do not establish released Dashboard support.

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

Trusted callers may replace the UI using `Config.Assets`, a map of URL paths to
packaged bytes and content types, copied at startup. It must preserve the fragment
handoff, in-memory proof, deliberate user action, and contextual text escaping.
The service permits HTML, JavaScript and CSS content types only. It serves no
filesystem, directory listing, source files, fallback page or external dependency.
Use external packaged scripts; inline scripts and eval are blocked by CSP. Render
provider strings with `textContent`, never as HTML or
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

## Repository and dependency views

The repository summary shows collected Issue counts and independent Core claims.
Its activity disclosure contains PRs, checks and Actions runs with native PR
references. These are repository activity, not inferred Issue associations.
Completion, pause and execution evidence remain separate from GitHub state and
native progress labels. Missing evidence stays Unknown/unverified.

The directed graph uses readable dependent → blocker rows. Buttons select collected
Issues; external and unresolved endpoints remain explicitly labeled references.
The textual blocker disclosure conveys the same qualified identities, direction
and resolution limits. Search matches qualified identity or title. State and native
progress filters retain hidden blocker context, and hidden collected blockers can
still be inspected. Selection survives filtering until a refreshed snapshot removes
the Issue. Known-empty, unavailable and partial collections have different messages.

The freshness banner shows observation and expiry times. A memory-only timer marks
retained facts stale at expiry, without silently fetching more data. A failed refresh
marks retained observations as not current. Unsupported schema versions and service
failures show a fixed actionable error, never provider diagnostics.

Only exact official GitHub resource URLs matching the represented repository and
resource identity become links. Links use no-referrer and noopener/noreferrer.
Provider text is inert; control and bidirectional formatting characters are replaced
with a visible replacement character. No third-party runtime scripts or styles load.
The layout wraps at narrow widths; native controls and Issue buttons are keyboard
operable with visible focus. Selection moves focus to the details region.

## Verification boundary

Local tests use real loopback listeners and official-API-shaped synthetic fixtures,
including failures, isolation, expiry, concurrent collection and in-flight stop.
They require no credentials. The existing affected-package CI selector discovers
`internal/dashboard` on Linux and Windows; no native dependency is added. Actual
Windows and real-provider validation must be reported separately when
observed. This service is a consumer seam, not a release/support claim.

### Packaged browser regression suite

Use the Go version in go.mod and Node 22 or newer:

```sh
npm ci --ignore-scripts --no-audit --no-fund
npx --no-install playwright install chromium
npm run test:browser
```

The suite builds a disposable synthetic Collector host using the existing service
and default embedded assets. It uses real Chromium and loopback HTTP. It covers
session handoff, directed and cross-repository edges, unresolved references, filters,
independent status failures, expiry, unsupported schema, hostile text/URLs and
keyboard/mobile navigation over 130 Issues. An intercepted HTTP failure and an
extended response check client error handling and additive-field compatibility;
these are not provider results. Every test closes its
browser and service and removes fixture directories; the build is removed on exit.
No browser reports, profiles or snapshots are maintained artifacts.

The affected-test entry installs locked JavaScript dependencies only when selected.
Hosted CI uses preinstalled official Chrome through Playwright's chrome channel;
it never installs browsers or OS packages. Dashboard source,
browser fixtures/tooling, dependency manifests and shared verification changes select
the suite. Unrelated changes keep targeted checks. A selected suite fails if Node,
dependencies, browser or assertions are unavailable. Both verify and Windows
affected checks use the same entry point.
Local runs use separately provisioned Playwright Chromium. Run the selector without
Git metadata by passing known changed paths, for example:

```sh
GOFLAGS=-buildvcs=false go run ./scripts/staged --paths internal/dashboard/assets/view.js
```

--browser reports browser selection; --cache reports the existing cache selection.
Both support --paths. Native CI still uses the actual Git comparison. The source
checker enforces the 90-character/1,000-line limits for Go, JS/MJS, CSS and HTML.
Synthetic browser results establish neither live GitHub acceptance nor release or
platform support. Keep those evidence boundaries explicit.

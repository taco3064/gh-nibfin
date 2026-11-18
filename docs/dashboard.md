# Local dashboard service (G12)

`internal/dashboard.Start(ctx, Config{Repository: "owner/repo", Provider: provider})`
starts an optional local service. Construct `provider` as
`snapshot.Provider{API: cli.GitHub{Process: cli.NativeProcess{}}}`. This reuses
`gh api --hostname github.com` with the current user's read access; it never calls
the installation push/admin gate. Repository mode uses trusted startup selection.
Overview mode permits authenticated navigation among permission-checked adopted
repositories. Pause/Resume uses a trusted injected
`pause.Controller`, assembled by the CLI with `cli.NativePause` and the current gh login.
The CLI entry point is `gh nibfin dashboard [--repo OWNER/REPO]`. The default
packaged UI renders the repository summary and native Issue dependency graph.

`URL()` returns the actual origin without credentials. Default binding is
`127.0.0.1:0`, so the OS selects an available port. `Config.Bind` accepts only a
literal loopback IP and canonical numeric port, including `[::1]:0`; it never
resolves hostnames. Startup failures return generic errors and own no resources.
Configuration is instance-local. No process-global session or snapshot cache exists.

## Foreground CLI lifetime

Run `gh nibfin dashboard --repo OWNER/REPO` with your existing GitHub CLI login.
In repository mode, omitting `--repo` resolves the current repository using
`gh repo view`; if discovery fails, supply `--repo` explicitly. Repository startup
requires a valid current user and matching repository identity with read permission,
without push/admin. `--overview` authenticates the user without inferring a repository.
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
put it in API queries, or route it through another website. The URL fragment carries
a 256-bit cryptographically random, per-instance session proof and an optional
overview mode suffix. The proof is not a GitHub
credential. Fragments are not sent in HTTP requests. No unauthenticated endpoint
returns this proof, credentials, or snapshot data.

The embedded shell takes the fragment into memory, immediately removes it with
`history.replaceState`. Repository mode waits for **Read snapshot** before issuing
`GET /api/snapshot` with `X-Nibfin-Session: <64 lowercase hex>`. Explicit overview
mode starts its authenticated stream automatically. Neither mode uses cookies,
browser storage, external scripts or redirects. Session URLs must be opened directly, avoiding redirect
services and browser URL synchronization. A foreign website cannot discover the
random fragment or complete the authenticated bootstrap automatically.

Trusted callers may replace the UI using `Config.Assets`, a map of URL paths to
packaged bytes and content types, copied at startup. It must preserve the fragment
handoff, in-memory proof, chosen entry mode, and contextual text escaping.
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

Exact `GET /api/snapshot` collects facts. Exact `POST /api/labels` accepts
bounded JSON review/apply intents. GET routes reject bodies. All routes reject
queries (even an empty `?`), transfer encodings, encoded/ambiguous paths and
absolute-form request targets. Only exact allowlisted asset paths are served.
There are no shell requests or redirects. Session headers on assets are
rejected. Application errors have JSON shape `{"error":"<safe_code>"}`:

| Status | Safe code | Meaning |
| --- | --- | --- |
| 200 | none | Snapshot JSON or packaged asset; not proof of complete facts. |
| 400 | invalid_request | Query, body, ambiguous path or proof on an asset. |
| 401 | unauthorized | Missing, malformed, wrong or expired session proof. |
| 403 | forbidden | Host, Origin or Fetch Metadata rejection. |
| 404 | not_found | Unknown exact path; no filesystem fallback. |
| 405 | method_not_allowed | Unsupported route method; no OPTIONS/CORS grant. |
| 409 | review_unavailable | Unknown, consumed or expired review; never retry edits. |
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

One repository collection or mutation per instance runs synchronously; concurrent
requests for that slot receive 503. Overview has its own serialized bounded collector;
its streams never occupy the repository collection slot. A disconnected browser
does not free that slot until collection actually returns. No collection is
abandoned in a background goroutine. The native provider has a two-minute
collection deadline plus at most one final two-minute transport call. Custom
`Collector` implementations must also return within `CollectionBound` (four
minutes), with equivalent per-call transport bounds. Custom clocks must advance
normally during production collection. A collector is trusted local code, not
an extension API for browser-controlled executables.

Repository responses use a socket write deadline of `RequestBound` (four minutes
plus five seconds),
allowing that documented final-call overrun and bounded response writing without
streaming partial JSON. There is no claim of immediate API/process cancellation.
Call `Close()` after use, including on context cancellation. Close is concurrent,
idempotent, invalidates the session first, stops accepting connections and drains
active handlers with a `ShutdownBound` of four minutes plus ten seconds. It waits
for the synchronous collector and the tracked overview collector; it does not claim shutdown finished while an
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

For UI changes, see the [Dashboard delivery guidance](dashboard-ui-delivery.md)
for design, visual checks and screenshot evidence.

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

## Fixed Pause/Resume requests

Existing Issue details offer Pause and Resume. Repository controls review Pause All
(open scoped candidates) and Resume All (observed paused scoped candidates).
The batch shows collected candidates and the eligible affected set before a separate
confirmation. Partial collections are labeled; batches over 50 candidates are disabled.
Later-created Issues and other repositories are never added. These are per-Issue
requests, without rollback or a persistent hold. After submitting, refresh observations
before another review. Cancellation performs no edits.

The single fixed mutation route accepts `application/json` up to 32 KiB, rejects
unknown/duplicate fields and validates positive native repository/Issue identities.
A review intent supplies `phase: "review"`, `repository`, `action` (pause/resume),
`batch` and `targets` (repository_id, issue_id, number). An apply intent supplies
only `phase: "apply"` and the opaque `review` token. Reviews belong to one service
session, expire after two minutes and are consumed before external effects. At most
16 unexpired reviews are retained. Snapshot responses advertise the authoritative
`label_action_limit` used by the packaged controls. Replayed or in-flight submissions cannot duplicate
an edit. Ambiguous external effects cause no automatic retry.

Native reads recheck the logged-in user, repository identity and permissions, Issue
identity/type, assignment, state and labels before each edit. Personal owners and
organization Admins do not require assignment or an online Runner. Other callers need
native label-edit permission and current assignment. Pause rejects closed Issues;
batch targets also require scope. A changed identity or unavailable evidence prevents
an edit. Resume does not depend on the original pause actor. Unverified actor evidence
is displayed as Unknown. These preflight checks do not replace Core event authority.

The only edit arguments are `gh issue edit NUMBER --repo OWNER/REPO --add-label pause`
or `--remove-label pause`. Native operations share a two-minute context deadline,
shortened by remaining session life and cancelled by shutdown. Already-matching labels
produce Not needed without editing. API/process diagnostics are never returned to the
browser. Native HTTP 403/404 rejections are definite request failures; other process
failures remain ambiguous. Provider text is rendered inertly.

Per-Issue and summary results separate submission (Requested, Request failed, Unknown,
Not needed), observed label (present, absent, unreadable), and final Core verdict.
Read-back failure does not erase a successful submission. Final Core verdict is always
Unknown because this implementation has no supported verdict reader. Removing a label
requests existing wake/recovery; it proves neither resumed delivery nor Writer termination.
Controlled Go and packaged browser fixtures cover these requests through the real local
handler. They establish no live GitHub/Core, multi-account or installed-platform result.

## User overview

`gh nibfin dashboard --overview` opens the signed-in user's local work overview.
It conflicts with an explicit `--repo`, including an empty one. Without `--overview`,
repository selection and inference retain their existing behavior.

The overview shows any number of local registrations, keyed by registration
repository ID and Runner ID, with merged adopted routes. Busy, idle, offline and
unknown are separate native observations. A recent verified busy observation has
an indicator beside visible text even when Issue or phase is unknown. The indicator
is static with reduced motion. After 30 seconds without a recent observation, the
card loses its current execution claim. Elapsed time requires an associated native
job start time. Source timestamps, uncertainty and observation provenance remain
available in disclosures.

Assigned open Issues are listed independently of Core's scope-only work discovery.
The lightweight native inventory includes non-scope Issues and excludes PRs. An
assignment, scope label or absent Core work record never proves admission or queue
state. Multiple native jobs remain evidence on one Issue row. Explicit no-admission
observations, queued jobs, dispatched runs, execution, qualified pause, dependencies
and unresolved external effects remain distinguishable. Pause does not mean that
admitted execution has stopped. Unknown and stale evidence never enables an action.

Issue buttons select only permission-checked adopted repositories and open the
existing local repository graph/detail view. Full graph, PR and Actions reads occur
on that path. Existing Pause/Resume review and permission checks remain in force.
Back to overview retains the overview search and selected Issue. Background updates
do not replace the repository detail display. This release adds no diagnosis or
new action controls.

### Observation and transport boundaries

Only the trusted installed Core is invoked, without installation or administrator
entry points. `observe-local-work` receives exactly a JSON `token` object on private
stdin using the current `gh auth token --hostname github.com` credential. Neither credentials nor raw
process diagnostics are served. Unsupported commands, inaccessible installation,
denial and partial discovery are explicit availability states. Supplemental existing
shared-host inspection can resolve adopted repository IDs even without scope work;
its two-slot installation decoder is not the Runner projection.

Overview native discovery is bounded to 32 adopted repositories, 80 native requests,
three pages per list, 300 Issues per repository and 1,000 Issues in total. The collection deadline includes
Core observation; one final bounded native transport may finish after it. Reads are
serialized separately from repository/detail collection. Background reads start ten seconds after the previous read finishes. Explicit
refresh requests coalesce and wait at least five seconds after the previous read. There is no persistent
work database. Partial results retain previously seen rows with their original
observation timestamps; only complete inventory can confirm their removal.

`GET /api/overview` returns the latest reconciliation envelope, or a pending response.
`GET /api/updates` streams `event: reset` SSE frames. Both require the existing
`X-Nibfin-Session` header, loopback authority/origin validation and unexpired session.
The browser uses fetch streaming, never EventSource or query-string proof. The
bootstrap fragment retains the existing ephemeral proof handoff, with an overview
mode suffix; it is removed before any API read. Responses remain no-store.

IDs are ordered integers within one service session. Every reset contains the current
bounded keyed projection. A reconnect's `Last-Event-ID` replays retained updates when
available; an absent, evicted or future cursor receives the latest reset. This is
in-memory convergence, not durable event history. The service retains eight updates,
limits each encoded update to 2 MiB, and allows eight simultaneous subscribers.
Streams end after one minute, below the existing HTTP write timeout, and writes have
five-second deadlines. Heartbeats and authorization checks occur every five seconds.
Browser reconnect delay grows from 500 ms to ten seconds and stops on denial/expiry. A 75-second client
watchdog also reconnects stalled transports; browser offline events abort the active
stream and enter bounded reconnect.
Shutdown cancels streams and waits for the bounded collector; no detached read is
started for each browser connection.

`POST /api/repository` accepts only `{"repository":"OWNER/REPO"}`, with the session
header, JSON content type and a bounded body. Selection uses the collected adopted
repository allowlist and the existing detail/mutation collection slot. Navigating
invalidates outstanding label-operation reviews; it adds no mutation authority.

### Additive integration contracts for #219

`OverviewUpdate` is `{id, kind: "reset", overview}`. `overview.schema_version` is 1.
`issues.items` rows have a stable `key` (repository ID plus Issue node ID), native
`identity` (including the separately retained database ID), `node_id`, `title`,
`state`, `assignees`, `observed_at`, and an `evidence` array of matched Core work.
`runners.items` use registration repository ID plus Runner ID. `work` retains Core
work independently of assignment so Runner association does not depend on the list.

Each collection exposes its own availability, completeness and safe reason. The
projection also exposes discovery status, Core availability/code, error codes,
observation time and expiry. Core field observations retain value, source,
observedAt, sourceTime, freshness and code. Consumers must evaluate field freshness
and native collection timestamps independently; a fresh envelope does not renew a
retained row. Retained Issue/Runner rows carry `retained: true` and lose current
execution claims immediately, independently of their observation age. A reset replaces the keyed view while keeping local filter/selection
state. These contracts convey facts and uncertainty, not permission to act.

Controlled Go and browser fixtures cover the adapter, projection and stream without
installed host state or production credentials. Hosted browser/CI checks own full
suite results. Installed-host rollout and live-provider support require separate
verification.

## Selected-Issue explanation and diagnosis

In the user overview, select an assigned Issue to open its repository details.
The explanation shows retained Core blocker reports by stage and admission
attempt. This attempt is not a GitHub run-attempt number. Native facts,
attributed explanations, possible causes and missing evidence have separate
labels. Historical reports cannot establish the current cause, even when the
collection is recent. Expand the evidence to inspect source times and safe
native links; the dependency graph remains available.

**Request read-only diagnosis** is explicit. Rendering, refreshing and reconnecting
only observe retained reports through the existing overview stream. The service
binds the repository ID, Issue database and node IDs, number and session subject;
the CLI revalidates current native identity and assignment before private stdin
transport to the trusted installed Core. Core independently owns authorization
and capacity. Older Core versions or unsupported platforms report unavailable;
the Dashboard does not install or upgrade anything.

A submitted request remains pending until a response arrives. Denial,
insufficient evidence, unavailable capacity and an unresolved effect are distinct.
An in-flight retained record does not prove running progress. Lost responses stay
unknown until retained evidence resolves them, with no automatic resubmission.
Requests use the current service/session lifecycle rather than the short
observation timeout. Diagnosis never resumes or reruns delivery. Pause and Resume
continue through the existing fixed review/apply path with native permission
checks, repository-scoped batches and single-use reviews. Navigation or stale
observations invalidate the visible review; a label submission still does not
prove a confirmed Core effect.

### Windows overview through an existing WSL 2 installation

From any Windows directory, including outside a Git checkout:

```powershell
gh nibfin dashboard --overview
# If more than one verified guest belongs to your current Windows gh user:
gh nibfin dashboard --overview --target wsl:Ubuntu-24.04
```

The service and browser stay on Windows. Discovery considers only running WSL 2
user distributions, excludes Docker Desktop, and does not run installation
preflight. Offline Runners and missing installation tools do not disqualify a
readable adopted Core. A sole verified target is selected; multiple verified
targets require `--target`. An explicit unavailable target never falls back to
another guest. No Git directory, default distribution or persistent target registry
participates in selection. Discovery checks candidates until all have been inspected
or a second verified target makes explicit selection necessary. Unavailable guests
do not hide an eligible target later in the inventory.

The guest must already have an adopted Core, its verified native Node executable,
and a compatible installed Linux CLI implementing the private overview bridge.
An older CLI or Core is an unavailable compatibility state; update it deliberately
inside the guest. The credential-free bootstrap uses the guest's system Python 3
and gh's Linux extension data location (`$XDG_DATA_HOME/gh/extensions`, otherwise
`~/.local/share/gh/extensions`). It resolves the canonical executable before
running it and rejects repository/temporary entries, foreign ownership and group
or world writable ancestry. A user-owned CLI is allowed. Core and Node retain
stronger root-owned installation and source verification.

Machine identity, native architecture and canonical CLI entry (including its file
identity) are bound to this Dashboard session. Every private read rechecks that
binding and the guest rechecks it before reading private stdin. If the target
changes, stops or disappears, restart deliberately with the intended target;
refresh stays on the bound guest. A failed verification read reports unavailable
for that refresh. The next refresh verifies the same guest before obtaining
credentials again.

Only after target verification does Windows obtain its current `gh` credential.
It travels as a token-only JSON object over anonymous stdin to that guest bridge
and the verified Core. The adapter does not use a guest gh login or administrator
credential. Tokens never appear in arguments, environment, URLs or temporary files.
The browser receives the shared sanitized observation projection without guest
paths or diagnostics. Denied, partial, offline and unavailable facts retain their
meanings; supplemental adopted IDs remain discoverable without active Issue work.
Inventory retains its 64 KiB bound; private Core observations retain their 1 MiB
contract with bounded wrapper metadata. Windows discovery and observation use the
Dashboard lifetime and propagate cancellation through the native transport. The
guest bridge uses its caller context without an additional observation deadline;
slow readable installations are not rejected by a fixed discovery or read timeout.

Controlled adapter and bridge fixtures exercise these paths without Windows,
installed WSL state or real credentials. Actual Windows/WSL installation behavior,
Windows browser rendering, release compatibility and hosted Windows CI remain
separate verification evidence; Linux fixture results do not certify them.

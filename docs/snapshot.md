# Read-only snapshot contract (schema version 1)

`internal/snapshot.Provider.Collect("owner/repository")` supplies the Go and
JSON contract for future local Dashboard consumers. Inject the existing
`cli.GitHub{Process: cli.NativeProcess{}}` as its `API`. Authentication stays in
the official `gh api --hostname github.com` transport under the current user's
existing permissions. No push/admin permission is required. The provider does
not install anything, invoke controls, persist data, or expose an HTTP route.

`Snapshot` is a timed projection, never an atomic GitHub transaction or delivery
authority. Its top-level repository, Issues, PRs and runs sections are independently
observed. Issues each have an independent `blocked_by` section; PRs each have an
independent `checks` section. A complete Issue list does **not** imply every
Issue's dependency list or endpoint resolution succeeded. Consumers must inspect
all statuses before calling a graph complete. Collection ordering follows the
provider response; progress labels use scope, shaped, delivered order, and
assignees retain native order.

## Identities, associations and provenance

Repository identity is its positive GitHub database ID plus canonical full name
and official URL. Issue/PR identities additionally contain a positive provider ID,
number and official URL. Never key a graph by number alone. An edge is directed
from the dependent Issue (`from`) to its native blocker (`to.identity`). Only the
selected repository's Issue nodes are collected. Cross-repository native blocker
references are resolved once through repository metadata and an exact Issue read;
they are not recursively crawled. A denied endpoint retains the original reference
and its own resolution status. Repository ID 0 means metadata is unavailable;
the Issue ID, qualified name and number still come from the native edge response.
It is a reference, not a fabricated Issue node. A contradictory resolution stays
unresolved without replacing the original edge.

Issue facts include title, open/closed state, the three native progress labels,
and assignee IDs/logins. Pull-request entries in the Issues API are excluded.
PRs and runs are repository-level activity, **not attributed to an Issue**.
Text such as “Fixes #1”, matching names, green checks and AI prose establish no
association. Runs include the API's explicit `pull_requests` references only when
the base repository and head revision match. These references need not appear in
a complete PR list if that list is independently unavailable. PR checks are
selected by exact head SHA, including fork PR heads served through the selected
repository's commit check-runs API. Checks describe that revision, not a proof of
PR delivery. A check shared by several PRs at the same SHA may appear under each.
Only PR head checks are collected; there is no claim to enumerate all checks in
all repository history. Runs retain provider ID, attempt, head SHA, native status
and nullable conclusion. PRs retain ID, state, nullable merged time and head SHA.
Checks retain provider ID, name, head SHA, status and nullable conclusion.

`status.source` is a constructed official API endpoint path, without pagination,
credentials or provider diagnostics. The status's `observed_at` is the collection
start time from the injected local clock. Each nested collection has its own
observation. Data was received between that time and `finished_at`. Official
resource links are constructed from validated identities. Arbitrary provider URLs
are not followed. Check HTML links are retained only for validated official
runs, Actions job, or PR check paths in the selected repository with matching
check ID; otherwise URL is the empty string. App `details_url` values are never
exposed.

## Availability and nullability

Every section contains `status` and `items`:

| Availability | Complete | Meaning |
| --- | --- | --- |
| known | true | The bounded collection finished successfully, possibly empty. |
| unknown | false | Facts could not be established, or collection is incomplete. |
| denied | false | An exact safe transport code established an access refusal. |

A complete empty collection has `items: []`. A section with no available facts has
`items: null`. An incomplete collection can retain already observed items, with
its incomplete status and provenance. Unknown and Denied never mean “no blockers”,
“never started”, “safe to retry” or “permitted”. Conclusions and merged times are
null when the native response has no value. Check URL may be empty. No section or
claim is omitted from JSON; consumers should ignore unknown fields for future
additive schema changes and reject unsupported schema versions.

Reasons are machine-readable: `none`, `unavailable`, `permission_denied`,
`not_found_or_concealed`, `rate_limited`, `invalid_response`, `collection_limit`,
`inconsistent_collection`, `stale_observation`, `invalid_repository`, and
`no_supported_reader`. Generic errors, offline failures and unidentified timeouts
are Unknown/unavailable. The existing transport distinguishes 403 and 404; it
cannot reliably distinguish every rate-limit or network cause. The provider
recognizes `RATE_LIMITED` only if a transport can actually identify it. It never
classifies free-form error prose or exposes raw errors.

Core completion, pause and execution are separate `Claim` values both globally
and per Issue. All default claims are Unknown/unverified/no_supported_reader,
source `none`, binding null. No supported evidence input exists in this provider;
there is deliberately no alleged-evidence ingestion or verifier callback. Hostile,
stale, denied, missing and mismatched alleged evidence in provider text is ignored.
A 403 on GitHub facts marks those facts Denied, not Core Denied: no Core read was
attempted. The optional binding type defines the repository ID, Issue ID, exact
revision and run ID that a future independently authorized supported reader would
have to establish. Its existence makes no supported reader or verified result
available. Native success and progress labels never change these Core claims.

## Freshness and finite limits

The local clock defaults to `time.Now`, and can be injected for tests. Observations
expire exactly five minutes after `started_at`. `Snapshot.At(now)` returns a copy
with Unknown/stale_observation statuses when `now` is before start or at/after
expiry, preserving facts and provenance. All nested statuses are invalidated;
Denied remains Denied with incomplete status. Collection calls `At(finished_at)`
itself. Consumers retaining snapshots must call `At` again at use time. This is
observation age, not a claim about when GitHub last changed a resource. Paging
cannot prove atomicity under concurrent mutation; duplicate IDs/numbers,
non-progressing pages, changing totals and invalid identities are rejected.

Hard defaults (configurable only downward through `Limits`):

- 20 pages per collection, 100 records per request. A full array page requires a
  following page to establish its end; count envelopes must agree across pages.
- 1,000 selected Issues-list records, including excluded PR entries. Native
  dependencies, PRs, runs and checks each have a 2,000-record bound.
- 5,000 total GET requests per snapshot, including cross-repository resolutions.
  Thus cross-repository endpoints and all nested expansion remain finite.
- Two-minute collection deadline, checked before and after every request. API
  implementations must return; no background goroutines are spawned to abandon
  blocked transports. The official transport additionally bounds each process to
  two minutes and output to 2 MiB, so a call that crosses the collection deadline
  can extend wall time by at most one transport timeout. Custom API implementations
  must supply equivalent per-call bounds. No new requests follow the deadline.

A limit, malformed page, later-page failure or changing total marks that section
incomplete. Already observed partial facts remain. Independent sections still
attempt reads within the shared budget. This provider is not a second Issue
database and supplies no persistent evidence history.

All title/login/check-name strings are untrusted inert text, including control
characters and instructions. JSON serialization escapes markup; future renderers
must apply contextual escaping and safe control-character presentation. Do not
render them as HTML or terminal commands. Provider bodies, logs, raw diagnostics,
authentication data and alleged evidence are not included.

## Reproducible evidence boundary

`testdata/native.json` is an official-API-shaped synthetic fixture. Contract tests
compare its native facts with projected values, exercise more than 100 records,
all collection pagination, failures, read-only permission, unresolved cross-repo
edges, freshness and hostile input. No fixture establishes real-provider access
or Core verification. G12/G13 can consume these types without provider credentials.
Live installed-provider comparison belongs to G16. CI and platform checks must
run on the actual candidate head; local fixture passes do not establish them.

Issue projections include an optional additive `pause_label` object with native
`present` (boolean) and `actor` (Unknown until verifiable evidence is available).
It is a label observation, independent of `core.pause`, which remains Unknown with
no_supported_reader. Missing fields in older consumers/fixtures mean unreadable,
not an absent label. Collection freshness applies to these observations too.

# Safe managed-source upgrades

Use an authenticated GitHub CLI login with actual push permission to an initialized
public Personal or Organization repository. Workflow changes may also require
workflow permission under GitHub's effective token and repository policies.
Upgrade does not change those policies or request extra grants.

```sh
gh nibfin upgrade --repo OWNER/REPO
gh nibfin upgrade --repo OWNER/REPO --version v1.2.3
gh nibfin upgrade --repo OWNER/REPO --version v1.2.3 --apply
```

Preview is the default. `--version` accepts `latest` (the default) or an exact
stable official `vMAJOR.MINOR.PATCH` release. Prereleases, drafts, arbitrary source
repositories, branches and bare commit selectors are unsupported. The example
version is illustrative; it is not a claim that this release exists.

## What is verified

The CLI uses authenticated provider reads from `taco3064/gh-nibfin`. A release and
its tag locate a commit; a release name alone never authorizes a write. Lightweight
and bounded annotated tags are resolved to a full immutable commit and read back.
The CLI reads that commit's existing `internal/cli/producers.json`. No new
compatibility catalog or Core API is required.

Both old and target tuples must refer to the official `taco3064/nibfin-core` and
`taco3064/nibfin-act` sources. Their exact commits and the Action's complete
`core-source.json` binding are verified. The artifact identity/digest, enclosed
archive/pin and manifest digest are checked in memory. The archive must contain
the same caller recipe bytes as the immutable Core source. Downloaded code is
never executed to establish preview trust. Expired or unavailable source artifacts
are unsupported rather than a reason to fall back to a tag or local cache.

Upgrade authenticates the named `nibfin-manifest.json` using its canonical JSON
content identity, including the pinned repository and commit. Whitespace, object
key ordering and a trailing newline do not change that identity. Every declared
file must match its path, mode, size and digest; missing and undeclared files are
rejected. Authenticated recipe bytes must also match the pinned immutable source.
This verification reads archive data without executing downloaded code. It does
not install Core, publish a release or certify host adoption.

The installed `.nibfin/producer-pins.json` and every managed workflow must match
bytes reconstructed from those verified old sources, including the original
`nibfin-user-*` member binding. The authenticated maintainer is not substituted
for that member. This installed pin format records Core/Action/artifact identities;
it does not record which historical CLI binary performed initialization.
The target CLI source identity identifies the producer tuple being proposed;
this command does not upgrade the locally installed extension binary.

Both the legacy wake plus separate stage layout and unified dispatch layout are
recognized. Both exact legacy and managed Issue recipes are recognized; ordinary
upgrades preserve that transport. `--managed-issue` explicitly proposes the
managed recipe only for an inactive installation without a Wake token secret,
using the same source verification and PR process. It never activates a route.
See [managed activation](managed-activation.md) for the subsequent readback and
active legacy migration boundary. Transitions add or remove the separate stage
workflow as needed.
Only a verified old managed file can be deleted. Missing pins, edited workflows,
foreign files at reserved managed paths, symlinks and executable managed files
are rejected before mutation. Unrelated tree entries remain unchanged.

## Preview and apply

The ordered JSON preview includes repository/base/tree identity, original member,
current and target immutable sources, compatibility, exact added/modified/deleted
paths and replacement bytes. It also reports host-adoption obligations separately.
Identical verified inputs yield the same plan ID and output. Preview performs no
GitHub writes, filesystem/cache installation, Runner or service operation. A
current installation returns `no_change`; apply makes no GitHub mutations for it.
Applied invocations can still reconcile obsolete CLI source-cache entries.

Explicit apply revalidates the repository, current permission, default branch,
installed content and resolved target before each mutation. It creates only
the bounded `nibfin/upgrade` branch and a proposed PR. Full resulting tree
hashes, the commit's single base parent, branch and PR repository/head/base/state/
URL/body are read back. It never updates main, force-pushes, merges, changes
protections or labels, or modifies active deliveries, adopted processes,
credentials, grants, Runner/service configuration or Pause state.

A returned PR is still a proposal. Review, current native CI, protection and merge
remain necessary. It is neither an installed upgrade nor host adoption. Core owns
separate trusted adoption and official-host coordination. This command does not
stop a host or decide whether a delivery may continue.

## Pending and unknown results

Apply failures return exit 2 with a pending result and a bounded diagnostic.
Changed repository ownership, revoked permission, changed installed managed
content or changed source intent remains pending with a specific cause. Repeat
explicit apply after resolving that cause; preview remains read-only.

Trees and commits have deterministic Git object identities, including explicit
fixed commit metadata, so lost creation responses can be read back by expected ID.
A failed, denied or incomplete observation stops further writes. No local receipt
or original write response is needed to continue.

Repeated explicit apply reads the reserved branch, all relevant PR states and
individual PR details. It corroborates CLI proposal metadata with repository and
branch identities, the commit's single parent and the complete verified tree.
Only the managed changes reconstructed from verified sources and the original
member and caller transport are permitted. Extra edits, altered modes, foreign
reserved paths, ambiguous PRs and closed unmerged proposals require inspection;
the CLI never overwrites or resets them.

New proposal commits also record the verified target release and CLI/Core/Action
source tuple. Native rebase preserves this metadata. Continuation compares it
with the freshly verified target even when two CLI commits generate identical
managed files. Legacy commits without that tuple are recoverable only when their
original plan binding can still be verified against the proposal parent or its
authoritative ancestry. This preserves that source binding after a native rebase.
Unavailable ancestry reads remain pending until observation succeeds; an obsolete
plan ID is never replaced with an assumption about today's target release.

Ordinary default-branch advancement is recoverable. For an open verified proposal,
the CLI uses GitHub's native PR-branch rebase with `REBASE` and the observed
`expectedHeadOid` guard on the same PR. It then reads back the branch, PR, ancestry,
full managed result and current authorization and source binding. Unrelated main
changes are preserved. A mutation response alone is not proof of completion.
Conflicts or competing edits stay pending with their cause; resolve conflicts
through the repository's normal process before repeating explicit apply.

After a lost rebase response, confirmed current results are reused without another
mutation. Successful reads that show unfinished work allow the next explicit
apply to re-enter the normal guarded operation. Unavailable observations remain
pending until they can be read successfully; no background polling is added.

If complete successful reads confirm that a verified unfinished branch has no PR,
explicit apply performs ordinary PR publication. It reads all relevant PR state
again after publication, including after a lost response, and reuses a uniquely
confirmed matching PR. A retained branch behind main can first be published and
then rebased through the same guarded native route. Failed or malformed listings
never establish absence or authorize a blind write replay.

Once the verified installation is current, apply returns `no_change` without
GitHub mutations even if the old branch remains. Before proposing a different upgrade,
reconcile or remove that branch through normal repository operations. The CLI
never resets it, merges a PR, or weakens protections to recover an unknown effect.

Controlled tests cover Personal/Organization permissions, source integrity,
layout changes, races, lost responses and isolation. They do not certify live
release availability, protected GitHub E2E, released-version A-to-B host adoption
or native platform support. Those remain separate native acceptance/release
checks. Unavailable live evidence must remain Unverified.

## Source-cache lifetime

Applied upgrade reconciles CLI-owned Core source operations already present in
its user cache, including when no source change is needed. It does not extract a
new Core tree, execute downloaded code or depend on a previous cache copy. Preview
remains free of cleanup mutations. Provider-confirmed branch and PR effects are
reused through the normal continuation path even if cleanup's response is lost.

Only exclusively owned operation copies with confirmed disposal eligibility can
be removed. Other operations and repositories keep their selected sources. Linux
uses the original owner process identity and recorded verifier process group;
surviving or reused groups defer removal. Unknown identity/consumer state,
retained handoffs, incomplete startup records, foreign changes and symlinks retain
the entry with a concrete diagnostic. Other platforms defer when native process
observation is unavailable. Legacy shared commit directories are preserved because
current use cannot be excluded safely. Missing housekeeping never proves disposal
eligibility and does not block an unrelated upgrade. A later applied invocation
reconciles partial removal against actual remaining filesystem state.

This concerns the CLI's user source cache only. Privileged installed Core, rollout
backups, Runner directories and Job workspaces retain their existing owners.

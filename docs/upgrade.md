# Safe managed-source upgrades

Use an authenticated GitHub CLI login with actual push permission to an initialized
public Personal or Organization repository. Workflow changes may also require
workflow permission under GitHub's effective token and repository policies.
Upgrade does not change those policies or request extra grants.

```sh
gh nibfin upgrade --repo OWNER/REPO
gh nibfin upgrade --repo OWNER/REPO --apply
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
The CLI reads that commit's existing `internal/cli/producers.json` for a release
target. Managed latest can use the verified maintained source pair described below.

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
recognized. Both exact legacy and managed Issue recipes are recognized. For an
adopted managed Linux installation, default/latest apply migrates its actual
adopted consumers to native Issue dispatch. Explicit stable versions and other
installations retain PR-only behavior. New `--managed-issue` requests report that
the artifact route is retired; historical proposal metadata remains readable.
Transitions add or remove the separate stage workflow as needed.

Managed latest uses one verified source pair across all consumers. It compares
the maintained execution repository and verified installed Core with the stable
release, preserving the newer complete compatible pair. A current source target
is identified separately from a CLI release. Diverged source pairs are reported
without deployment; an older CLI's embedded sources cannot downgrade the host.
Only a verified old managed file can be deleted. Missing pins, edited workflows,
foreign files at reserved managed paths, symlinks and executable managed files
are rejected before mutation. Unrelated tree entries remain unchanged.

## Preview and apply

The ordered JSON preview includes repository/base/tree identity, original member,
current and target immutable sources, compatibility, exact added/modified/deleted
paths and replacement bytes. It also reports host-adoption obligations separately.
Managed latest includes `managed_plans` for each repository apply would change
or complete. The original `plan` still describes the selected repository. A
same-source operation leaves unrelated consumers alone unless the execution
repository needs its first upgrade workflow. A shared Core transition plans
the full adopted set for that single source change.
Identical verified inputs yield the same plan ID and output. Preview performs no
GitHub writes, filesystem/cache installation, Runner or service operation. A
current repository returns `no_change`. Managed latest apply still completes
any unfinished configuration, adoption, cleanup and lifecycle continuation.

PR publication revalidates the repository, current permission, default branch,
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

## Managed latest handoff

Managed apply reuses the selected `gh auth login` through the shared private
credential publisher. It discovers actual adopted consumers from Core-owned host
bindings, independently of old notification configuration or journal receipts.
The adopted host must already expose Core's fixed upgrade receiver. Core CD or
the existing authorized source maintenance installs it. Apply reports
`CORE_UPGRADE_RECEIVER_REQUIRED` when that prerequisite is unfinished.

The CLI reads the selected Core's maintained notification and execution recipes.
It prepares the native route from the selected `gh auth login` and combines
required caller files and pins in one verified proposal per changed repository.
Each proposal is initially a draft. After Core records its actual native PR
identity, the CLI makes it ready for ordinary review and merge. A PR URL alone
confirms neither automatic routing nor installation. When the execution caller
is new, review and merge the execution repository proposal first.

The final output lists each read-back PR URL and shows whether its review and
merge are pending. The nested `upgrade` object reports `handoff`, `installation`,
`readiness` and `continuation` separately. When Core confirms handoff, the CLI
can close and Core continues from the native merge event. Owner pause remains
effective. An unchanged repository can complete directly without a proposal,
including when another adopted repository is still awaiting review. A separate
cleanup result reports any remaining shared-resource retirement.

GitHub suppresses `pull_request: closed` runs for merges made with a workflow's
`GITHUB_TOKEN`. A managed merger using that token must read back the successful
merge and explicitly dispatch the notification workflow on the repository's
default branch with `pull_number` and effective Actions write permission. The
same Core receiver validates both routes. See [GitHub token event behavior](https://docs.github.com/en/actions/concepts/security/github_token).

Core owns supported source maintenance, model-free Writer VM readiness when
required, and eligible Issue dispatch. An incompatible VM or host reports its
maintenance requirement and preserves confirmed progress. Retry the existing
operation when a reported observation or unfinished step needs recovery. The
CLI reuses confirmed provider effects without starting a second upgrade.
If a verified merged proposal has no active or successful native notification
or execution, an explicit retry can recover its native notification. It
reports the observed Actions run URL and preserves unknown read results.

## Pending and unknown results

Unfinished apply returns exit 2 with a pending result and a safe diagnostic.
Core administrator denial or an unsupported operation retains exit 1 and its
specific code. Optional GitHub API diagnostics include only their kind and HTTP
status, without response bodies or credential information. Management operations
respect the command's cancellation and caller deadline without an added duration.
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

New proposal commits record the verified release or maintained source target.
Native rebase preserves this metadata. A matching verified maintained pair can
reuse an original release proposal without rewriting its identity or publication.
Legacy commits without a source tuple are recoverable only when their
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
mutations even if the old branch remains. Before proposing a different upgrade,
reconcile or remove that branch through normal repository operations. The CLI
never resets it, merges a PR, or weakens protections to recover an unknown effect.

Controlled tests cover Personal/Organization permissions, source integrity,
layout changes, races, lost responses and isolation. They do not certify live
release availability, protected GitHub E2E, released-version A-to-B host adoption
or native platform support. Those remain separate native acceptance/release
checks. Unavailable live evidence must remain Unverified.

## Windows transport to an adopted WSL installation

Linux continues to run upgrade directly. On Windows, the same public command
selects an existing WSL 2 installation adopted for the requested repository and
invokes its installed Linux CLI from the guest home directory:

```sh
gh nibfin upgrade --repo OWNER/REPO --target wsl:Ubuntu
gh nibfin upgrade --repo OWNER/REPO --target wsl:Ubuntu --apply
```

A uniquely eligible running installation can be selected automatically. Apply
remembers a nonsecret repository/target/machine/architecture/pool selection hint
in the user configuration directory (`nibfin/upgrade-targets.json`). Each use
reobserves the installation; this preference grants no permission and records no
operation state. A confirmed replacement requires explicit target selection.
Preview never writes this hint or starts a stopped guest. To open an explicitly
selected stopped guest, use `--apply --consent-open-target`; opening is followed
by fresh adoption and identity checks before delegation. Multiple eligible
installations require `--target`. Docker Desktop distributions and WSL 1 are
excluded. No guest is provisioned or converted.

The guest must have an installed CLI supporting upgrade observation, its existing
Core inspection capability, and guest-local login and administration permissions.
Unavailable observation differs from confirmed non-adoption and unsupported
installed CLI capability. Missing adoption requires the normal init flow; a
running unadopted guest is never a fallback upgrade destination. The desktop
checkout, desktop tokens and Dashboard credentials are not used for mutation.
Public repository identity discovery uses a credential-free GitHub read; an
unavailable read is not proof of missing adoption.

Guest output and exit status, including pending exit 2, are relayed unchanged.
Repository-specific proposal URLs, review/merge pending, Actions references,
installation/readiness failures, Owner pauses and continuation remain guest
results. Transport does not certify VM readiness or manufacture native handoff.
Only actual guest confirmation establishes that closing the CLI leaves the
original operation continuing after merge. Explicit stable releases retain their
PR-only behavior; Windows creates no additional proposal or lifecycle ledger.

For cancellation, launch failure or lost transport before confirmed handoff,
use the emitted `resume_argv` with the same repository, target, version and
preview/apply intent. The guest operation owns provider read-back, reuse of
confirmed proposals and guarded recovery when main advances. A missing target
preference does not prove external work is absent. Never infer automatic
completion from process launch, a PR URL or transport exit alone. Windows/WSL
E2E and VM/platform certification require their own native evidence.

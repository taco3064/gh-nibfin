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

The installed `.nibfin/producer-pins.json` and every managed workflow must match
bytes reconstructed from those verified old sources, including the original
`nibfin-user-*` member binding. The authenticated maintainer is not substituted
for that member. This installed pin format records Core/Action/artifact identities;
it does not record which historical CLI binary performed initialization.
The target CLI source identity identifies the producer tuple being proposed;
this command does not upgrade the locally installed extension binary.

Both the legacy wake plus separate stage layout and unified dispatch layout are
recognized. Transitions add or remove the separate stage workflow as needed.
Only a verified old managed file can be deleted. Missing pins, edited workflows,
foreign files at reserved managed paths, symlinks and executable managed files
are rejected before mutation. Unrelated tree entries remain unchanged.

## Preview and apply

The ordered JSON preview includes repository/base/tree identity, original member,
current and target immutable sources, compatibility, exact added/modified/deleted
paths and replacement bytes. It also reports host-adoption obligations separately.
Identical verified inputs yield the same plan ID and output. Preview performs no
GitHub writes, filesystem/cache installation, Runner or service operation. A
current installation returns `no_change`; even apply then makes no mutations.

Explicit apply revalidates the repository, current permission, default branch,
base, installed content and resolved target before each mutation. It creates only
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
A changed repository, permission, base, installed content or source resolution
requires a fresh preview. If main advances after branch creation, the existing
branch is retained for inspection; the command reports the base change and does
not claim an installed upgrade or silently retarget the branch.

Trees and commits have deterministic Git object identities, including explicit
fixed commit metadata, so lost responses can be read back by the expected ID.
Unknown read-back stops further mutations. Existing branches are reused only
when their exact immutable commit matches the plan. A single reserved branch also
prevents a changed base or target from replaying an unresolved earlier proposal.
After merging, reconcile or remove the old branch through normal repository
operations before proposing another upgrade; the CLI never resets it. Matching
open PRs are read back rather than recreated. Conflicting, multiple, closed or foreign PRs stop.

An existing branch with no visible PR remains pending: a previous PR response
might have been lost. Repeating apply does not authorize another PR creation.
Inspect the named branch and all PR states through GitHub, establish the actual
effect, and reconcile under your repository's normal review process. The CLI does
not delete branches or reset unknown effects for you. Never resolve an unknown
result by force-pushing or weakening protection.

Controlled tests cover Personal/Organization permissions, source integrity,
layout changes, races, lost responses and isolation. They do not certify live
release availability, protected GitHub E2E, released-version A-to-B host adoption
or native platform support. Those remain separate native acceptance/release
checks. Unavailable live evidence must remain Unverified.

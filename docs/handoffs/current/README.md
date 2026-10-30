# Current reviewed public runtime

This integration branch selects the reviewed public Core candidate source snapshot at
`aee2bb305be1e384399a06682e2bd54d135f2f24`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | f761964125844ae8dbdda09eda3645d73d6d0ccbb1a4bd3f35a9f14612417eb6 |
| release-manifest.json | fa68815e807232ccb48aadbca429f91755c4b1d176fd2c212c0d18afd2166790 |
| compatibility.json | d7ea5f0326a04564aecd399f01d997c1bafe7912cd34f927e85cb4949c005276 |

The Core component identity is
`2e7a65bddfcf8019e3c839c463033152656cff493dec881878ce3ce17a417609`.
The release manifest honestly retains the producer's candidate source identity;
these files do not establish publication of a release. The superseded #233 input and incompatibility report remain available in Git history; they are not installation inputs.
N01/N15/N17 remain separately pinned historical contracts.

The loader verifies all three anchors, full component inventory, package metadata,
locked JOSE identity, installed bytes and public compatibility before invoking a
consumer. It installs the two exact local archives offline with lifecycle scripts
disabled. Runtime factories remain inside the installation callback so their lazy
source reads cannot outlive the verified package installation.

The admission process fixture uses the actual installed shell and Node Hook and
disposable Worker doubles, with authenticated provider transports replaced. It
covers Repository and explicitly scoped Organization registrations, legacy scope
rejection, wrong actors, denied reads and the shell fallback after Node startup
failure. Termination must suppress an always-style continuation sentinel. This is
local process proof, not official Runner binary or platform certification.

The current installation fixture uses the installed Core installation API and
real CLI adapter with a simulated GitHub provider and notifier transport. It covers
read-only checks, first/repeat setup, explicit opt-in, modified files, denied
permission and unconfirmed writes. It does not certify live provider/service setup.

Run installed consumers through the guarded verification lane:

```sh
node /verification-runner.mjs -- node --test test/current/*.mjs
```

Issue #55 remains incomplete: complete native-host onboarding, automatic staging,
actual coordinated Runner upgrade/rollback and retained lifecycle integration
still require proof. The installed maintenance command and preserved-work tests
do not certify those missing integrations. No release, production cutover, live
model/provider use or Linux/WSL certification is claimed.

Requirement sources use the common `observeRequirementAuthority` API. The installed
`github-issue` adapter owns GitHub identity and API handling; Jira and Linear are
separate future adapters and currently fail before I/O. The installation chooses
the source kind, target and connection. Requirement text cannot switch providers
or grant execution privileges. Source changes do not transfer existing pending
work or Grants to another authority.

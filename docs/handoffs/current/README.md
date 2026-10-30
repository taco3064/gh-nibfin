# Current reviewed public runtime

This integration branch selects the reviewed public Core candidate source snapshot at
`34ee0e28ceaf86f2b966fac2b190902d9aad92be`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | 1710e6161b496b017102f25e3e47c5fb6321d0aa2a61d668331a6fd927288db0 |
| release-manifest.json | f7d0c8eaeedd04654a84ca18cd01bb7f58df593dec117f79976f3c5142a151ff |
| compatibility.json | 515a9c292bf4961d5083fb4bbf136679c22415c807c9bd6078ad0ff8fc652aa2 |

The Core component identity is
`3332f9147fce80ed62435867e5b5fda2e9966af1eff5422048742ff170c9db24`.
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

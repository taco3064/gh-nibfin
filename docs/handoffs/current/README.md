# Current reviewed public runtime

This integration branch selects the reviewed public Core candidate source snapshot at
`1c9d8b9f08331eb4088c6dd304e24372ab5b0e1a`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | 3602682b755294ea0bb82ecc1bad824ff9701fa36240ba742af5d69555d991b5 |
| release-manifest.json | 91e93f3627137a12fbd76cb0da6c262e36b9b5de03c50a2d308a786f8ba4e9e7 |
| compatibility.json | 7cc4b6b98db1254824a3b0cf11d34853b452789fdf9b4d0c004df9009bc4a7cc |

The Core component identity is
`4a4887201334c42422f2d3b836f307a7c6ca71ac77168c92636aca4e3b967163`.
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

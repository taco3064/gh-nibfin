# Current reviewed public runtime

This integration branch selects the reviewed public Core candidate source snapshot at
`5f10dab08d320d67f691d71b79df6bdc0782138b`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | 7352f24fb878542024f6e77e60f457f03ad0fb6708ddcc71f115cf042ac837c9 |
| release-manifest.json | 12f81c2c3fb56176f0d5689310474d34b36eeae04c4357bd9d7aeb0991a4b936 |
| compatibility.json | 225942f76cb42b85578cc7a4fe828f45945552cb28398f1b79d5fdb184f34299 |

The Core component identity is
`3e553f1557e4e56d0cc88e62d4dce53ba0dc81a9157aeeee810496be3d591736`.
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

# Current reviewed public runtime

This integration branch selects the reviewed public Core candidate source snapshot at
`c0ce0b7cc4163266c6ce040c725e7d3bf5ee9901`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | 74aada6e8b9d5d0a2a2e47a053e88701d3f7879eb23752ea4a1e578bc85f046c |
| release-manifest.json | 5e9631ba17eed7f492a959821a10c987d9fd08d2ed890f8a9b9b107cf9d54a7e |
| compatibility.json | be004f5810b31580563ca387f4262107b97b3683a8c5b495fbfe87e186eab837 |

The Core component identity is
`d3f159006cf09273dba4fee0dfdf270f19c446b801f7b2a8670d059d19fbacdc`.
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

# Current reviewed public runtime

The production loader selects the Nibfin PR252 source snapshot at
`58636bf124cc0363d61ba0155888232e2f1cb602`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | 825c5988d6bc56da04934d9086d4ccb483c13167dd6c8bae60647dc4c88db3f9 |
| release-manifest.json | 14f74e749963d70f1e64c79408fab6a0629ed1adb310bfb9b52210beeb5376e3 |
| compatibility.json | c7e9e57d6025c64b216688d095f47e7d3d2988d1a118ba27d5e5cb0ba0cf3d55 |

The Core component identity is
`6fdba0f33e3c1abac0be6999593d951b149d3a7b3f50c1fdd0f4f7f51cc13004`.
The release manifest honestly retains the producer's candidate source identity;
these files do not establish publication of a release. The older #233 input and
its historical incompatibility report remain under [n233](../n233/README.md).
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

Issue #55 remains incomplete: upgrade/rollback, complete recovery-host onboarding,
retained lifecycle fixture adaptation and fresh matching non-root regression proof
are still required. No release, production cutover, live model/provider use or
Linux/WSL certification is claimed.

Requirement sources use the common `observeRequirementAuthority` API. The installed
`github-issue` adapter owns GitHub identity and API handling; Jira and Linear are
separate future adapters and currently fail before I/O. The installation chooses
the source kind, target and connection. Requirement text cannot switch providers
or grant execution privileges. Source changes do not transfer existing pending
work or Grants to another authority.

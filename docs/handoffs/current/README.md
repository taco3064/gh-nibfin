# Current reviewed public runtime

This integration branch selects the Nibfin PR255 candidate source snapshot at
`276d9c900a4659cc307a7d200fc997fef890a586`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | d789723b8833b303b26bdff77fc40d7ad456e6bced8c367bd92b916a6d539b55 |
| release-manifest.json | 13ec57b9019f188435d8ef43501ae8220a2d9578dccfa339ca10fc4b8dbc3b08 |
| compatibility.json | 3701c0b5921f35fc124c3acaa212aaf043d14985e9a836dffa3af427eda9804f |

The Core component identity is
`51036e8855c1856662cb787504a94a922b87e13ba58465d13b7c78bde5b6797f`.
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

# Current reviewed public runtime

The production loader selects Nibfin PR246, merged at
`9504d15d65476725b01b32df1bd7f93b8de85627`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | 13d65ffd998559ad19d3542df583cc28224a6f4fee76ea739e5722df7fbf302b |
| release-manifest.json | c5313f5fde12a20c8521b04f7d5538beb6b484aedcda261b4fcd91cbf3101ec5 |
| compatibility.json | fa96fc97c3a6df7690af698d40457138b76cd15de81d55ea417dba898ef2c7e5 |

The Core component identity is
`2536e2e5435f639be669bfd23f4b0467d8f41ee1690c20a7a5250f0e91e559ec`.
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

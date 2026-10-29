# Current reviewed public runtime

The production loader selects the Nibfin PR251 source snapshot at
`8b4e71cb627c85710f569f0483b1b6bdd1bb7097`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | 10414d2dd87c60f8f097d8ff73ee083424d5ac3fe36e216fae857a11161e8759 |
| release-manifest.json | 187c0a263079b9a26a63b6279a8ea4b4b5b8deacfa7093d98b0902cbccc6159e |
| compatibility.json | 3947b6bab83fc4b1a0c27d08321403eb5efb419fee9c20debf895532d23be44d |

The Core component identity is
`3ecd0bf634acbe2bd598fd33ba949cd67625ae75d39521f6b794f51cc2374145`.
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

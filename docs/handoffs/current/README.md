# Current reviewed public runtime

The production loader selects the Nibfin PR251 source snapshot at
`f411c96bb2336ae271c8ebaa1c5bd17b1bb8f252`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | fe060f98d4095c851901d20acaef7401bf9dc8915763396808391682c32e1da7 |
| release-manifest.json | 30c242c1812889716b344e819ddf90bf491783488cf23192954aa919d15417e4 |
| compatibility.json | 32d7d6b4290d7b2b25f8df6e8bb199c7d8a90c0783bcf184a69288cbd5fdf166 |

The Core component identity is
`0699738e8ecf66ff5b816e0be7d7a8ff82a811b343acf03a5d3ea58d5ca8c889`.
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

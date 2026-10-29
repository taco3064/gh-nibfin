# Current reviewed public runtime

The production loader selects the Nibfin PR250 source snapshot at
`c478de50d02ad730d3fe88cd2c5303a462875cbe`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | 67582857bcaf194b170c0fab5df29b94f9221ccf1cdbf196cf89826e84bfcbf3 |
| release-manifest.json | a88e98cc757a9cf13d5cc0ed0d656c46542342e8c8ef7b1133f477a399065922 |
| compatibility.json | e822b87fc58a2aa8020cd15032bc4d39d9f893fa62d8ac02d93f3f9311b99e3e |

The Core component identity is
`60676d29c29fbee5e147b3ac297b9402f0d4780fb9f5ad936afba64e9d4a01fd`.
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

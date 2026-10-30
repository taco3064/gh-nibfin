# Current reviewed public runtime

This integration branch selects the Nibfin PR255 candidate source snapshot at
`a5597ff7d6ba3336db7a7358b0685adc46b2f404`, package `@nibfin/core@0.1.0`.
The exact reviewed inputs are retained here:

| Input | SHA-256 |
| --- | --- |
| nibfin-core.tgz | 970c83b30e5be2e84d016fea89598e7e15b97ef0bc81b04b69da309133d6db34 |
| release-manifest.json | a539385562312c5582a026983605b4b154b9e1a002a3edf2c69a3a9985c10c91 |
| compatibility.json | 29d4a50cf8ab8cb6e10f481c20731b5e36d0b17955a56dc5e88954a4ed557744 |

The Core component identity is
`db7c42fe96366c1c476ffdc9fd39c956a682e8dde22a11728481bd11b58c0acc`.
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

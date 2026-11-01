# Current reviewed public runtime

The CLI selects merged public Core `@nibfin/core@0.1.5` from
`5c9ae0833acf5fd0d29fec6618f4a3623169123e`. Its component identity is
`9df89deccde8ee3e14f841b37e2167f97da9239cfef576d6aa51e10f1e26e221`.

The exact archive, manifest and compatibility inputs are retained here. The
authoritative asset hashes are in `src/current-core.mjs`; the loader checks every
asset and full file inventory before executing Core. These immutable release
inputs were independently read back from the official v0.1.5 release.
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

The retained Shape fixture verifies explicit historical provenance under a new
Owner Grant, then resumes the original Writer at Deliver. It never resets Issue
labels or turns old Shape evidence into a new Run. Terminal history is verified
read-only after activation changes and Grant expiry. A completed-work upgrade
fixture preserves the original cycle bytes through the sealed filesystem switch;
its provider, service and resource observations are explicit doubles. These tests
do not establish Issue #55 delivery, independent Acceptance or live upgrade proof.

Requirement sources use the common `observeRequirementAuthority` API. The installed
`github-issue` adapter owns GitHub identity and API handling; Jira and Linear are
separate future adapters and currently fail before I/O. The installation chooses
the source kind, target and connection. Requirement text cannot switch providers
or grant execution privileges. Source changes do not transfer existing pending
work or Grants to another authority.

# Current reviewed public runtime

The CLI selects merged public Core `@nibfin/core@0.1.6` from
`efb31d8bd3118b3050bba862440624861a4e16ce`. Its component identity is
`794911c77cc23c3bbb645dcc05d7076dce592421fe7988efda02e18bc6458585`.

The exact archive, manifest and compatibility inputs are retained here. The
authoritative asset hashes are in `src/current-core.mjs`; the loader checks every
asset and full file inventory before executing Core. These immutable release
inputs were independently read back from the official v0.1.6 release.
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

The native dependency fixture selects Core's explicit GitHub-label progress mode.
It keeps source authorization and bounded native graph traversal, without asking
this machine to recreate historical completion receipts for delivered Issues.
The reapproval fixture retains the old signed state, cycle, session and repair
count through an engine-binding change. A sealed filesystem-switch fixture also
preserves the untouched prefix before current Owner reapproval. Alternate engine
bindings and provider/service/resource observations are explicit test doubles;
the original deployment and actual resumed delivery require separate readback.

Requirement sources use the common `observeRequirementAuthority` API. The installed
`github-issue` adapter owns GitHub identity and API handling; Jira and Linear are
separate future adapters and currently fail before I/O. The installation chooses
the source kind, target and connection. Requirement text cannot switch providers
or grant execution privileges. Source changes do not transfer existing pending
work or Grants to another authority.

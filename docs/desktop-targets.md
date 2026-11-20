# Desktop installation through an existing Linux guest

Native Windows and macOS executable names identify the installer, not the shared
execution backend. Shared execution still runs in a native Ubuntu 24.04 Linux
environment. Apple Silicon requires a native Linux arm64 guest. Discovery reports
observations and gaps; it does not certify isolation or protected execution.
Native Windows/macOS runtime support and lifecycle E2E remain unverified until
checked in those environments. Direct Linux installation remains available.

Preview before opening anything:

```sh
gh nibfin init --repo OWNER/REPO
gh nibfin preflight --repo OWNER/REPO
```

On Windows the CLI uses official `wsl.exe` inventory, excludes Docker Desktop's
implementation distributions and diagnoses WSL 1 explicitly. It decodes UTF-16
inventory independently of localized headings and uses a separate running list.
On Apple Silicon it queries the installed Canonical Multipass CLI for existing
Ubuntu guests. Preview never starts stopped guests. Stopped and coarsely inspected
guests remain unverified; a native CLI observation is required before delegation.

An eligible adopted guest is preferred. A sole eligible target is shown with its
plan; several targets require selection. A sole stopped target may be proposed
for opening, but is never described as validated. To choose explicitly and consent
to opening an existing guest in unattended use:

```sh
gh nibfin init --repo OWNER/REPO --apply \
  --target wsl:Ubuntu-24.04 --consent-open-target \
  --consent-experimental-sparse
# Apple Silicon:
gh nibfin init --repo OWNER/REPO --apply \
  --target multipass:nibfin --consent-open-target
```

Interactive use asks for selection and opening consent when needed. Without a
terminal, explicit opening consent is required. Opening consent does not replace
guest-local managed installation consent, selected GitHub account verification or
Core authorization. The desktop CLI does not plan an installation PR itself: it
hands the requested init to the selected Linux CLI. Linux path options such as
`--runner-directory`, `--directory` and `--handoff-directory` must be used directly
inside the guest; desktop paths are never silently translated. Quick registration
also stays inside Linux. Guest machine identity and architecture are rechecked
before handoff and again in the Linux entry before setup or PR mutation. If an
observation is unavailable or changes, setup stops.

## Missing prerequisites

These are operator instructions; the CLI does not install distributions, convert
WSL versions, configure kernels, provision VMs or install a hypervisor.

Windows: follow [Microsoft's official WSL commands][wsl] and run:

```powershell
wsl --install -d Ubuntu-24.04
wsl.exe --distribution Ubuntu-24.04 --cd ~
```

WSL 1 requires an operator-managed WSL 2 environment. Running state is only a
snapshot: [systemd alone does not keep WSL alive][systemd].

Apple Silicon: install [Canonical's official Multipass macOS package][multipass],
then, if you need a new environment, run yourself:

```sh
multipass launch 24.04 --name nibfin
multipass shell nibfin
```

You may instead run the Linux CLI directly in an equivalent native Ubuntu 24.04
arm64 VM without Multipass discovery. macOS sleep or guest shutdown interrupts
execution.

Inside the guest, use the matching locally prepared executable:
`gh-nibfin_linux_amd64` for native amd64, or `gh-nibfin_linux_arm64` for native arm64.
Apple Silicon must use the latter. This does not promise a published download.
Place the matching executable as `gh-nibfin` inside a separate `gh-nibfin` directory,
then run there:

```sh
chmod +x gh-nibfin
gh extension install .
```

If tools or authentication are missing, install the official guest-side GitHub CLI
and Codex CLI, then use their official login flows inside the guest:

```sh
gh auth login
codex login
gh nibfin preflight --repo OWNER/REPO
gh nibfin init --repo OWNER/REPO --apply
```

Credentials remain in the guest. Desktop tokens, login directories and desktop
homes are not copied or mounted by this handoff. Existing guest accounts and
managed-login checks continue to govern installation.

## Disconnect and recovery

A desktop disconnect is not completion. Re-enter the **same guest** using the
entry command shown in the preview, then run the shown guest-side preflight and
init commands with the original installation options. Existing durable intent and
relay read-back reconcile pending work. Unknown mutation effects remain pending;
do not replay an unknown mutation, start a second Writer or move to another guest
to bypass recovery. Core owns isolation, Runner adoption and recovery decisions.

[wsl]: https://learn.microsoft.com/en-us/windows/wsl/basic-commands
[systemd]: https://learn.microsoft.com/en-us/windows/wsl/systemd
[multipass]: https://canonical.com/multipass/docs/latest/how-to-guides/install-multipass/

## Read-only Windows Dashboard overview

Overview observation is separate from the installation handoff described above.
`gh nibfin dashboard --overview [--target wsl:NAME]` uses the **Windows** gh user
and keeps its service/browser on Windows. It neither starts stopped distributions
nor provisions, upgrades or enrolls guests. Only a running WSL 2 guest with a
verified installed CLI bridge and an existing adopted Core for that same member
is eligible. Runner online status and installation preflight tools are not gates.

A single verified target is unambiguous. Several require explicit selection; an
invalid explicit target cannot redirect to another guest. Missing or outdated
bridges, unreadable installations and wrong-member targets produce unavailable
states, never complete empty inventories. Session refresh revalidates the bound
machine, architecture and executable and requires deliberate restart/reselection
after a confirmed change. A failed verification read can retry on the same
bound guest without sending credentials to an unverified target. Discovery and
private reads follow the Dashboard lifetime and
propagate cancellation without a fixed deadline. Discovery stops once two verified
targets establish ambiguity; inventory size alone does not require selection.
Inventory and private observation output retain their separate size bounds.

Unlike init handoff, this fixed read forwards the Windows token on anonymous
stdin **only after** credential-free filesystem and adopted-Core verification.
It never forwards environment credentials or uses the guest's gh login. The
installed user-owned executable must be outside Git repositories and temporary
directories with safe ownership and permissions throughout its canonical ancestry.
Local development extension links into a checkout are therefore unsuitable for
private observation. Core/Node still require their existing root-owned manifest
and architecture verification. Updating an incompatible guest CLI is an operator
action. See [the Dashboard boundary](dashboard.md#windows-overview-through-an-existing-wsl-2-installation)
for bootstrap locations, transport limits and platform evidence limitations.

## Experimental Windows sparse reclamation

Windows selected-target init normally proposes `--reclamation enable`. Preview and
preflight inspect the selected existing WSL 2 registration, backing-file sparse
metadata and native command capability without opening a stopped guest. Unknown or
ambiguous metadata stays unknown; it is never treated as disabled or enabled.

Opening consent is separate from experimental sparse conversion consent. Interactive
apply asks for that choice. Unattended apply must supply either
`--consent-experimental-sparse` or `--reclamation unchanged`. Host-only options are
not forwarded to Linux. The shown Windows resume command preserves the choice.

After fresh identity and stopped-state checks, consented supported setup invokes
`wsl.exe --manage NAME --set-sparse true`, without unsafe forcing flags. It reads
back the same registration and backing file before reporting enabled. Already
enabled state is reused, including after a lost command response. Replaced targets
invalidate the binding. Missing observations do not authorize a conversion retry.

Running guests are deferred; the CLI never stops or terminates them. Resume the
same Windows selected-target init after the guest naturally stops. Unsupported,
permission-denied, failed operation and unknown read-back results are reported
separately. These reclamation gaps do not prevent an otherwise valid installation.
Continue guest-only setup or choose unchanged, and retry normal Windows setup when
appropriate. Do not interrupt admitted Jobs to reclaim space.

Direct Linux init/preflight detects WSL from kernel observations, even with Windows
interop disabled. It cannot configure the Windows host setting and never invokes
Windows executables. Use the Windows selected-target route for host configuration;
guest installation remains usable. Linux reports root mount filesystem, backing
block-device discard capability and `fstrim` tool availability as separate,
read-only facts. Unknown observations are informational, not installation gates.

Sparse configuration, Linux discard support and measured physical Windows disk
recovery are different facts. Neither fewer Linux files nor `fstrim` output proves
physical recovery. This flow does not run discard, schedule cleanup, alter global
`.wslconfig`, create distributions or broaden interop. Native Windows operation and
physical recovery still require their corresponding platform evidence.

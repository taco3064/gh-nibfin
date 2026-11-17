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
  --target wsl:Ubuntu-24.04 --consent-open-target
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

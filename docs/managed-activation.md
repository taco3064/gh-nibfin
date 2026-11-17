# Managed Issue wake activation on Linux

Plain `gh nibfin init --repo OWNER/REPO` previews the managed caller on a
compatible Linux x64 host. `--apply` uses the installing user's existing GitHub
CLI login after the existing consent step (`--consent-managed-login` supplies
explicit consent). The installation PR still requires native review and merge.
No repository token secret or test Issue is created.

After verifying the merged managed caller, init adopts the repository, starts
or reads back the two shared Runner slots, verifies the execution destination,
and configures Core's installed notification relay. A peer repository retains
its own adoption and joins the same relay and Runner pool. Init does not
register additional slots or install another service for a peer.

The managed caller's `mode: managed` selects Core's notification behavior.
`NIBFIN_SHARED_RUNNER_POOL=true` separately suppresses local reusable-workflow
dispatch. Init establishes that variable only after relay preparation and reads
it back before enabling the timer and explicitly resuming the relay. Lost
variable responses are reconciled through GET before another mutation.

`MANAGED_ROUTING_CONFIGURED` means the installed bindings, destination, Runner
identities, suppression variables, timer and control state were read back.
The result includes `relay.dispatch_observed` for the target repository,
with host-wide consumed receipt and unknown-effect counts separately. A configured route need not have received a
notification yet. Consumed receipts prove dispatch readback, not completion of
an Issue or availability of a Runner for a later Job.

## Existing installations

Ordinary upgrade preserves the installed Issue transport, including managed
callers. Init refuses edited or legacy callers instead of overwriting them.
For a verified **inactive** legacy installation, preview an explicit proposal:

```sh
gh nibfin upgrade --repo OWNER/REPO --managed-issue
gh nibfin upgrade --repo OWNER/REPO --managed-issue --apply
```

This uses the existing verified official release/source and PR process. If a
compatible released source is unavailable, the proposal remains unavailable.
After review and merge, rerun managed init. Core refreshes only the Issue caller
binding and independently preserves the original Action workflow and Runner
identities. The original CLI-1 installation intent remains available.

An active legacy route or existing Wake token secret requires explicit local
migration and reconciliation. Neither ordinary init nor the proposal command
disables that route, removes its secret, or migrates outstanding effects.
There is no automatic active-route migration command.

## Pending results and inspection

Pending codes distinguish missing compatible sources, changed bindings,
credentials/readiness that cannot be verified, local configuration conflicts,
paused state and uncertain writes. No configured result is emitted for these
states. Existing compatible origins and all Core recovery receipts are retained.

A local administrator can use the installed pinned Node executable with
`/opt/nibfin/core/bin/nibfin-notification-relay.mjs inspect`. The same fixed entry
supports `pause` and `resume`; Core owns their admission and recovery semantics.
A busy pause is not a completed pause. Never edit or remove relay control,
journal or unknown-effect receipts to retry a dispatch. Credential inspection
and reauthorization remain in Core's existing `managed-credentials` interface;
repeating init on an adopted host does not replace those credentials.

Configuration reconciliation uses a root-owned lock directory at
`/etc/nibfin/notification-relay.cli-lock` and an atomic private configuration at
`/etc/nibfin/notification-relay.json`. An interrupted writer can leave the lock
for administrator inspection. Do not remove it while another init is running.
An existing paused relay is never silently resumed by init. Inspect the relay
and reconcile any uncertain effects through Core's supported administration
before explicitly resuming and rerunning init. A failed first activation can
require this inspection even when the variable write succeeded.

Linux/systemd and the installed compatible Core remain prerequisites. These
local and simulated checks do not certify a live installation, provider
notification delivery or a release. Those checks belong to independent
Acceptance and the later native installation/E2E stages.

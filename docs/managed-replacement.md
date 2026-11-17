# Managed replacement recovery

Use `gh nibfin runner rebuild --repo OWNER/REPO --agent codex` to preview.
Applying requires `--apply --consent-rebuild`, the installing user's stored
GitHub login, repository administration rights for all adopted repositories,
and independent local administrator access. Run inside the managed Linux host.
See the README for provider support limits: production Claude composition is
unavailable; positive Claude orchestration tests use private controlled fixtures.

The CLI owns durable orchestration intent and official GitHub registration
operations. Core owns provider validation, adoption validation, settlement,
resource cleanup, admission and final activation. Rebuild never edits Core
retirement or drain files and never changes shared caller recipes or pins.

| Interruption | Recovery on the same apply command |
| --- | --- |
| Before settlement | Request fresh Core retirement readback; no removal on unknown settlement. |
| During old removal | Read each old ID and the full listing; request fresh Core permission before another removal. |
| During cleanup | Advance Core until it reports `retired`; no fresh registration before that. |
| During registration | Reconcile native identity and private staged registration records; do not duplicate uncertain effects. |
| During primary adoption | Match Core replacement identity against the saved fresh IDs. |
| During peer adoption | Re-adopt missing historical peers using Core's maintained operation. |
| During service or wake setup | Reuse verified installer and routing readback; admission remains held. |
| During activation | Repeat `activate-shared-replacement` through rebuild; local phases and online listeners do not establish release. |

A pending result is incomplete. Preserve private intent and resolve the reported
prerequisite; do not unregister unrelated Runners, revoke original logins, remove
Core records or infer safety from offline status, cancellation or elapsed time.
Native authorization failures, incomplete listings and ambiguous responses cannot
prove absence. Controlled fixtures establish CLI behavior only; live host/provider
integration, independent Acceptance and PR-head CI have separate evidence owners.

# Native ephemeral Runner runtime

The maintained host composes `nativePoolRuntime` with Core's retained JIT owner
and finite pool allocator. Its production defaults use the pinned official Runner,
the execution account's stored `gh` login, private local files, and user systemd.
These are internal maintained modules. The public installation does not accept
JavaScript bindings, module paths or executable callbacks from a customer, Issue,
model answer or workflow input.

Each leased slot prepares a fresh application from the verified official archive.
Before launch, the JIT response must contain exactly `.runner`, `.credentials`
and `.credentials_rsaparams`; unexpected paths, another Runner identity, mutable
updates, non-ephemeral settings and unexpected servers are rejected. The exact
decoded files are written privately with mode 0600, matching the official
[Listener's JIT materialization](https://github.com/actions/runner/blob/d1eb12d4b115c301201de27d667284e1a80f2ab5/src/Runner.Listener/Runner.cs).
The configured Listener receives neither JIT credentials in argv nor in its
environment. The Runner's GitHub URL may be absent in an official JIT response;
repository identity is also bound by the scoped API request, protected Job,
Runner ID and installed admission policy.

The fixed per-slot service starts on demand with `Restart=no`. No persistent
listener is enabled. The actual service PID, invocation and control group must
agree with the private single-use launch record. Lost replies do not authorize
another start. Worker admission is installed before launch. Listener output stays
in a private log; public Action diagnosis uses the separate Core reporting path.

A terminated Listener is not proof that its GitHub Job succeeded or that its model
containers stopped. The runtime independently reads the exact completed Job and
checks the original process identity and empty service control group. Original
work must be retained by the maintained Core custody owner after model processes
terminate. Failed Job conclusions remain failed in the retained outcome.

Cleanup requires fresh original-work retention and model-resource observations,
remote deregistration, and retirement of the exact service and admission policy.
Open files, visible process references, mapped files, visible mounts, hard links,
changed trees or unknown managed-resource observations retain the slot. If another
process's private environment is inaccessible, its independently readable cgroup
must be outside the authenticated retired service scope; an unrelated protected
host service is not treated as an active Runner. Model-resource observations remain
mandatory. Disposable application files are
atomically parked before removal. A retained inventory permits recovery after
partial removal only when every remaining entry is unchanged. Completed cleanup
keeps a compact tombstone; it cannot remove replacement work at the old path.

## Current integration boundary

The maintained control process now composes authenticated queued-Job intake,
finite ephemeral Runner coordination, stage execution and Core delivery owners.
The source-only pilot retains original work/custody and distinct Shape validation,
Deliver, both isolated hook gates, independent pre-commit Acceptance, protected
publication, exact-head CI, fresh final review, protected merge, post-merge checks
and causal completion/readback boundaries. No stage's liveness substitutes for
another stage's evidence. Missing configuration or unresolved work remains held.

The default `nativeDispatchMonitor` remotely observes every **30 seconds**; local
stage completion, slot release and activation-file events can wake it immediately.
The private wakeup inbox also uses file subscriptions for retained completion
notifications. This is a local monitor, not cron or idle scheduled Actions. It
neither guarantees immediate remote observation nor instant revocation. Each
project uses its own workflows and Actions logs; Core's `actionRunName` includes
Issue number, stage and correlation. Physical Runner capacity is finite and shared
across the selected projects, not one Runner per project. This does not promise
custom GitHub queue semantics, atomic routing or a distributed lease.

`test/current/native-complete-delivery-consumer.mjs` doubles external model and
GitHub observations: it and other retained consumers are source/integration
contract evidence only, not live delivery certification. Native AI Rescue now
has maintained diagnostic/original-role-continuation adapters, but those remain
separate from source-only delivery and do not supply Acceptance or product-stage
execution authority; their presence is not a completed recovery rollout proof.
Legacy-work adoption, additional-artifact support and deployment obligations are
not established by this pilot. Unsupported or unresolved work stays held for
reconciliation, never silently adopted or discarded. Source-only completion
requires explicit Scope applicability for no additional artifacts and no deployment;
it cannot waive obligations for other Issues. #55 and its acceptance status remain
unchanged; this documentation does not depend on accepting or replacing it.

No release publication, actual package installation, live model/provider
availability, browser/platform certification or completed rollout is claimed.

## Runtime and network readiness

Public init and resume details are in [the onboarding guide](onboarding-integration.md#owner-and-member-contexts).
Preparation requires Linux x64, host Node 22, a non-root execution account with
user-systemd, rootless Docker, verified official Runner distribution, reviewed
immutable image/profile, required tools and the complete runtime/isolation/network
readiness probes. These are host prerequisites, not capabilities established by
the UID or tools of an isolated verification container. The execution account
uses its stored GitHub authentication and
the maintained native member login; management credentials remain outside model
workspaces. Missing or unresolved authentication/readiness is not a successful login.

The packaged producer's explicit `linux-amd64-codex-node22-docker29-v1` profile
requires Docker client **and** server **29.9.1**, Codex **codex-cli 0.163.0**, Node
**v22.23.3**, Debian 12 guest, Linux x64 and the `node22-bookworm-v1` image layout.
It is an exact supported tuple, not a minimum-version rule. A newer Docker install
or a syntactically accepted digest does not establish compatibility. Compatible
resume preserves the protected image/profile; it does not infer a different one.

| Network boundary | Source-backed destinations |
| --- | --- |
| Management GitHub observations/operations | `api.github.com`, `github.com` |
| Official Runner acquisition (including allowed redirects) | `github.com`, `release-assets.githubusercontent.com`, `objects.githubusercontent.com` |
| Isolated Codex model egress | `chatgpt.com`, `auth.openai.com` |
| Isolated dependency probes | `registry.npmjs.org`, `cdn.playwright.dev`, `playwright.download.prss.microsoft.com` |

Management/acquisition networking is separate from isolated model/dependency
egress. These destinations come from maintained provider/acquisition code and the
packaged `codex.mjs`, `egress.mjs` and `agent-session.mjs`; they do not cover every
provider. Listing a domain proves no availability: actual required probes must
pass in the selected runtime. No environment bypass, new credential, executable
binding or additional setup command is implied.

The existing `init --handover` boundary can also prepare a historical unaccepted
Writer when the selected Core provides the explicit unaccepted-handover contract.
It reconstructs the original Git index/tree and conversation in disposable proof
directories without executing historical configuration or hooks. The current
Owner must independently authorize the exact donor and current Issue Scope.
The shaped label preserves the completed Shape stage. Original Author, Validator
and publication records remain explicit historical evidence; no new Shape or Run
is invented. Acceptance, CI and protected completion must run on current bytes. The original Writer resumes only when normal dispatch reaches
Deliver; three-way integration preserves current main and retained edits.

The first legacy profile requires a completed Writer waiting at Accept and zero
spent repairs. Other boundaries remain explicitly unsupported. A maintenance
pause is owned through a durable journal and CAS; unknown writes, current work or
a later Owner hold cannot be erased by import or crash recovery. Source-contract
tests and donor readback are not proof of actual resumed delivery.

An activated host with independently verified completed local cycles can use the
existing sealed upgrade boundary. The host checks actual completed project Runs,
released pool jobs and unused physical resources, retains the exact cycle bytes,
and owns one reversible maintenance pause. Applicable completed activation and
repair records move to private history before their fixed paths are reused.
Owner holds, pending or unaccepted cycles and unknown effects remain blocking.
The existing explicitly bound failed-closure repair profile remains separate.

Completed continuation receipts are checked as historical evidence under the
current independently trusted Owner key. Their original signature, source and
receipt identities remain unchanged after activation changes or Grant expiry.
This read-only check returns no active Grant or permission to execute again.
Normal dispatch still requires the actual current installation and authorization.

Repository development instructions in AGENTS.md apply to the continued Writer
and reviewers, including whole-file source size limits and resource cleanup.

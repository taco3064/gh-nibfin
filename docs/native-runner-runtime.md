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
Open files, process references, mapped files, visible mounts, hard links, changed
trees or unknown observations retain the slot. Disposable application files are
atomically parked before removal. A retained inventory permits recovery after
partial removal only when every remaining entry is unchanged. Completed cleanup
keeps a compact tombstone; it cannot remove replacement work at the old path.

## Current integration boundary

The native control daemon still reports delivery as blocked. This runtime and its
process/service adapters are implemented; the default control composition must
still bind the real Core work-custody owner, actual queued-Job intake/dispatch,
the managed installation transition and preserved original work before activation.
Process and service fixtures are not a live GitHub Runner or autonomous-delivery
certification. The existing member login and unaccepted candidates remain intact.

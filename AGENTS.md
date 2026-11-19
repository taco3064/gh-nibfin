# Repository instructions

## Follow GitHub's documented mechanisms

Design every GitHub integration around GitHub's documented behavior. Use
GitHub Actions for job assignment, queueing, concurrency, cancellation and
reruns. Use GitHub's native state, permissions and branch protection for
repository operations. Check the current workflow and repository settings
before making claims about their behavior.

Treat GitHub's documented guarantees as the design baseline. Do not assume
GitHub has a defect or add a second scheduler, queue, lock, retry policy, or
approval gate to compensate for a hypothetical gap. Keep any Nibfin-specific
safeguard tied to a demonstrated responsibility of Nibfin. Explain the
observed need before proposing a safeguard, and keep its scope limited to
the affected operation.

If the documented mechanism appears insufficient or observed behavior
contradicts it, bring the relevant official documentation, effective settings,
and a minimal reproduction or native run record to a human maintainer.
Pause the affected design decision until a maintainer agrees on the diagnosis
and the proposed response. Do not autonomously implement a workaround for
an alleged GitHub gap.

[GitHub Actions concurrency](https://docs.github.com/en/actions/concepts/workflows-and-actions/concurrency)
and [self-hosted runner routing](https://docs.github.com/en/actions/reference/runners/self-hosted-runners#routing-precedence-for-self-hosted-runners).

## Use events instead of periodic repair machinery

Do not introduce timers, cron jobs, scheduled workflows, polling services or
external monitors to keep dispatch, continuation, recovery, installation or
upgrade control flow moving. Use documented GitHub events, explicit lifecycle
completion and the existing operation's normal retry path. Do not replace a
rejected timer with another periodic mechanism.

Scheduled work requires an actual time-based product requirement and explicit
Owner approval. Automatic or reliable operation alone is not such approval.
Explain any genuine platform gap with current source, effective configuration
and official documentation before proposing an exception.

Do not invent a restriction and then add infrastructure to satisfy it. The
requirement that users must not manually handle credentials means automating
supported setup from their existing GitHub CLI login; it does not require
credentials to remain only on the local host. Verify actual permissions and
preserve confidential handling without requiring manual token creation,
copying or pasting.

The Owner has rejected the notification-relay.timer design. Do not extend that
timer or its artifact-polling route, or make it a prerequisite for installation,
Runner replacement or continuation. Use the authorized migration path to remove
its dependencies while preserving active work and existing native event routing.

## Shared Runner upgrades

Use the Nibfin Dashboard's Pause/Resume for an individual Issue when an
operator needs to hold that work at a stage boundary. Record which Issues were
paused for the maintenance and resume only those Issues after verification.

For a Core or Runner upgrade affecting the shared pool, follow the
[Core rollout instructions](https://github.com/taco3064/nibfin-core/blob/main/AGENTS.md#shared-runner-upgrades).
The deployment owns the idle checks, safe service stop, version switch and
restart. Individual Issue pauses are optional. Preserve existing pauses and
let admitted work settle before the services stop.

The Dashboard's Issue control is documented in [docs/dashboard.md](docs/dashboard.md).

## English for public-facing text

Write all externally visible project text in English. This includes Issue and
pull request titles and bodies, comments, reviews, Discussions, release notes,
and repository documentation such as README files, guides, and documentation
published from the repository. Apply the same rule to automated publication.

## Limits and recovery

Do not invent timeouts, retry counts, size or count limits, fixed evidence
versions, or other thresholds that can delay, reject, or stop work. Every
such limit needs a concrete failure it prevents and a basis for its value,
such as a documented platform constraint or a measured resource need.
Explain what happens when the limit is reached and how work can resume.
Discuss any new or changed limit with a human maintainer before adopting it
unless the maintainer has already approved the specific change.

On continuation, check the authoritative GitHub state for the work already
completed. When a successful read finds no completed outcome, rerun the
unfinished stage using its normal admission and native GitHub checks.
A missing local receipt alone must never stop continuation. Keep the
ordinary handling for failed observations and apply it to the affected step.

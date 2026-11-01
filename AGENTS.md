# Global working agreements

## Mandatory code structure and maintainability

- Every line in a program source file must be at most 90 characters long.
- Every program source file must contain at most 1,000 lines.
- These limits apply to the entire file whenever it is created or modified,
  including pre-existing violations. Do not limit compliance to the changed lines
  or excuse violations as legacy code.
- Split files and refactor responsibilities when necessary to meet both limits.
  Do not compress code, obscure names, or duplicate logic to satisfy the limits.
- Strictly follow SSOT (Single Source of Truth), SOLID, and SRP (Single
  Responsibility Principle). Maintain one authoritative implementation for each
  rule or fact, clear responsibility boundaries, and appropriate abstractions.
- Before completing a code change, check every created or modified source file
  against both limits and correct any violations.

## Clean up resources after use

- Treat cleanup as part of completing a task. When a temporary resource created for the work has served its purpose and you have confirmed it will not be needed again, remove it instead of leaving it behind for the user to discover.
- Cover temporary files, helper scripts, test checkouts, prototype configuration, Runner registrations, services, scheduled tasks, test accounts, disposable containers, and obsolete credentials created for those resources. Retire the registration or service before removing its files.
- Check actual use and dependencies before deletion: running processes, mounts, service references, registrations, uncommitted or unpublished work, original conversations, pending work, and required recovery or audit evidence. A stopped service or a clean Git checkout alone does not prove a resource is disposable.
- Preserve active work, user-owned data, current model logins, signing keys, and the minimum necessary recovery evidence. Keep an item only for a concrete remaining purpose; do not accumulate unnecessary copies or backups of disposable material.
- Before reporting completion, verify the cleanup and briefly report what was removed and any items retained for an ongoing purpose. Follow the user's authorized scope and applicable deletion and permission requirements.

## Diagnose errors before reporting

- When reporting an error, failure, or blocked workflow, investigate the underlying cause first and provide the evidence-backed cause. Do not stop at a generic status such as owner-result, blocked, timeout, or repair-budget exhaustion.
- Distinguish the original failure from secondary recovery limits. If evidence is insufficient, continue investigating; state precisely what remains unproven rather than presenting a guess as the cause.

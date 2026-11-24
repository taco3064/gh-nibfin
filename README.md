# gh nibfin

GitHub CLI extension for Nibfin.

The extension is being rebuilt for the redesigned Nibfin. The previous
implementation remains in the Git history at commit `5aad9ea`.

## Commands

```
gh nibfin runner add <claude|codex> [--count N] [--yes]
gh nibfin runner remove <claude|codex> [--count N] [--delete-login] [--yes]
gh nibfin login [claude|codex]
gh nibfin init [-R OWNER/REPO] [--yes]
gh nibfin upgrade [-R OWNER/REPO] [--yes]
gh nibfin status [-R OWNER/REPO] [--json]
gh nibfin pause <issue>...
gh nibfin resume <issue>...
gh nibfin dashboard [--port N] [--no-browser]
```

Every command currently reports `not_implemented`. The behaviour of each
command is tracked as a scope Issue in this repository.

`-R, --repo` defaults to the current directory's repository. A changing
command shows its plan and asks to confirm; `--yes` skips the question.

## Workflow templates

`internal/templates/workflows/` holds the workflows `init` and `upgrade` write
into a repository: the execution set (`nibfin-issue.yml`,
`nibfin-shared-runner.yml`, `nibfin-job.yml`) and the peer set
(`nibfin-issue.yml`). They call nothing in other repositories; the Job runs
`ghcr.io/taco3064/nibfin` pinned to the digest of the latest attested main
image.

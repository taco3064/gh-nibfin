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
gh nibfin pause <issue>... [-R OWNER/REPO]
gh nibfin resume <issue>... [-R OWNER/REPO]
gh nibfin dashboard [--port N] [--no-browser]
```

`pause` adds the `pause` label to each Issue; `resume` removes `pause` and
`pending`. An `<issue>` is a number (`N` or `#N`, in the `-R` repository) or
an Issue URL, and several can be given at once. Both commands write all
changes in one GraphQL mutation, read them back, and ask no confirmation.

`login`, `upgrade`, `runner add` and `status` are also implemented. Every
other command currently reports `not_implemented`. The behaviour of each
command is tracked as a scope Issue in this repository.

`-R, --repo` defaults to the current directory's repository; for `status` it
selects the repository view instead (see [Status](#status)). A changing
command shows its plan and asks to confirm; `--yes` skips the question.

## Workflow templates

`internal/templates/workflows/` holds the workflows `init` and `upgrade` write
into a repository: the execution set (`nibfin-issue.yml`,
`nibfin-shared-runner.yml`, `nibfin-job.yml`) and the peer set
(`nibfin-issue.yml`). They call nothing in other repositories; the Job runs
`ghcr.io/taco3064/nibfin` pinned to the digest of the latest attested main
image.

## Upgrade

`gh nibfin upgrade [-R OWNER/REPO]` renders the repository's Nibfin workflows
from the current templates, pinned to the latest attested image digest. The
set is the execution set when the default branch has `nibfin-shared-runner.yml`
or `nibfin-job.yml`, otherwise the peer set; a repository without
`nibfin-issue.yml` needs `gh nibfin init` first. When the result differs from
the default branch, it writes one commit to the branch `nibfin/upgrade`
(replacing that branch's earlier contents) and opens one PR into the default
branch, or keeps the one already open; merging it is up to you. An already
current repository gets no PR. Writing `.github/workflows` needs the
`workflow` scope (`gh auth refresh -s workflow`).

## Runners

`gh nibfin runner add <claude|codex> [--count N]` adds N Runners (default 2)
where the Runners live: the Linux host, or the default `podman machine` VM on
Windows and macOS.

- The first Runner on the host registers to the current directory's
  repository. Later Runners reuse the repository the existing Runners use, from
  any directory; Runners that disagree stop the command
  (`runner_repo_conflict`).
- Each Runner is the official GitHub Actions runner, downloaded through the
  repository's runner downloads and checked against their SHA-256. It is
  registered with the labels `nibfin` and `nibfin-<user id>-<provider>` (the
  user id of the current gh login) besides GitHub's default labels. This needs
  admin access to the repository.
- Each Runner lives in `${XDG_DATA_HOME:-~/.local/share}/nibfin/runners/<provider>-<n>`
  and runs as the systemd user service
  `${XDG_CONFIG_HOME:-~/.config}/systemd/user/nibfin-runner-<provider>-<n>.service`.
  The command enables linger (`loginctl enable-linger`) so the services keep
  running after logout.
- It creates the `nibfin-tools` Podman volume and runs `login` for the
  provider when it is not logged in.
- A rerun after an interruption completes the provider's unfinished slots
  first; they count toward N.

The command only advises, and writes nothing, about:

- On a Linux host, cgroup v2 delegation: when `cpu`, `memory` or `pids` is not
  delegated to the user, delegate them
  ([rootlesscontaine.rs](https://rootlesscontaine.rs/getting-started/common/cgroup2/))
  or set the repository variable `NIBFIN_CONTAINER_LIMITS=off`.
- On a WSL-based machine, which cannot apply per-container limits: set
  `NIBFIN_CONTAINER_LIMITS=off`, and cap `[wsl2] memory=` in
  `%UserProfile%\.wslconfig` to leave at least 3 GB (the `NIBFIN_MEMORY`
  default) per Runner, then run `wsl --shutdown`.

The registration token is fetched for each run and reaches the runner's
`config.sh` only on its stdin; it is never stored or printed.

## Login

`gh nibfin login [claude|codex]` logs a provider CLI in where the Runners run.
It needs Runners of that provider on the host (`no_runners` otherwise; add
them with `runner add`).
It creates the `nibfin-home-<provider>` Podman volume when it is missing and
runs the CLI's own login (`claude auth login`, or `codex login --device-auth`)
in `ghcr.io/taco3064/nibfin:main` with the Jobs' user namespace and config
home. It then reads the state back with the CLI's own check
(`claude auth status`, `codex login status`). Without a provider it logs in
every provider with Runners on the host that is not logged in.

On a terminal the CLI is attached to it. Otherwise the command prints the URL
to open and the one-time code, and reads a pasted code from stdin.

Host requirements:

- [Podman](https://podman.io/docs/installation).
- On Windows and macOS, a `podman machine` (`podman machine init`). A stopped
  default machine is started, and commands use the machine's rootless
  connection, named as the machine.

gh nibfin stores no token; it uses only the exit status of the CLI's check.

## Status

`gh nibfin status [-R OWNER/REPO] [--json]` reports; it writes nothing. It
never starts the podman machine, pulls an image, creates a volume or writes to
GitHub, and it shows labels, comments and runs as GitHub returns them, without
deriving a cause or advice.

Every view shows the runner host:

- the Podman host and whether its machine is running, with the host's total
  and available memory (`MemAvailable`);
- each Runner's systemd service (active or not) against GitHub's
  online/offline and busy, as listed for the repository the Runners serve;
- each provider's login, from the CLI's own check run with `--pull=never`
  (no `nibfin-home-<provider>` volume means not logged in).

Without `-R` it shows the person view: the open Issues labelled `scope` and
assigned to you on gh's default host, across repositories (GitHub's issue
search, up to its 1,000 results), grouped by repository. Each Issue shows
`pause` and `pending`; a `pending` Issue links the oldest
`<!-- nibfin:pending -->` comment by the account that added the `pending`
label last, or shows `pending (no marked comment)`. Each repository shows
whether its workflows pin the latest attested image (`execution` set), or
that it has the `peer` set or none.

With `-R` it shows the repository view: all open Issues of that repository,
its image, and whether a `Nibfin · ` run succeeded after the init PR merged.
The init commit is the oldest commit of `nibfin-issue.yml` on the default
branch that starts with the generated header.

A part that cannot be read (no Podman, a stopped machine, no admin access to
list runners, an image that cannot be verified, an unreadable repository)
shows its own `<code>: <message>`, and the rest of the report is still
printed. Only an invalid `-R` (`invalid_repo`) or no gh login
(`auth_unavailable`) fails the command. `--json` prints the same report as a
JSON object.

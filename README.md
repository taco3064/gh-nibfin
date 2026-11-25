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

`login`, `init`, `upgrade`, `runner add`, `runner remove` and `status` are
also implemented. `dashboard` currently reports `not_implemented`. The
behaviour of each command is tracked as a scope Issue in this repository.

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

## Init

`gh nibfin init [-R OWNER/REPO]` prepares one repository for Nibfin and opens
one PR that imports its workflows. It needs admin access to the repository and
a gh login with the `workflow` scope; without that scope it stops before any
write and asks for `gh auth refresh -s workflow`.

It reads this host's Runners first. Without Runners it stops and points to
`gh nibfin runner add`. When the Runners serve the target repository it uses
the execution set; otherwise the peer set, which wakes the Runners'
repository. A stopped podman machine is started (through the autostart entry
when one is registered); init registers no entry, installs no Runner, logs
nothing in and creates no volume.

It then sets, only where they differ:

- the labels `scope`, `shaped`, `delivered`, `pause` and `pending` (an
  existing label keeps its colour and description; one that differs only in
  case is renamed);
- pull request creation limited to collaborators;
- approval required for fork PR workflows: on a public repository the policy
  "Require approval for all external contributors"; on a private repository
  "Require approval for fork pull request workflows" when fork PR workflows
  run (where they are off, they stay off). When an organization or enterprise
  manages this policy and GitHub refuses the change, init stops before the
  variables, the Secret and the PR; require approval there and run init again;
- "Automatically delete head branches" off;
- the variable `NIBFIN_EXECUTION_REPO` (the Runners' repository) and, in the
  execution repository, `NIBFIN_PROVIDER` (kept when it is `claude` or
  `codex`, otherwise the one provider of the Runners; with Runners of both
  providers set it yourself with `gh variable set NIBFIN_PROVIDER`);
- the Secret `NIBFIN_GITHUB_TOKEN` from the current gh login, every time. The
  token goes to `gh secret set` on its standard input and is never printed.

The workflows, pinned to the latest attested image, go to the branch
`nibfin/init` and one PR into the default branch; a rerun keeps that PR.
A repository that already has `nibfin-issue.yml` on its default branch gets
its prerequisites but no PR: use `gh nibfin upgrade`.

GitHub offers Issues "Creation allowed by: Collaborators only" only in the web
UI, so init prints the repository's Settings link and asks you to set it there.

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

On Windows and macOS the Runners are online only while the `podman machine`
VM runs, so the command also sets up the host to keep the machine up while
the host is on. Each change is listed in the plan, made only when it is
missing (a rerun changes nothing already set), and read back:

- Windows, WSL machine: `[wsl2] vmIdleTimeout=-1` in
  `%UserProfile%\.wslconfig`. Every other key and section is kept; the file or
  the section is created when missing. This needs WSL 2.x (`wsl --version`);
  an older WSL is refused (`unsupported_host`) with the hint to run
  `wsl --update`. A key set while the VM runs takes effect after the next
  `wsl --shutdown`; the command never shuts the VM down.
- Windows: the per-user Task Scheduler task `Nibfin podman machine`, which
  runs `podman machine start <machine>` without a window (through
  `conhost.exe --headless`) at the user's logon and on resume from sleep
  (System log, `Microsoft-Windows-Power-Troubleshooter` event 1). It runs with
  the user's normal privileges and on battery, without repetition, and ignores
  a new start while one runs. It needs no admin rights; a refused registration
  stops the command (`autostart_failed`) before any Runner step.
- macOS: the per-user LaunchAgent
  `~/Library/LaunchAgents/io.github.taco3064.nibfin.podman-machine.plist`
  (`RunAtLoad`), which runs `podman machine start <machine>` at login.
- A stopped machine is started through that task or LaunchAgent
  (`launchctl kickstart`), so the VM does not depend on the terminal or app
  that ran `gh`. The command continues once that start ends.

Linux needs none of this: the Runners are systemd user services with linger.
Power and sleep settings are never changed.

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

`gh nibfin runner remove <claude|codex> [--count N] [--delete-login]` removes
N of the provider's Runners on this host, all of them without `--count`.

- Unfinished Runners (left by an interrupted `runner add` or `runner remove`)
  go first, then idle ones, then busy ones.
- The chosen Runners lose their custom labels first, so they take no new
  nibfin Job. GitHub can still give them a Job whose `runs-on` uses only the
  default labels (`self-hosted`, the OS, the architecture) in the same
  repository; removal waits for such a Job too.
- A busy Runner is removed after its Job ends: the command waits until no Job
  runs on it, then stops its service. The running Job is never cancelled.
  The wait uses GNU coreutils `tail --pid` on the host.
- Each removal deletes the GitHub registration (this needs admin access to
  the repository), the systemd user service and the Runner's directory. The
  command then reads back how many Runners of the provider remain.
- A count above the existing number is refused with that number.
- `--delete-login` deletes the `nibfin-home-<provider>` volume only when no
  Runner of the provider remains on this host; otherwise it is kept.
- When no Runner of any provider remains on the host, the command also
  removes the task or LaunchAgent that starts the machine. It leaves
  `%UserProfile%\.wslconfig` as it is, since other WSL users may rely on
  `vmIdleTimeout`, and says so. A Runner left busy keeps the entry.
- An interrupted removal is finished by running `runner remove` again. Do that
  before `runner add`, which would otherwise restart a Runner whose
  registration is already gone.
- `runner_busy` means GitHub reports a Runner busy but no Job of it runs on
  the host; the Runner keeps running without its nibfin labels until the next
  `runner remove` finishes it.

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
- whether the task or LaunchAgent that starts the machine is present
  (`autostart: ... present`, marked outdated when it differs from the one
  `runner add` registers; `absent`; or not needed on Linux), read also while
  the machine is stopped;
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

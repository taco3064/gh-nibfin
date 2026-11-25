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

`login`, `init`, `upgrade`, `runner add`, `runner remove`, `status` and
`dashboard` are also implemented (see [Dashboard](#dashboard)). The behaviour
of each command is tracked as a scope Issue in this repository.

`-R, --repo` defaults to the current directory's repository; for `status` it
selects the repository view instead (see [Status](#status)). A changing
command shows its plan and asks to confirm; `--yes` skips the question.
The plan lists every item the command considers, marking `[no change]` for
those already in place and `[after start]` for those only known after the
machine starts, then its notes. While it applies, each item prints
`started:`, then `done:` or `failed:` on stderr. The result then lists each
item as `[confirmed]` or `[not confirmed]` with the reason, also when the
read-back fails; the error still follows on stderr as `<code>: <message>`
and the exit code is 1. These lines come from the same items the
[Dashboard API](#api) returns.

## Workflow templates

`internal/templates/workflows/` holds the workflows `init` and `upgrade` write
into a repository: the execution set (`nibfin-issue.yml`,
`nibfin-shared-runner.yml`, `nibfin-job.yml`, `nibfin-runner-request.yml`)
and the peer set (`nibfin-issue.yml`). They call nothing in other
repositories; the Job runs `ghcr.io/taco3064/nibfin` pinned to the digest of
the latest attested main image.

- The stage Job runs on any Runner labelled `nibfin-<user id>` of the Issue's
  first Assignee and takes the provider from that Runner: its `.env` sets
  `NIBFIN_PROVIDER` to `claude` or `codex` (the runner reads it when it
  starts). Without a valid value the Job stops before any container starts.
  No repository variable or input chooses the provider; an old
  `NIBFIN_PROVIDER` repository variable is unused and can be deleted.
- Event text, inputs and repository variables reach every script only as
  quoted environment variables, never as `${{ }}` inside a `run:` script.
- `nibfin-issue.yml` also runs when a review is submitted on a pull request.
  A review on a `nibfin/issue-*` pull request from the same repository wakes
  its Issue by branch when the reviewer has admin or write permission, the same
  check as for Issue events. Reviews on other branches or from forks are not
  routed, and edited or dismissed reviews start no run.
- Required approvals: when a branch protection rule or Ruleset requires
  approving reviews, Nibfin waits for a member's approval, and submitting
  that approval wakes the Issue. Nibfin's pull requests are authored by the
  login `init` stored as `NIBFIN_GITHUB_TOKEN` (the Owner's), and the author
  cannot approve their own pull request. So in an Owner-only repository
  that requires approvals, Nibfin's pull requests cannot merge. Existing
  repositories get the review trigger with `gh nibfin upgrade`.

### Runner request

`nibfin-runner-request.yml` gives a collaborator of the execution repository
a runner registration token for it. Making someone a collaborator with write
access approves their Runners. It runs on a GitHub-hosted runner when an
Issue is opened and answers only Issues whose body has the request marker.

The request is an Issue opened by the requester with:

- the title `Nibfin runner request`;
- a body with the line `<!-- nibfin:runner-request -->` and exactly one line
  `ssh-key-id: <id>`, the id of one of the author's SSH keys in
  `GET /users/{login}/keys` (`ssh-ed25519` or `ssh-rsa`). Other text is
  ignored. The requester is always the Issue's author.

When the author is a user with write or admin permission, the workflow
creates a registration token with `NIBFIN_GITHUB_TOKEN`, masks it, encrypts it
with [age](https://github.com/FiloSottile/age) to that SSH key and comments:

````text
<!-- nibfin:runner-registration -->
requester: <login>
ssh-key-id: <id>
expires-at: <time>

<a sentence>

```text
-----BEGIN AGE ENCRYPTED FILE-----
…
-----END AGE ENCRYPTED FILE-----
```
````

Otherwise (another title, zero or several `ssh-key-id` lines, an unknown key
or another key type, read or no permission, a non-user author) and when the
workflow fails, it comments

```text
<!-- nibfin:runner-request-refused -->
reason: <why>

<what to do next>
```

and closes the Issue as not planned. Both comments are written by the
workflow's `GITHUB_TOKEN`; a rerun updates the workflow's earlier comment of
the same kind instead of adding one, and stops when the Issue is closed. Only
`opened` starts the workflow, so editing a request does nothing. The token
expires after an hour. `internal/runnerreq` holds the same
contract for `gh nibfin runner add`.

## Init

`gh nibfin init [-R OWNER/REPO]` prepares one repository for Nibfin and opens
one PR that imports its workflows. It needs admin access to the repository and
a gh login with the `workflow` scope; without that scope it stops before any
write and asks for `gh auth refresh -s workflow`.

It reads this host's Runners first. Without Runners it stops and points to
`gh nibfin runner add`. When the Runners serve the target repository it uses
the execution set; otherwise the peer set, which wakes the Runners'
repository. For the peer set the current gh login must be admin of the
Runners' repository too; otherwise (Runners of a collaborator) init stops
before any write (`runner_repo_not_admin`), so it never stores your login as
another repository's Secret. A stopped podman machine is started (through the autostart entry
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
- the variable `NIBFIN_EXECUTION_REPO` (the Runners' repository). Init
  neither writes nor reads `NIBFIN_PROVIDER`; each Runner names its provider
  (see [Workflow templates](#workflow-templates)), and an existing variable is
  left as it is;
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
set is the execution set when the default branch has `nibfin-shared-runner.yml`,
`nibfin-job.yml` or `nibfin-runner-request.yml`, otherwise the peer set (an
execution repository set up before `nibfin-runner-request.yml` existed gets it
in the upgrade PR); a repository without
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
- Each Runner is the official GitHub Actions runner, checked against its
  published SHA-256. It is registered with the custom label `nibfin-<user id>`
  (the user id of the current gh login), which the stage Job of an Issue
  assigned to that user runs on, besides GitHub's default labels
  (`self-hosted` among them). Only an admin's Runners also get `nibfin`, which
  routing runs on, so routing never runs on a collaborator's host.
- Each Runner's `.env` holds `NIBFIN_PROVIDER=<provider>` before its service
  first starts; the stage Job reads the provider from it. Runners that were
  already complete keep their labels and `.env` as they are.
- The command takes one of two routes by the gh login's access to the
  repository:
  - **Admin**: the runner archive comes from the repository's runner
    downloads and the registration token from GitHub directly.
  - **Write collaborator** (the request route): the archive comes from the
    latest public `actions/runner` release (its asset `digest`), and the token
    through a request Issue that `nibfin-runner-request.yml` answers. The
    command needs a local SSH key pair in `~/.ssh` (`ssh-ed25519` or
    `ssh-rsa`) whose public key is on the user's GitHub account; otherwise it
    stops before any change (`ssh_key_missing`) and names the steps:
    `ssh-keygen -t ed25519`, then `gh ssh-key add ~/.ssh/id_ed25519.pub`
    (`gh auth refresh -s write:public_key` when gh asks), then run
    `runner add` again. A key with a passphrase is unlocked on the terminal
    without echo, in at most three tries, before the Issue is opened; without
    a terminal (the Dashboard, piped input) or after three wrong tries it
    stops (`ssh_key_locked`). The command then opens the Issue
    `Nibfin runner request`, waits up to 10 minutes for the workflow's
    encrypted answer, decrypts it in memory with the key and registers. Only
    a comment by `github-actions[bot]` for this user, this key and a future
    expiry counts. Afterwards it comments the registered runner names and
    closes the Issue as completed. A refusal stops with its reason
    (`runner_request_refused`); no answer in time closes the Issue as not
    planned (`runner_request_timeout`, run `runner add` again); an Issue
    closed by someone, a malformed answer or one that does not decrypt is
    `runner_request_failed`. Every other failure or a cancel also closes the
    Issue as not planned. A rerun that only finishes configured Runners opens
    no request. GitHub's runner list needs admin, so the read-back confirms a
    collaborator's Runners from the host only.
  - Without write access the command stops before any change
    (`runner_request_refused`): the repository's owner must add the user as
    a collaborator with write access.
- Each Runner lives in `${XDG_DATA_HOME:-~/.local/share}/nibfin/runners/<provider>-<n>`
  and runs as the systemd user service
  `${XDG_CONFIG_HOME:-~/.config}/systemd/user/nibfin-runner-<provider>-<n>.service`.
  The command enables linger (`loginctl enable-linger`) so the services keep
  running after logout. On a `podman machine` whose unit directory is
  root-owned (as podman's WSL provisioning creates it), it gives the VM user
  ownership of that directory and its `default.target.wants` with
  `sudo -n chown`, without recursing; a refused `sudo` stops the Runner
  (`runner_install_failed`) with the `chown` to run inside the VM.
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
`config.sh` only on its stdin; it is never stored, printed or passed as an
argument. The SSH private key and its passphrase never leave the gh process.

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
to open and the one-time code, and reads a pasted code from stdin. A
cancelled login without a terminal removes its `nibfin-login-<provider>`
container.

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
its image, and whether a `Nibfin · ` run succeeded after `nibfin-issue.yml`
last changed on the default branch. A repository uses Nibfin when its default
branch has `.github/workflows/nibfin-issue.yml`, whether `gh nibfin init`
generated it or the repository maintains it by hand. The last change is the
newest commit that changed the file on the default branch, at the merge time
of its PR, or else at its commit time; the check reads that commit only and
never walks the file's history. A repository without the file shows that it
does not use Nibfin.

Both views also read, for the Dashboard and `--json`:

- each Issue's native `Blocked by` Issues, open and closed, in any
  repository (repository, number, title, state, URL), read in batches; the
  text output shows only the open ones (`blocked by: OWNER/REPO#N Title`);
- each Issue's Nibfin runs, the runs titled `Nibfin · OWNER/REPO#N · …` in
  its repository and in the execution repository (the one this host's
  Runners serve), newest first. Each repository's run list is scanned once,
  from the oldest listed Issue's creation time; `runsTruncated` marks Issues
  whose scan reached GitHub's 1,000-run window;
- the gh account's permission on each listed repository, as GitHub names it
  (`permission`);
- the account's repositories (owned, as a collaborator or through an
  organization) whose default branch has `nibfin-issue.yml` (`nibfinRepos`),
  each with its URL, visibility, permission and whether it is the execution
  repository.

Each of these reports its own error (`blockedByError`, `runsError`,
`permissionError`, `nibfinReposError`), never an empty list in its place.
Stages are what the labels show; nothing is derived from runs.

[`schema/status.schema.json`](schema/status.schema.json) (JSON Schema draft
2020-12) describes the `status --json` output of both views, which is also
the `result` of the Dashboard's `status` route. It is generated from the Go
types; after changing them, regenerate it with
`go test ./internal/status -run TestSchemaFile -update` (the tests fail while
it is stale). A command-level failure prints the `{code, message}` error
object, which this schema does not describe.

A part that cannot be read (no Podman, a stopped machine, no admin access to
list runners, an image that cannot be verified, an unreadable repository)
shows its own `<code>: <message>`, and the rest of the report is still
printed. Only an invalid `-R` (`invalid_repo`) or no gh login
(`auth_unavailable`) fails the command. `--json` prints the same report as a
JSON object.

## Dashboard

`gh nibfin dashboard [--port N] [--no-browser]` runs a local server in the
foreground until Ctrl-C (or SIGTERM). It listens on `127.0.0.1` only, on port
`N` or any free port (`listen_failed` when the port is taken).

It prints `Dashboard: http://127.0.0.1:<port>/` and opens your browser (gh's
launcher: `GH_BROWSER`, the gh `browser` setting, `BROWSER`, then the system
default) on the session URL `http://127.0.0.1:<port>/#proof=<proof>`. With
`--no-browser`, or when no browser can be opened, it prints the session URL
instead.

The proof in the URL works once. The first page or client that exchanges it
gets the run's only session, which gives the same access as your gh login on
this machine; keep the URL private until then. The terminal prints
`session opened` when that happens. If it appears when you did not open the
URL, stop the server: that ends the session. A reload, a second tab or a
copied URL finds the proof used (`proof_invalid`); restart the server for a
new session URL. Nothing is stored: the proof, the session and the plans live
in the server's memory.

The page is the Dashboard UI, built from `dashboard/` and embedded in the
binary. It removes the proof from the address bar, exchanges it and keeps the
session in memory only. It then opens the live stream below, which keeps the
person view current without reloading (see [Live updates](#live-updates)). It
shows three views: Person (your open Issues with `scope`, grouped by
repository, each with whether its workflows use the latest image),
Repositories and Runners. The Repositories view picks a repository from a
searchable list of your repositories that use Nibfin on the current host (the
person read's host, or the open repository's), or by `[HOST/]OWNER/REPO`
entered by hand, and reads its repository-view `status` once, and again on
Refresh; it has no live updates. Both views show each section as soon as it
is read, with its own loading state until then: the open Issues first, then
their blockers, runs and pending comments, the image, the Nibfin run, the
permission, the host and the repository list. A Refresh keeps the data shown
until the new read is complete. It shows the image its workflows pin, whether
a Nibfin run succeeded after `nibfin-issue.yml` last changed (or that the
repository does not use Nibfin), Init or Upgrade through the
operation dialog with the repository filled in, and a dependency graph of its
open Issues: each points to its blockers, including closed ones and those in
other repositories. Search and stage, paused, pending and blocked filters hide
Issues but keep the blockers of those shown visible as context. A freshness
indicator shows the time of the data shown (the open repository's read on the
Repositories view) and turns stale after five minutes. The page opens on the
Runners view, with a guide to `runner add` then `init`, when this host has no
complete Runner (or no Podman or machine), and on the Person view otherwise.
Its Dark, Light and System theme choice is the only thing it stores (in the
browser).

The Runners view shows the host (the podman machine state and its free
memory), each Runner slot's local service against GitHub's online, offline and
busy state, and each provider's login. It opens `runner add`, `runner remove`
and `login` in the operation dialog, built from `/api/commands` and the plan
and apply routes below, and reads the status again after the dialog closes. A
login, also one `runner add` runs, shows the CLI's URL and either a field for
the pasted code (Claude) or the one-time code to copy (Codex), then Waiting for
authorization, Verifying and Done once the read-back confirms it. Closing the
dialog while a login waits, or 15 minutes without authorization, ends the
request; the server then cancels the apply and removes the login container.

A binary
built without the UI serves a placeholder page whose only script does the same
exchange. The page and its files need no session; they keep the Host and Origin
checks below, and other paths outside `/api/` are `not_found`.

### API

Every request must carry `Host: 127.0.0.1:<port>` exactly (`forbidden_host`,
also for `localhost`), and an `Origin` header, when present, must be exactly
`http://127.0.0.1:<port>` (`forbidden_origin`). No CORS header is ever sent.

- `POST /api/session` with `X-Nibfin-Proof: <proof>` returns
  `{"session": "<value>"}` once. Every other `/api/` request needs
  `X-Nibfin-Session: <value>` (`session_required`).
- `GET /api/commands` returns `{"commands": [...]}`: every runnable command's
  `path` (such as `runner add`), `short` and `long` descriptions, `flags`
  (`name`, `shorthand`, `type`, `default`, `usage`, `required`, `allowed`) and
  positional `args` (`name`, `required`, `repeatable`, `allowed`), built at
  start from the CLI's own command definition. `api` is the command's name in
  the plan route below (absent for `runner` and `dashboard`), and each input's
  `param` is its request field there; it is empty for CLI-only inputs such as
  `--yes`, `--json`, `--port` and `--no-browser`.
- `POST /api/commands/<command>/plan` with the command's parameters as JSON:
  - `runner-add` `{"provider", "count"}` (count defaults to 2);
  - `runner-remove` `{"provider", "count", "deleteLogin"}` (count 0 removes
    all);
  - `login` `{"provider"}` (empty: every provider that needs it);
  - `init`, `upgrade` `{"repo"}` (empty: the directory the server started in);
  - `status` `{"repo"}` (empty: the person view; set: the repository view);
  - `pause`, `resume` `{"repo", "issues"}`.

  A plan is `{"operation", "repo", "steps", "notes", "confirm",
  "readOnly"}`. Every changing command lists each item it considers in
  `steps`, including those already in place. A step is `{"id", "action",
  "target", "detail", "current", "next", "noChange", "afterStart"}`: `id`
  names the item in the plan, its progress and its read-back; `current` is
  the state read at plan time (absent when unknown) and `next` the resulting
  state; `noChange` marks an item already in place, which nothing applies;
  `afterStart` marks an item whose target or state is only known after the
  machine starts (such as the slots `runner add` chooses). A Secret's states
  are only `set` or `absent`. `notes` are advice and steps only you can take,
  such as the web-UI setting of `init`, absent when there are none; they are
  not items.

  A read-only plan returns `{"plan", "result"}` and is read back at once:
  `status`, whose result is the object `status --json` prints, and a
  changing command with no changing item (`upgrade` when current, `login`
  with nothing to do, `runner remove` when every item is kept). When that
  read-back fails, the response is the error body with `plan` and, when the
  read-back returned one, `result`, status `422`. Any other plan returns
  `{"id", "plan"}`.
- `POST /api/commands/status/read` with `{"repo"}` (empty: the person view)
  reads `status` as the plan route does and answers NDJSON while it reads:
  `{"event":"partial","result":{...},"loading":[...]}` each time a section is
  read while others are still being read, then one
  `{"event":"result","result":{...}}` or
  `{"event":"error","error":{"code","message"},"result":{...}}`. A partial
  `result` holds the sections read so far; `loading` names those still being
  read, by their key in the result (`host`, `latestImage`, `issues`, `image`,
  `nibfinRun`, `permission`, `nibfinRepos`, `user`, `repos`) or by the Issue
  field still being read (`pendingComment`, `blockedBy`, `runs`). The last
  `result` is the object `status --json` prints. Closing the request cancels
  the read.

  The result of every changing command has `items` and `notes` next to its
  command keys. `items` holds one `{"id", "action", "target", "confirmed",
  "state", "reason"}` per item, including the no-change ones, which are read
  again: `state` is the state read back and `reason` says why an item is not
  confirmed. An item a failed read never reached is not confirmed, with that
  error as its reason.
- `POST /api/plans/<id>/apply` with `{"confirm": true}` applies a stored plan
  once. A plan with `"confirm": true` that is applied without it fails with
  `confirmation_required`, writes nothing and stays stored. The response is
  NDJSON: `{"event":"progress","progress":{"id","action","target","phase",
  "code","message"}}` for each item as it is `started`, then `done` or
  `failed` (with the error's `code` and `message`; an Issue of `pause` or
  `resume` whose outcome is unknown after a failed mutation is `failed`, never
  `done`); `{"event":"login_step","step":{...}}` for each login step
  (`open_url`, `device_code`, `code_requested`, `succeeded`, `failed`),
  between the `started` and `done` of its log-in item; then one
  `{"event":"result","result":{...}}` or
  `{"event":"error","error":{"code","message"},"result":{...}}`, where
  `result` is present when the read-back returned one. Closing the request
  cancels the apply, as Ctrl-C cancels a command.
- `POST /api/plans/<id>/code` with `{"code": "..."}` hands a pasted code to the
  login that asked for it. It is accepted once the `code_requested` line has
  been sent, and `409` means no code is requested.
- `DELETE /api/plans/<id>` drops a stored plan.

One apply runs at a time; another is refused with `operation_running`. Errors
are `{"error": {"code", "message"}}` with the codes the CLI prints: `400` for
a malformed request, `401`/`403` for the checks above, `404` for an unknown
command, route or plan, `409` for a conflict, and `422` for an error from the
operation itself.

### Live updates

The page stays current by itself; nothing is pushed by GitHub and no Job ever
connects to the Dashboard. Dispatch is unchanged: it stays event-driven in
Core.

- **Local Jobs.** The server follows `podman events` on the Runners' store
  (the default machine's rootless store on Windows and macOS; it never starts a
  machine). The Job template labels each Job container with
  `io.github.taco3064.nibfin.repo` and `io.github.taco3064.nibfin.issue`;
  `gh nibfin upgrade` brings the label to existing execution repositories.
  When a labelled container of a listed Issue starts or ends, only that Issue
  is read again. Jobs of Issues not in the view cause no read. While Podman is
  missing or the machine is stopped the page says local Job events are not
  watched, and a later check picks Podman up again.
- **Everywhere else** (other runners, hosted CI, edits on github.com): GitHub
  is checked every 30 seconds or more with conditional requests (`ETag`,
  `If-None-Match`), honouring `x-poll-interval` and `Retry-After`. An
  unchanged answer is a `304`, which does not count against the primary rate
  limit. The checks are the open `scope` Issues assigned to you (labels,
  state, assignment, title), the Nibfin runs (`nibfin-issue.yml` of each
  listed repository, `nibfin-shared-runner.yml` of the execution repository)
  and each listed repository's workflow files. A changed Issue list or
  workflow folder reads the whole view again; newer Nibfin runs read only
  their Issues again.
- **Not live:** the latest image release, permissions, the Nibfin
  repository list and blockers changed in other repositories update on the
  next full read or on Refresh.

The freshness time is the last successful check, or the last read. A failed
check shows its error and stops the time, which turns stale after five
minutes. When the stream ends the page shows "Live updates off" with the
reason; Refresh reconnects it. Without a stream the page reads once, as
Refresh does.

`GET /api/live` (session header required, like every `/api/` route; the
session never goes in the URL) answers `text/event-stream`. Each event is one
`data: <json>` frame with `event` and, by event:

- `partial` `{at, result, loading}`: before the stream's first `snapshot`
  only, the part of the first person-view read done so far, with `loading` as
  in the status read route above;
- `snapshot` `{at, result}`: the whole person view, `result` being the object
  `status --json` prints;
- `issue` `{at, repo, issue}`: one re-read Issue of `repo`;
- `checked` `{at}`: every check succeeded and found nothing new up to `at`;
- `podman` `{watching, error}`: whether local Job events are followed;
- `error` `{source, error}`: a failed GitHub check (`github`) or read
  (`status`). A first read that fails as a command, such as no gh login, ends
  the stream with this frame.

`at` is the server's RFC 3339 time. A session has one stream: a new request
ends the one before. The stream ends when the page leaves or the server stops.

### Public live data Worker

`dashboard/src/worker/` is a Cloudflare Worker that plays the Dashboard
server's role for the public read-only site. One Durable Object serves every
visitor at `wss://<worker>/live` (a WebSocket upgrade is required, otherwise
`426`; other paths are `404`). It reads `taco3064/nibfin-core`,
`taco3064/gh-nibfin` and `taco3064/nibfin-e2e` with a read-only fine-grained
token (Issues, Actions and Administration read) held only as the Worker secret
`GITHUB_TOKEN`; the token never reaches a page, a message or the stored state.
It never writes to GitHub. `.github/workflows/worker.yml` deploys it from
`dashboard/src/worker/wrangler.jsonc` and sets the secret.

A Durable Object alarm checks GitHub every 30 seconds or more, only while a
page is connected, with conditional requests (`ETag`, `If-None-Match`),
honouring `x-poll-interval` and `Retry-After`. GitHub usage is the same for
one page or many. When anything changed, every page receives the same message.
Each WebSocket text message is one event of the [Live updates](#live-updates)
format: `snapshot` `{at, result}`, `checked` `{at}` and `error`
`{source: "github", error}`; `issue` and `podman` are not sent. A new page
first gets the last snapshot, then `checked` or the last errors. A failed
check keeps the last good data, sends `error` and stops the `checked` time.
Before the secret is set, pages get `auth_unavailable` and no request is made.

`result` (`LiveData`, typed in `dashboard/src/worker/messages.ts`) holds:

- `repos`: per repository `repo` (`github.com/OWNER/REPO`), `issues` and
  `issuesError`, as in `status --json`'s repo view: the open Issues with
  labels, `pause`, `pending`, blockers and the Nibfin runs from each
  workflow's newest 100 runs (`runsTruncated` when older ones may exist).
  `pendingComment`, image, init-run, permission and repository-list data are
  not included;
- `summary`: `created`, the Issues created since 2026-11-21 (Asia/Taipei),
  and `delivered`, the Issues closed since then with `delivered` whenever they
  were created, summed over the three repositories from search totals;
- `runners`: per repository each runner's GitHub `status` and `busy`, and
  for a busy runner its running job's repository, Issue or ref, and stage,
  read from the job's run title.

### Building the Dashboard UI

The UI is a Vite, React, TypeScript, Tailwind and shadcn/ui project in
`dashboard/`. Node is needed only to build it; the binary embeds the output and
runs without Node. Build it before `go build` to embed it:

```sh
cd dashboard
npm ci
npm run build   # writes internal/dashboard/dist/
cd ..
go build
```

`internal/dashboard/dist/` is not committed; only its `.gitignore` is, so
`go build` and `go test` work without a build (the placeholder page is served).
In `dashboard/`, `npm run typecheck`, `npm run lint` and `npm test` (unit tests,
no browser) check the project; CI runs them with the build before the Go steps.

`npm run screenshots` builds the page and a separate harness of the shared
Issue element (`screenshots.html`) into the ignored `dashboard/.screenshots-dist/`,
starts `go run -tags dashboardscreenshots . dashboard` on port 4790, which serves
that folder instead of the embedded UI, with a mocked API, captures the pages
at 375, 768, 1280 and 1920 px in the dark and light themes into the ignored
`dashboard/.screenshots/`, and stops the server. No other build has the tag, so
the harness never reaches `internal/dashboard/dist/` or a product binary.
It fails on a console error or a Content-Security-Policy violation. Install its
browser first with `npx playwright install --only-shell chromium`; the
lockfile's Playwright version decides the build.

The page's Content-Security-Policy allows only same-origin files. Scripts have
no inline exception; `<style>` elements are allowed only with a nonce that the
server makes fresh for every page response. The build marks the page with the
placeholder `NIBFIN_CSP_NONCE` (Vite's `html.cspNonce`); the server replaces it
in `meta[property=csp-nonce]` and in the policy's `style-src`, and the client
passes it to the scroll lock of shadcn/ui dialogs, menus and selects through
`get-nonce`'s `setNonce`.

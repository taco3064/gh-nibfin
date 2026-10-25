# gh-nibfin

Local GitHub CLI extension with help, version diagnostics and seven bounded
command paths. Runner creation implements local allocation and diagnostics through
the Core boundary described below. Other business operations return exit status 2.
This is not a complete Nibfin runtime or a supported-platform claim.

Public repositories owned by personal accounts or Organizations are in product
scope. Repo ownership, Repository/Organization Runner registration level and
Owner-enabled automatic registration are separate dimensions. Private repositories
are outside scope. Identity syntax and routing labels confer no registration,
execution permission or Owner authorization.

## Prerequisites and local checks

Installation prerequisites are Node.js 22.x, npm 10.x, a POSIX `/bin/sh` interpreter,
Git, and a GitHub CLI version with `gh extension install`/`remove` support. The root
`gh-nibfin` is executable and resolves `src/cli.mjs` relative to its own location.
No installed gh, login, npm dependency installation or network is needed for local
baseline checks. The loader uses Node built-ins; npm is an installation ecosystem
prerequisite, not a loader subprocess.

```sh
./gh-nibfin --help
./gh-nibfin --version
python3 tooling/check.py
```

The reproducible local check additionally requires Linux, Python 3 and libseccomp.
It fails if it cannot enforce networking denial. The inherited syscall filter
rejects socket creation and network operations, probes the denial, and runs Node
and child processes with a minimal credential-free environment. Unix socketpair
IPC remains available for Node's process plumbing. `node --test
test/*.test.mjs` runs the same tests without this network-isolation proof.
Neither command is actual GitHub CLI installation verification.

The seven paths have these responsibilities:

| Path | Responsibility (create implemented locally; others pending) |
| --- | --- |
| init | Initialize repository workflows/labels and optional automatic registration. |
| runner create | Create one fixed-Agent environment with a positive integer total of unregistered instances; default 2. |
| runner register | Register existing instances, without silently adding instances. |
| runner unregister | Unregister one instance while retaining its environment and other registrations. |
| runner list | Show local environment/registration/service state without secrets; remote state requires an actual authorized read. |
| runner remove | Member-controlled environment removal; report failed remote cleanup. |
| upgrade | Update Nibfin with version, compatibility and authorization checks. |

Help/version are options, not additional commands. Unknown options, extra tokens
and incomplete paths fail with usage guidance. Baseline refusals perform no
initialization, authentication, network access, service operation or registration.
Creation parameters are described below; other parameter execution belongs to later Issues.

## Verified Core boundary

The read-only [N01 handoff](docs/handoffs/n01/README.md) is the initial local
integration input, not a formal release. The approved producer is nibfin #84 /
PR #136, commit `0a2e276ad5b37d94b485bbcf8c7288743b9d158f`, package
`@nibfin/core@0.1.0`. Its 3,980-byte archive has raw-byte SHA-256
`956b12fb80620e18576212d371989b0448cf39878533b813e8cba838a40c1759`.

`src/core.mjs` pins archive and reviewed provenance bytes, checks the exact four
regular files and per-file identities, bounded gzip/tar structure and package
metadata before writing or executing anything from the package. It rejects
changed content, traversal, links, duplicates, extra/missing files and unsafe
metadata. There is no floating latest, download, cache fallback or install script.
A dependency-free installer writes only verified bytes into a fresh external
`node_modules/@nibfin/core`, then a fresh bridge imports by public package name.
The approved package has no dependencies or install scripts. This preserves
package exports resolution without invoking npm or trusting caller dependencies.
Temporary state is removed on success and write/import/callback failures.

The maintained consumer entry exports `withCore`; for example, from this checkout:

```sh
node --input-type=module -e "import {withCore} from './src/cli.mjs'; await withCore(core => console.log(core.memberRunnerLabel('42')))"
```

This uses actual producer exports, not consumer replicas or an eighth product
command. `--version` also verifies and imports Core. Help and refusals need only
Node 22.x. On verification failure, restore the exact reviewed handoff from the
applicable immutable CLI artifact and retry; do not edit pins or use another
archive. Missing Node requires installing Node 22.x. Temporary-directory failure
requires a writable external temporary directory. No automatic repair downloads
or credential reads occur.

## Later real installation procedure — not performed

The later [N15 observation handoff](docs/handoffs/n15/README.md) supplies the
immutable producer interface and accepted package needed by runner registration
work in #21. It is supplemental evidence; the current N01 product loader remains
as described above until a normal reviewed implementation adopts that package.

[GitHub's extension layout](https://docs.github.com/en/github-cli/github-cli/creating-github-cli-extensions)
requires the executable at the repository root with the repository name.
[Official installation/removal](https://docs.github.com/en/github-cli/github-cli/using-github-cli-extensions)
is owned by #27 on Linux and #28 on supported Windows-hosted WSL. Both consume
the applicable initial immutable CLI artifact through their existing release
dependency. Real installation and platform verification are **not yet performed**.
No measured gh version or successful installation transcript is claimed here.

In an authorized disposable suite environment, first obtain and verify that
immutable CLI artifact using its release identity/digest. Extract it as a directory
named `gh-nibfin`, preserving the executable mode. Set `cli_artifact` below to its
absolute extracted path. Use a disposable OS user/container so existing user
extensions, credentials and Agent login stores cannot be read or removed. Supply
only the suite's authorized gh authentication if the installed gh requires it;
never reuse Agent credentials or print tokens.

```sh
# Run as the disposable user. cli_artifact is the verified extracted directory.
scratch=$(mktemp -d)
export HOME="$scratch/home" GH_CONFIG_DIR="$scratch/gh" XDG_DATA_HOME="$scratch/data"
export XDG_CONFIG_HOME="$scratch/config" XDG_CACHE_HOME="$scratch/cache"
mkdir -p "$HOME" "$GH_CONFIG_DIR" "$XDG_DATA_HOME" "$scratch/other"
cd "$cli_artifact"
gh extension install .
cd "$scratch/other"
gh nibfin --help
gh nibfin --version
gh extension list
gh extension remove nibfin
gh extension list
# Assert nibfin is absent from the final list and installed extension directory;
# confirm the immutable source artifact is unchanged, then remove disposable state.
rm -rf "$scratch"
```

Capture OS/WSL host and guest versions, architecture, actual Node/npm/Git/gh
versions, artifact/producer identities, executable mode, command statuses/output,
installed extension resolution from the unrelated directory, removal read-back
and cleanup observations. On failure retain sanitized diagnostics, perform the
same removal/read-back and disposable-user cleanup, and record failure honestly.
These suites prove installation only within their tested configurations; local
module/process tests do not establish full product or platform support.

## Runner creation: local command boundary

```sh
gh nibfin runner create --name local-codex --agent codex --count 4
```

Count is the total for this one environment, not per Repo/Org: omitted means 2;
1 through 128 are supported. Larger values fail rather than truncate. Names must
start with a lowercase letter, contain only lowercase letters, digits or hyphens,
and be at most 48 characters. Agent is `codex`, `claude` or `gemini`; these names
select identity, not a claim that every native backend/platform is installed.
The reviewed interface here is Linux/Codex: Claude/Gemini creation reports a
missing matching adapter rather than invoking a Codex backend for another Agent.
Options occur once; unknown options, extra operands and missing values fail.

Storage is `~/.local/share/nibfin/environments/NAME`, using the OS account
home returned by `userInfo()` rather than an inherited `HOME` override, with private directories (0700) and an `environment.json` record (0600).
Each `instance-N` contains separate `workspace`, `registration` and `service`
directories. UUID local identities are not GitHub Runner IDs. All instances refer
to the one environment `login` directory; another environment has another login
space. Records explicitly say `unregistered` and `not-installed`. Creation does
not register, install/start services, start models, or require a target Repo,
Issue, existing Runner ID or Owner Grant. It conveys no delivery authorization.

Existing names cannot be overwritten, resized or switched to another Agent.
Choose another name for another Agent/account. Symlinks and non-directory storage
components are rejected. Allocation reserves the name exclusively; only the
current attempt's newly reserved directory is removed after failure. A record is
published by renaming a private pending file after prerequisites pass. Readers
must require `environment.json`; directories without it are incomplete attempts.
After successful rollback retry normally. An incomplete-cleanup diagnostic names
the environment: inspect it under the storage root and preserve any login data
before manually removing residual resources. Existing environments and unrelated
files are never rollback targets. Host administrators remain trusted; this layout
is not a new isolation platform or protection against hostile host mutation.

The command checks Git/gh availability, gh auth status and the stable GitHub user
ID using the actual execution account's gh configuration. Actions tokens, provider
keys, alternate inherited Agent login homes and Node injection settings are not
passed to diagnostic subprocesses. It never logs in for the member. Run it as the
intended service account; an interactive user's login does not establish service,
container or WSL login. Backend readiness receives that execution account and the
environment-specific login selection. Backend errors and unavailable/unknown
readiness fail safely with tool/login/isolation recovery guidance. Raw provider
responses, tokens and login contents are neither printed nor persisted. HTTP
401/403 in gh failures are sanitized diagnostics, not authorization grants.

The production loader currently provides **only the pinned N01 identity package**.
It lacks `createAgentEnvironments` and `createLinuxCodexBackend`; create therefore
reports the missing capability after gh checks and publishes no environment.
A reviewed immutable later package must enter through the maintained loader before
native creation can succeed; there is no download, floating pin or fake-ready
fallback. No later package is included or certified by this change. Core owns
descriptor validation and backend login/isolation logic; the CLI calls `describe`
and `readiness`, never backend model lifecycle methods to allocate instances.

`python3 tooling/check.py` discovers both retained test files and enforces network
denial with a minimal credential-free environment. Command tests use an explicit
imported `main` dependency boundary with local Core/process contract doubles,
including an executable Node harness from a checkout path containing spaces.
There is no product flag/environment toggle that enables these doubles. Tests
inspect real disposable filesystem records and exercise default/1/4/invalid
counts, separation, collisions, symlinks, process diagnostics, unavailable Core,
partial publication and rollback failures. These are local injected observations,
not actual Agent login, model execution, native isolation or installed-platform
success. #27/#28 retain actual installed-environment/service/login lifecycle proof.
The supplied upstream API anchor is nibfin commit
`7d3b8f9261cdf61f0d29d7176fa51a5317830f5c`; an attempted read of its source and
lifecycle documentation returned HTTP 403 in this execution. No claim of inspecting
those source bytes or measuring their backend behavior is made.

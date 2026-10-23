# gh-nibfin

Development baseline for the Nibfin GitHub CLI extension. This checkout provides
command routing, validation and help. All seven operations currently report an
unavailable backend; none installs workflows, creates environments, registers
Runners, changes services, removes resources or upgrades software.

Use Node **22.23.3** and npm **10.9.9** (also pinned in package.json and
.node-version). The executable uses Node through PATH. No npm dependencies or
installation step are needed for the baseline. From this checkout:

```sh
./gh-nibfin --help
./gh-nibfin runner --help
npm run build
npm run check
npm test
```

Build validates JavaScript syntax and the directly usable executable; there is no
bundled or generated distribution. These commands need no network, Core package,
GitHub login or Agent credentials. This is unreleased source, not a published
extension installation or a verified OS/Agent support matrix.

## Commands

These are the complete seven operation leaves. `runner` is a help/navigation
group. Append `--help` to any operation to list its options.

| Operation | Options |
| --- | --- |
| init | --repo OWNER/REPO (required), --auto-register, --check |
| runner create | --name NAME (required), --agent AGENT (required), --count N |
| runner register | --runner NAME (required), exactly one of --repo OWNER/REPO or --org ORG, --group NAME (Org only), --method auto or manual |
| runner unregister | --runner NAME (required), exactly one of --repo OWNER/REPO or --org ORG |
| runner list | --runner NAME, --json |
| runner remove | --runner NAME (required), --yes |
| upgrade | --runner NAME, --repo OWNER/REPO, --check, --version VERSION |

Options take separate arguments; repeated options, extra positional arguments,
unknown options and empty values are rejected. Parsing is noninteractive.
Register defaults to manual. Agent selections are opaque nonempty strings and do
not assert adapter support. An explicit version value must be nonempty; no
floating latest version is selected automatically.

`runner create --count` defaults to 2 and accepts positive safe integers only.
It describes the total initially unregistered instances in one fixed-Agent local
environment: 1 or 4 means one or four total, not a per-Repository allocation.
Instances would have separate local identities and work/registration spaces,
sharing only the environment's Agent login; different environments must isolate
Agent credentials. Parsing creates no environment, instances or Runner IDs.
Register intent selects existing resources and never requests extra instances
when adding a Repo or Org target.

Examples below demonstrate parsed intent, and currently exit 1:

```sh
./gh-nibfin runner create --name workstation --agent codex --count 4
./gh-nibfin runner register --runner workstation --repo OWNER/REPO
./gh-nibfin runner remove --runner workstation --yes
```

Exit 0 means help (or successful internal read-only Core verification); exit 2
means invalid input; exit 1 means unavailable capability or verification failure.
Help goes to stdout, diagnostics to stderr, without arbitrary rejected values or
stack traces. No synthetic test handler can be selected through command arguments
or environment variables. There are no delivery, signing, scheduling or Scope
publication commands.

## Independent Core installation verification

Core owns its installation verifier. The internal `src/core.mjs` consumer accepts
an absolute installed module path and independently reviewed expected identity
from a trusted installer, never CLI arguments or environment configuration. It
calls the actual `verifyInstallation(expected)` export. Missing configuration,
package, verifier or failed verification returns a safe failure. This consumer
is not connected to an operation that could claim initialization success.

The reviewed integration target is the unreleased `@nibfin/core` 0.1.0 from
`taco3064/nibfin` commit `1e617281842cb484a140e45ebed2fcac654bc7b9`, exporting
`@nibfin/core/identity`. No published release is established. Do not use the
installed package's own identity as its expected trust anchor, or copy Core's
verification algorithm into this extension.

For the required real integration check, obtain and review that pinned source
through authorized access, derive an expected JSON record with `name`, `version`
and `contentDigest` from independent reviewed source/artifact evidence, and make
a local package archive with `npm pack --ignore-scripts` in that source checkout.
Supply absolute paths to the resulting archive and expected JSON:

```sh
NIBFIN_TEST_CORE_ARCHIVE=/absolute/path/core.tgz \
NIBFIN_TEST_CORE_EXPECTED=/absolute/path/reviewed-identity.json \
npm run test:integration
```

The tracked test installs the archive offline, with lifecycle scripts disabled,
in a temporary consumer and calls the real export through this extension. It
requires a success and rejection for each wrong identity field. It fails rather
than skips when prerequisites are missing. Any transitive package dependencies
must also be available offline; no registry fallback is permitted. The complete
verification sequence is build, check, test and test:integration. The integration
check was exercised against the reviewed pinned archive: the independent expected
record passed, while wrong name, version and digest were rejected. Core returns
the actual identity on success and throws on mismatch. Local routing and archive
integration tests do not prove real installation/registration E2E or a release.
The test inputs are external prerequisites, not bundled product dependencies.

## Authorization and safety boundaries

Installation verification is distinct from GitHub Runner registration and from
permission to execute a particular Issue. A gh login, Runner ID, label or online
status cannot create an Owner Grant or bypass Core's independent gates. Core in
`nibfin` remains the sole owner of delivery state, authorization, verification and
scheduling. This extension baseline performs no GitHub, shell, service, credential
acquisition, automatic login, signing or delivery-stage operation. Mutating
capabilities need separately delivered approved interfaces before implementation.

Runner removal must eventually permit members to stop local resources without
Owner approval and report failed remote cleanup honestly; this baseline does not
claim to perform removal. Public and private repository trust and Agent credential
isolation remain mandatory. No P1 onboarding or three-repository E2E completion is
claimed.

## Public-facing language

All maintained public-facing information must be English. This includes help,
diagnostics, examples, documentation, workflow descriptions and release/PR text.
The maintained README and existing repository-control prose were reviewed for
English consistency. Existing control workflows and license terms are preserved.

## License

This project is **source-available under a custom restricted license**, not an
OSI-approved open-source project. See [LICENSE](LICENSE) for the binding terms.

- You may download, install, build, and use the **unmodified original** for
  **noncommercial purposes**.
- Modification, derivative works, and redistribution (including unmodified
  copies) require separate prior written permission.
- Commercial use, including internal business use and paid client work,
  requires separate prior written permission.
- Ordinary external configuration and necessary installation/build copies are
  allowed. Your independently created projects and output do not become subject
  to this license merely because you use the software.
- GitHub viewing/forking rights and non-waivable legal exceptions are preserved.
  Share the official repository link instead of redistributing copies.

For additional permissions, contact the maintainer through this repository's
Discussions (if enabled) or an Issue. The English LICENSE text governs.

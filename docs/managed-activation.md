# Native managed Issue dispatch on Linux

Fresh `gh nibfin init --repo OWNER/REPO` uses the native Issue caller on a
compatible managed Linux host. `--apply` uses the selected existing GitHub CLI
login after consent (`--consent-managed-login` supplies explicit consent).
The installation PR still requires ordinary native review and merge.

CLI verifies the selected account, repository identities and effective repository
permissions. Before publishing the caller, it prepares the shared execution
destination while dispatch is disabled, then uses the official `gh secret set`
interface to encrypt and publish `NIBFIN_WAKE_TOKEN`. The credential travels in
private process input and the selected child's private environment. It is never
printed, passed as a command argument or saved in installation intent. Users do
not create, copy or paste a token. If the stored OAuth login needs workflow
authorization, CLI uses `gh auth refresh` for the same active account and validates
the selected identity again. Supported browser/device authorization may require
normal GitHub confirmation; CLI never silently switches accounts.

After the merged caller and the adopted two-slot host are verified, init enables
the native pool and invokes Core's fixed `complete-installed-lifecycle` operation.
It does not discover or queue Issues itself, create a test Issue, install a
notification relay, or require notification journal receipts. Explicit manual
service and Wake options keep their existing behavior.

## Configuration and continuation

`NATIVE_ROUTING_CONFIGURED` reports completed installation and configured routing.
The credential remains `configured_unverified`: secret metadata cannot establish
its plaintext equality, credential validity or a future workflow's successful
operation. The nested continuation is independently `dispatched`, `paused` or
`pending`, according to Core's structured result. `dispatched` does not mean that
an Issue has completed. A paused result preserves the Owner's pause.

A pending continuation exits with status 2 while retaining the completed
installation. Retry init to reobserve the caller and host, reuse the confirmed
configuration and registrations, and invoke the same Core completion operation.
When local intent is missing, init can reuse a complete native installation
after independently observing its selected account, caller, adopted host/member,
Runner registrations, destination and secret configuration. This also works
before the pool activation variable is enabled. It neither reconstructs a secret
write receipt nor claims ownership or equality from metadata alone.
The same observation handles a valid historical intent whose producer selection
has since changed. Intent parsing, private-file and identity checks still apply;
the current complete installation supersedes obsolete bookkeeping.

For an unfinished installation, CLI observes the original PR first, preserves its
selected official source and reuses or resumes normal publication. Historical
metadata never authorizes a root package installation. Once that PR is confirmed
merged, a current normal plan with the same Core/Action source, integrity and
caller configuration can enter its unfinished host stage. Artifact locators are
retrieval metadata; a changed locator alone does not require another PR or block
installation. A genuine caller or source transition belongs to upgrade/migration.
An empty, malformed or unavailable completion response never becomes a claimed
dispatch success. Core owns eligibility and reuse of confirmed dispatch effects.

## Configuration conflicts and uncertain writes

An unrelated existing secret or a different destination is a conflict; init does
not overwrite it. Private installation intent contains only nonsecret operation
observations. A confirmed secret write can be reused while its provider metadata
has not changed. Metadata is used to detect an external change, not to establish
credential equality.

A successful publication response is saved before fetching metadata. If that
later read fails, retry only the observation; do not publish the credential again.
A failed state save is reported rather than claimed durable. An observed missing
or externally changed setting is a configuration change, not an unknown write.

A lost secret-write response remains unfinished. On retry, CLI revalidates the
same private installation intent, selected account, repository, destination and
disabled route, then repeats its authorized setup through GitHub's documented
create-or-update interface. Only the new successful response confirms the write.
A present secret's metadata does not prove who made the earlier write or establish
equality. An unrelated existing secret on first setup remains a conflict, and a
lost intent cannot claim ownership from the secret's name or timestamps. Failed
observations use the same operation's retry path and do not restart registration
or PR publication.

Existing artifact-based installations and peer/upgrade/rebuild migration have
separate migration ownership. Fresh initialization does not silently change an
existing installation's transport or retire shared resources used by another
repository. Existing model/GitHub logins, adopted pool identities and Owner pauses
are preserved.

Official interfaces:

- [GitHub CLI secret setup](https://cli.github.com/manual/gh_secret_set)
- [GitHub CLI authorization refresh](https://cli.github.com/manual/gh_auth_refresh)
- [Repository secret metadata](https://docs.github.com/en/rest/actions/secrets#get-a-repository-secret)

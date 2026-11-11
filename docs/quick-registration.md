# Optional team-member quick registration

Existing Admin onboarding and GitHub's official manual registration remain available.
The optional `init --quick` route registers a repository Runner for an independently
GitHub-authenticated member with current repository write eligibility. It does not
open another installation PR. Finish repository onboarding separately first.
Personal and Organization public repositories are supported by synthetic contracts;
GitHub Apps, private repositories and organization-level Runners are unsupported.
Live provider registration and native Windows execution remain Unverified here.

Management and member commands run in **separate authenticated contexts** outside
Agent workspaces. Management needs Repository Admin and the provider's actual
`Administration: write` permission. Its long-lived credential never crosses to the
member. The member uses their own `gh` login; a supplied login or routing label
cannot replace GitHub authentication. Recheck the repository and the intended
member before issuing anything.

Use absolute paths outside checkouts, private parent directories controlled by
the current user, and a fresh handoff directory per attempt. The member Runner
name is a prefix of at most 31 characters; an unpredictable attempt suffix is
added. Every command previews by default, without token issuance, file creation,
Runner download or configuration. Add `--apply` only for the intended step.

1. **Member:** create a request using their own GitHub identity:

   ```sh
   gh nibfin init --repo OWNER/REPO --quick request \
     --handoff-directory /private/member-attempt \
     --runner-directory /private/new-runner --runner-name my-runner --apply
   ```

   Privately send `request.json` to the Admin. Request metadata contains no secret.
   Do not share `state*.json`; those are local recovery observations.

2. **Admin:** explicitly select the intended member login, verify eligibility,
   obtain official package metadata and request a short-lived registration token:

   ```sh
   gh nibfin init --repo OWNER/REPO --quick issue \
     --handoff-directory /private/admin-attempt \
     --request /private/inbox/request.json --member MEMBER_LOGIN \
     --secret-file /private/outbox/capability.json --apply
   ```

   Transfer the capability through a private, bounded, authenticated channel.
   Retire the sender's temporary copy after delivery or abort. Never paste it in
   Issues, workflow inputs, artifacts, logs, shell arguments or a repository.
   Separately communicate the printed SHA-256 digest through an existing channel
   where the member can independently authenticate the Repository Admin. A digest
   copied from the capability, an accompanying untrusted message or an unverified
   sender **does not establish management origin**. If that independent channel
   is unavailable, stop and use GitHub's official manual registration instead.
   This explicit operator trust input avoids a broker, new account system or
   user-maintained signing keys; the CLI cannot authenticate that external channel.

3. **Member:** use the independently authenticated digest, never the token itself:

   ```sh
   gh nibfin init --repo OWNER/REPO --quick register \
     --handoff-directory /private/member-attempt \
     --secret-file /private/inbox/capability.json \
     --trusted-digest ADMIN_AUTHENTICATED_SHA256 --apply
   ```

   The private regular input file is bounded to 64 KiB and consumed on reading,
   including malformed, expired or rejected inputs. The original provider expiry
   is retained; at least 150 seconds must remain both before package acquisition
   and before the official two-minute configuration call. Downloading may exhaust
   the remaining lifetime. There is no automatic issuance or configuration retry.
   The package and all packaged files are verified without Admin API access from
   the member context. Only the registration token reaches the official Runner
   through its masked environment input. Registration returns **Pending (exit 2)**
   until both management and native local results agree.

4. **Admin:** receive the member's nonsecret `result.json` and independently read
   the repository Runner list and exact Runner ID with management credentials:

   ```sh
   gh nibfin init --repo OWNER/REPO --quick confirm \
     --handoff-directory /private/admin-attempt \
     --result /private/inbox/result.json \
     --confirmation /private/outbox/confirmation.json --apply
   ```

   Transfer the nonsecret confirmation and separately authenticate its printed
   digest using the same management-origin procedure. Recipient metadata alone,
   missing/duplicate results and another member's label cannot confirm a Runner.
   Online/Busy status never selects a different member.

5. **Member:** compare that confirmation with the actual official `.runner`
   settings and verified package on this host:

   ```sh
   gh nibfin init --repo OWNER/REPO --quick reconcile \
     --handoff-directory /private/member-attempt \
     --confirmation /private/inbox/confirmation.json \
     --trusted-digest ADMIN_AUTHENTICATED_CONFIRMATION_SHA256 --apply
   ```

   A registered result still reports `core_binding: unverified`, protected execution
   unverified and host adoption required. No command dispatches a stage, adopts a
   host, establishes a Core binding or grants Merge permission. Existing trusted
   Core checks remain responsible for current membership/binding and revocation.

For interrupted configuration, timeout or unavailable read-back, preserve the
Runner directory and handoff journal. Confirm the official configuration process
has stopped, then export the local observation without reconfiguring:

```sh
gh nibfin init --quick inspect --repo OWNER/REPO \
  --handoff-directory /private/member-attempt --apply
```

Continue with management confirmation and reconciliation. Management confirmation
can be repeated to a **new** output path after a lost result; it only observes
GitHub. Immutable nonsecret journal entries preserve the original attempt when
persistence is interrupted. An incomplete latest entry or stale lock requires
inspection, never rollback to a retryable state. Do not remove Pending records or
unresolved registrations to make another attempt succeed.
After independently confirming the original CLI and official configuration process
have stopped, a stale `operation.lock` may be retired so observation can resume;
keep the journal and Runner intact. An incomplete journal needs administrative
inspection using the retained original request and GitHub's actual registration.

Before configuration, cancel local intent and consume an optional delivered file:

```sh
gh nibfin init --quick cancel --repo OWNER/REPO \
  --handoff-directory /private/member-attempt \
  --secret-file /private/inbox/capability.json --apply
```

Once configuration is Pending, cancellation retains that uncertainty and cannot
revoke a disclosed GitHub token. Tokens are bearer repository-registration
capabilities, normally valid about one hour: they carry no authenticated Nibfin
member identity and grant no stage, host-adoption or Merge authority. GitHub
provides no arbitrary revocation operation for a disclosed registration token.
Remove temporary exchange copies after use; retain only nonsecret recovery state
and the unresolved Runner until official reconciliation/removal makes cleanup safe.

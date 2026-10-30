# N15 read-only producer handoff for G04

This supplies the already delivered observation interface needed by gh-nibfin
[#21](https://github.com/taco3064/gh-nibfin/issues/21). The initial N01 archive is
identity-only. Its three exports do not include N15; consulting that archive
cannot establish the later producer contract. Both historical handoffs remain
available with distinct immutable identities.

## Immutable producer and package

- Producer: [nibfin #98](https://github.com/taco3064/nibfin/issues/98), N15,
  [merged PR #148](https://github.com/taco3064/nibfin/pull/148).
- Commit: `e4e2a835f4236975d41ddb4f4bc5ada7dbae1b30`.
- Accepted and merged tree: `d9a603bce5971c05c6fe54cd31fb77ef3ff023ff`.
- Package: `@nibfin/core@0.1.0`, ESM, Node.js 22.x / npm 10.x.
- `nibfin-core.tgz`: the original accepted 71,426-byte artifact, SHA-256
  `01f6b5fbd01ebbba37ea6723db7bdfff25f5cc44c68e832206974248e5717fef`.
- All 23 archived regular files were checked against Git blob identities in the
  immutable merged tree. `provenance.json` records those identities and raw-byte
  SHA-256 values, plus upstream completion and acceptance references.
- The exact upstream docs/package.md contract remains inside the pinned Core archive. The verifier checks its original hash and byte length after installation; no rewritten duplicate is retained.
- `producer-tests.mjs` and `producer-lock.json` are exact upstream
  `test/runners.test.mjs` and `package-lock.json` bytes at that commit.
- `jose-6.2.13.tgz` is the genuine registry dependency, checked against the
  immutable producer lock's SHA-512 integrity. It enables offline verification
  of the actual installed public package entry, without replacing a dependency.

The unchanged `0.1.0` version string alone cannot distinguish N01 and N15.
Use the commit, archive digest and file identities together. This is an initial
integration handoff, not a published release or a floating main/latest reference.

## Consumer interface and responsibility

Import `observeRunners`, `createGithubProvider`, `githubId`,
`repositoryCoordinates` and `memberRunnerLabel` through the verified package's
public entry. `observeRunners(provider, input)` accepts the public repository
identity, registration level, trusted local instance/Runner/member records,
platform/Label conditions and a safe correlation identifier. The exact object
shapes, pagination/group/access rules and failure diagnostics are defined in the
colocated producer contract and retained tests.

The outcomes are `matched`, `mismatch`, `unavailable` and `unsupported`.
Complete observations return normalized Runner/instance data, including actual
Runner IDs, member Labels, platform, online/offline status and busy state.
Unavailable evidence is not an offline Runner. Labels do not establish identity
or authorization; one matching inventory does not freeze membership. Only GET
requests are performed by this API. The caller provides a trusted provider
transport; this handoff carries no credentials.

N15 observes and reconciles registration; it does **not** issue registration
tokens, invoke `config.sh`, create instances or install services. Those manual
Repo/Org registration/configuration operations are the consumer work owned by
#21. A missing token-issuance export in N15 is not missing producer evidence.
Consume the N15 observations to verify readback after the consumer's authorized
official registration path. Keep target-management diagnostics and short-lived
registration data within the original consumer Scope. Do not invent a producer
registration protocol or duplicate the Core observation implementation.

The current product loader in `src/core.mjs` still pins N01. This handoff only
adds inspectable upstream evidence. G04 Shape must explicitly account for the
reviewed N15 package, its real dependency and loader compatibility; adapting the
loader belongs to normal G04 implementation and independent review. Never pass
N15 bytes to the N01 four-file verifier, silently change old pins, infer broader
capabilities from a package version, or make private upstream a runtime dependency.

## Reproduce the local package check

From the repository root on Linux with Node.js 22.x and npm 10.x:

```sh
node docs/handoffs/n15/verify.mjs
```

The verifier checks reviewed provenance/asset digests, the locked dependency
integrity and every installed producer file. It installs the two exact local
archives offline with scripts disabled in a disposable external directory and
a minimal environment. It then runs the original N15 contract tests through
`@nibfin/core`, changing only their relative import to the public package name.
Those tests use explicit fixture transports; no live registration is performed.

This is supporting producer evidence. It is not a Shape approval, an Acceptance
verdict or authority to register a real Repo/Org Runner. Independent validation,
Candidate/Hooks, exact-head CI, protected merge and the existing #27/#28/#122 live
integration responsibilities remain applicable. The Issue body and native graph
are unchanged. Resume the original blocked Shaper to reassess these exact bytes.

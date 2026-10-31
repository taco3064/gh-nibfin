# Core and platform responsibilities

Core is the authority for workflow and recovery decisions. The maintained native
Host keeps platform access, persistence and process composition:

| Former Host responsibility | Authoritative Core contract | Retained Host adapter |
| --- | --- | --- |
| Stored GitHub authentication, conditional reads and mutation invalidation | `localGhTransport`, `createConditionalGithubTransport` | Native executable choice and response observation |
| Primary/secondary quota deadlines | `githubRateLimit` | Serialized deadline persistence and wakeup |
| Delivery stage catalog and role set | `coreStages`, `modelRoleStages` | Native routing and workflow dispatch |
| Ordinary main integration and repeated Candidate gates | `deliveryBaseRevisionPolicy` | Git preparation, original Writer instruction delivery and journals |
| Protected-stage diagnosis eligibility and bounded pre-AI retry | `protectedStageDiagnosisPlan` | Stored marker, actual quota and active-run observations |
| Diagnosis versus verified resolution | `protectedStageDiagnosisResult` | AI invocation and real diagnostic record persistence |
| Completion receipt stage inventory | Actual Core completion receipt | Native receipt blob storage |
| New Shape grammar/recovery | Core validation and recovery | Finish only already-retained legacy format journals |

`native-rescue-composition.mjs` composes the same Core recovery owners; splitting
it from the dispatcher does not create another planner. Diagnosis alone remains
UNRESOLVED. Original Writer sessions, separate diagnosis attempts and the original
repair budget remain intact. Pending work is never accepted by migration.

The public Core retired the unused development `controller/` implementation.
Its public Git tree and adaptation hashes remain in packaged provenance. The CLI
uses only the verified public package; it does not require that historical tree.

The Action wrapper uses the same version/component identity and derives its stage
catalog from that exact Core. Native workflow dispatch and the Action entry are
different transports. Installed consumer tests do not establish external Caller,
platform, fresh installation or live AI repair acceptance.

## Official CLI publication

The manually dispatched `cli-release.yml` validates the shipped CLI and installed
Core before building a full exact-Git source archive and file manifest. Version
has one source in `src/version.mjs`. Publication requires the matching actual
immutable Core release, protected main and the merged PR identity. It reconciles
existing tag/release/assets, preserves unknown writes and reads back the immutable
release, exact asset digests and tag after publishing. Conflicting bytes fail;
there is no automatic release deletion or retagging.

Official installation uses GitHub CLI's source-extension path pinned to the
immutable published tag, with an independent installed-source readback. Publication
does not itself certify Linux/Windows/WSL or the complete autonomous lifecycle.

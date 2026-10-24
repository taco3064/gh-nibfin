# N01 read-only producer handoff for G01

This is traceable upstream evidence supplied to unblock gh-nibfin #18. It does not change the original Scope, native dependencies, signed development Grant, Skills or acceptance gates. Inspect the colocated producer-contract.md and provenance.json before defining the consumer boundary. The original N01 accepted archive is supplied as nibfin-core.tgz so Shape, Deliver and independent reviewers can inspect the same bytes without producer credentials or cross-repository writes.

## Immutable source and evidence

- Producer: taco3064/nibfin #84 (N01), merged by PR #136.
- Exact merged commit: 0a2e276ad5b37d94b485bbcf8c7288743b9d158f.
- Accepted and merged tree: d99a5bf0f524bce392b1b3b12dbee12006885037.
- Archive: 3980 bytes; raw-byte SHA-256 956b12fb80620e18576212d371989b0448cf39878533b813e8cba838a40c1759.
- Archive entries are exactly package/package.json, package/README.md, package/docs/package.md and package/src/index.mjs. The operator compared every archived file byte-for-byte against the immutable producer commit and checked the accepted artifact's recorded size and digest.
- provenance.json records independent upstream Git blob IDs and raw-byte SHA-256 values. Verify the expected digest from this reviewed handoff before extracting, installing or importing the archive. Runtime authorization and Nibfin signing hashes are different concepts.

## Actual N01 interface

The package is @nibfin/core@0.1.0, ESM, Node.js 22.x/npm 10.x. Import the public package entry after installing the exact archive; exports are githubId, repositoryCoordinates and memberRunnerLabel. N01 provides identity primitives and the archive-consumer boundary only. It has no bin executable, stage executor, CLI main function, registration protocol or complete delivery runtime. Do not invent or claim those surfaces from this baseline. Implement the bounded loader/capability behavior required by the frozen G01 Scope, and report unavailable later capabilities explicitly.

The exact artifact can be installed into a disposable consumer with npm install --offline --ignore-scripts --no-audit --no-fund followed by its filesystem path, then imported as @nibfin/core. Verify identity, approved immutable content and compatibility before any execution. The archive is inert on import and contains no install scripts or runtime dependencies. Keep the archive and contract as upstream evidence; do not duplicate producer implementation or make private Kairo a runtime dependency.

This fixture is an initial local integration handoff, not a formal published release. Immutable product release/distribution and upgrade support remain later owned work. Never silently switch this evidence to the current main branch or floating latest.

The blocked original Shape conversation should be resumed with the updated repository snapshot; its author and the independent validator must re-evaluate the actual evidence. This handoff alone is not a Shape approval, acceptance verdict or delivered marker.

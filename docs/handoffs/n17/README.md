# N17 consumer handoff

This is a consumer-assembled package, not a published producer release. It uses
unmodified package.json and every package.files entry from the controller's
read-only N17 handoff, commit 1dc666ff9a273e59b7cac417408ab1de5c5ff623,
tree 1d0be51e15d84c0ca2d10baae62942c25e281f51. Scope and Shape are respectively
2688d7531e38730118d44e0ff647b693f8cfee6f89b3aadebf1c24adf1906e33 and
a1e0e0a93a7974ee7f07ee7e7498428c11fd8ef3789bc3ad87fd64c0fe916d97.
Handoff 219bb9abbeb58a954a904a94e03f198dc59c7f7fadf1001372ff9d54ac37e28f
contained no separate accepted artifact files. No floating upstream was fetched.

Construction: Python standard-library USTAR, package.json then package.files in
order, package/ prefix, regular files, mode 0644, uid/gid/mtime zero, empty owner
names; gzip with mtime zero. provenance.json retains every exact file size/hash,
archive hash and construction. Installation uses the existing reviewed N15 jose
6.2.13 archive and lock integrity, offline with scripts disabled. N01/N15 pins
remain unchanged. Source/package exports and dependency bytes are checked before
installation; all installed Core bytes are checked before import.

In a verifier-owned disposable copy run `node docs/handoffs/n17/verify.mjs` with
Node 22/npm 10. It installs the real public package, verifies supporting fixture
bytes, and runs all six original producer registration contract tests. The only
adaptation is replacing relative public-entry imports in disposable test/helper
copies with the installed public-entry path. No producer tests or package code
are edited. This proves a consumer contract, not live registration/channel access.
The production CLI additionally tests official dispatch/source validation and
real disposable configuration/service process boundaries in test/register.test.mjs.

Official configurator fixture baseline: actions/runner v2.331.0, commit
39f6c643d3032f8795fa71f1482731ca90c8e038, ConfigurationManager.cs lines
307–354 and 420–444. This Shape-retained source identity records server-returned
AgentId saved into local settings. No official Runner archive was downloaded or
executed by these tests; production checks the documented pinned archive and its
extracted bytes before automatic requests. The commit-bound reference is retained from Shape; no fresh source inspection is claimed.

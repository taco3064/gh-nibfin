> Historical #233 input and observations. The compatible PR246 correction is now
> selected by the [current loader](../current/README.md); the incompatibility
> below describes these retained older bytes, not the current producer.

# Reviewed public runtime input

These are the exact accepted nibfin #233 archive, release manifest and
compatibility bytes supplied with the read-only producer handoff. The producer
merged as `dc69047979f15af9b34d4e835814948caa4b74cf`. The manifest identifies its
pre-merge candidate source honestly; it is not rewritten to claim a published
release. `src/current-core.mjs` anchors all three inputs and imports a fresh
offline installation with lifecycle scripts disabled. The existing N15 dependency
archive supplies the exact shrinkwrapped jose 6.2.13 bytes. Historical N01/N15/N17
consumers and their pins remain separate.

This input and loader are recoverable integration work, not a completed switch of
the production seven-command onboarding. Production initialization, creation,
registration, recovery installation and upgrade still need their Issue #55
integration. No release, platform, model or live-provider certification is claimed.

Run the independent local consumer fixtures through the required verification
lane:

```sh
node /verification-runner.mjs -- node --test test/current/package-consumer.mjs test/current/admission-consumer.mjs
```

The admission fixture uses the unchanged installed shell and Node Hook with
bounded authenticated-provider transport doubles and disposable Worker-named
processes. A valid Repository registration reaches its continuation sentinel.
Changing only the official local registration URL to the Organization-level form
makes admission deny `local-runner`, terminate the Worker and suppress that
sentinel. This confirms an integration blocker, not Org-level support or a passing
#55 admission obligation. It does not exercise official Runner binaries.

The packaged policy requires an `OWNER/REPO` repository. The installed Hook derives
`localRunner.repository` directly from `.runner.gitHubUrl`; Organization-level
registration legitimately stores `https://github.com/ORG`, as also required by the
existing CLI's official configuration readback. No policy configuration can make
these identities equal. The CLI must neither falsify official configuration nor
weaken or fork the producer Hook to hide this incompatibility. A compatible
producer-owned registration-scope/admission contract is needed before enabling
Organization-level instances with this protection. Organization-owned **public
Repository** Runners are a separate dimension and pass the positive fixture.

The [official Runner v2.331.0 configuration source](https://github.com/actions/runner/blob/v2.331.0/src/Runner.Listener/Configuration/ConfigurationManager.cs#L140) assigns the supplied registration URL to GitHubUrl. This is consistent with the retained CLI Org registration/readback contract.

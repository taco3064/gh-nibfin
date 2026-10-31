import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { nativeControlHost } from '../../src/native-host.mjs';
import { nativeCycleFixture } from './native-cycle-fixture.mjs';

test('configured Member reports the actual activation hold', async () => {
  await withCurrentCore(async (core, context) => {
    const fixture = await nativeCycleFixture(core, context);
    const reason = 'native-project-installation-and-activation-pending';
    const observed = {
      status: 'running',
      stageEntry: 'configured',
      diagnostics: [{ capability: 'host', code: reason }],
    };
    const host = nativeControlHost({
      root: fixture.root,
      service: {
        check: async () => observed,
        install: async () => observed,
      },
      verify: async () => {},
      prepare: (path) =>
        fs.writeFile(join(path, 'fixture'), 'application', { mode: 0o600 }),
    });
    const request = {
      core: { ...core, createGithubProvider: () => fixture.provider },
      context,
      executionPool: fixture.configuration,
      memberId: '7',
    };
    await host.prepare(request);
    const result = await host.check(request);
    assert.equal(result.status, 'blocked');
    assert.deepEqual(result.diagnostics, observed.diagnostics);

    observed.diagnostics = [];
    assert.deepEqual((await host.check(request)).diagnostics, [
      { capability: 'activation', code: reason },
    ]);
  });
});

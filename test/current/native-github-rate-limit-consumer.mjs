import test from 'node:test';
import assert from 'node:assert/strict';
import { join } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { nativeCycleFixture } from './native-cycle-fixture.mjs';
import {
  retainNativeGithubRateLimit,
  nativeGithubRateLimit,
} from '../../src/native-github-rate-limit.mjs';
import { nativeDispatchMonitor } from '../../src/native-dispatch-monitor.mjs';
import { initializeFileHostStore } from '../../src/host-store.mjs';
import { writePrivate } from '../../src/local-state.mjs';
test('exhausted quota suppresses observation until reset, without Actions', async () =>
  withCurrentCore(async (core, context) => {
    const f = await nativeCycleFixture(core, context),
      installation = { executionPool: f.configuration };
    await writePrivate(join(f.root, 'native-installation.json'), installation);
    const activation = {
      version: 1,
      phase: 'active',
      installation: core.contentIdentity(installation),
    };
    await writePrivate(join(f.root, 'activation.json'), {
      ...activation,
      identity: core.contentIdentity(activation),
    });
    const host = await initializeFileHostStore(core, {
      directory: join(f.root, 'state'),
      installation: {
        packageVersion: context.identity.packageVersion,
        pins: context.identity.pins,
      },
      repositories: [{ repository: 'example/project', repositoryId: '100' }],
    });
    try {
      const s = await host.read();
      await host.updateWork({
        expected: s.identity,
        work: { ...s.work, paused: false },
      });
    } finally {
      await host.close();
    }
    const now = Date.now(),
      resetAt = Math.floor(now / 1000) * 1000 + 60000;
    await retainNativeGithubRateLimit(core, f.root, {
      status: 403,
      headers: {
        'X-RateLimit-Remaining': '0',
        'X-RateLimit-Reset': String(resetAt / 1000),
        'X-RateLimit-Resource': 'core',
      },
    });
    await retainNativeGithubRateLimit(core, f.root, {
      status: 403,
      headers: {
        'x-ratelimit-remaining': '0',
        'x-ratelimit-reset': String(resetAt / 1000 - 10),
      },
    });
    assert.equal((await nativeGithubRateLimit(f.root)).resetAt, resetAt);
    assert.equal(await nativeGithubRateLimit(f.root, { now: resetAt }), null);
    let reads = 0,
      pools = 0;
    const provider = {
      request: async () => {
        reads++;
        throw Error('unexpected request');
      },
      collection: async () => {
        reads++;
        return { outcome: 'complete', data: [] };
      },
    };
    const coordinator = {
      status: async () => {
        pools++;
        return { host: 'running' };
      },
    };
    const monitor = await nativeDispatchMonitor(core, {
      root: f.root,
      record: installation,
      context,
      provider,
      coordinator,
      intervalMs: 1000,
    });
    try {
      await monitor.settled();
      assert.equal(reads, 0);
      assert.equal(pools, 0);
      await writePrivate(join(f.root, 'github-rate-limit.json'), {
        version: 1,
        resetAt: now - 1,
      });
      monitor.wake();
      await monitor.settled();
      assert.equal(pools, 1);
      assert.equal(reads, 1);
    } finally {
      await monitor.close();
    }
  }));

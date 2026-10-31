import test from 'node:test';
import assert from 'node:assert/strict';
import { nativeGithubTransport } from '../../src/native-github-transport.mjs';
import { withCurrentCore } from '../../src/current-core.mjs';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { nativeStoredGithubTransport } from '../../src/native-github-stored-login.mjs';
test('actual gh receives conditional headers without token overrides', async () =>
  withCurrentCore(async (core, context) => {
    const executable = join(context.root, 'fixture-gh.cjs'),
      log = join(context.root, 'requests.jsonl');
    await fs.writeFile(
      executable,
      '#!' +
        process.execPath +
        '\n' +
        `const fs = require('node:fs'), args = process.argv.slice(2);
if (process.env.GH_TOKEN || process.env.NIBFIN_SECRET) process.exit(9);
if (args[0] === 'auth') process.exit(0);
fs.appendFileSync(${JSON.stringify(log)}, JSON.stringify(args) + '\\n');
if (args.includes('If-None-Match: "fixture"')) {
  process.stdout.write('HTTP/2 304\\r\\nETag: "fixture"\\r\\n\\r\\n');
  process.exitCode = 1;
} else {
  process.stdout.write('HTTP/2 200\\r\\nETag: "fixture"\\r\\n\\r\\n{"id":7}');
}
`,
      { mode: 0o700 },
    );
    const old = process.env.GH_TOKEN,
      secret = process.env.NIBFIN_SECRET;
    process.env.GH_TOKEN = 'untrusted-environment';
    process.env.NIBFIN_SECRET = 'untrusted-environment';
    try {
      const transport = nativeStoredGithubTransport(core, { executable }),
        provider = core.createGithubProvider({ attempts: 1, transport }),
        r = {
          endpoint: '/user',
          target: '7',
          operation: 'read',
          correlation: 'fixture',
        };
      assert.equal((await provider.request(r)).data.id, '7');
      assert.equal((await provider.request(r)).data.id, '7');
      const requests = (await fs.readFile(log, 'utf8'))
        .trim()
        .split('\n')
        .map(JSON.parse);
      assert.ok(requests[1].includes('If-None-Match: "fixture"'));
      assert.equal(transport.metrics().notModified, 1);
    } finally {
      if (old === undefined) delete process.env.GH_TOKEN;
      else process.env.GH_TOKEN = old;
      if (secret === undefined) delete process.env.NIBFIN_SECRET;
      else process.env.NIBFIN_SECRET = secret;
    }
  }));
test('conditional reads contact GitHub; denial never reuses old values', async () =>
  withCurrentCore(async (core) => {
    let phase = 0;
    const sent = [];
    const transport = nativeGithubTransport(core, {
      send: async (r) => {
        sent.push(r);
        return phase++ === 0
          ? { status: 200, headers: { etag: '"first"' }, body: '{"id":7}' }
          : phase === 2
            ? { status: 304, headers: {}, body: '' }
            : { status: 403, headers: {}, body: '{}' };
      },
    });
    const provider = core.createGithubProvider({ attempts: 1, transport }),
      request = {
        endpoint: '/user',
        target: '7',
        operation: 'read',
        correlation: 'fixture',
      };
    assert.equal((await provider.request(request)).data.id, '7');
    assert.equal((await provider.request(request)).data.id, '7');
    assert.equal(sent[1].headers['If-None-Match'], '"first"');
    const denied = await provider.request(request);
    assert.equal(denied.outcome, 'failed');
    assert.equal(denied.diagnostic.kind, 'denied');
    assert.equal(sent.length, 3);
    assert.equal(transport.metrics().notModified, 1);
  }));
test('mutation invalidates in-flight reads; collection metadata stays fresh', async () =>
  withCurrentCore(async (core) => {
    const request = {
        method: 'GET',
        endpoint: '/user',
        identity: 'member',
        headers: { Accept: 'application/json' },
      },
      seen = [];
    let changed = false,
      release,
      waiting;
    const send = async (r) => {
      seen.push(r);
      if (r.method !== 'GET') {
        changed = true;
        return { status: 200, headers: {}, body: '{}' };
      }
      if (waiting) {
        await waiting;
        waiting = null;
        return { status: 304, headers: {}, body: '' };
      }
      return {
        status: 200,
        headers: { etag: changed ? '"new"' : '"old"' },
        body: changed ? 'new' : 'old',
      };
    };
    const transport = nativeGithubTransport(core, { send });
    await transport(request);
    waiting = new Promise((resolve) => (release = resolve));
    const prior = transport(request);
    await Promise.resolve();
    await transport({
      method: 'PATCH',
      endpoint: '/repos/example/project/issues/1',
      headers: {},
    });
    release();
    assert.equal((await prior).body, 'new');
    assert.equal(seen.at(-1).headers['If-None-Match'], undefined);
    const collection = {
      ...request,
      endpoint: '/repos/example/project/issues?per_page=100',
    };
    await transport(collection);
    await transport(collection);
    assert.equal(seen.at(-1).headers['If-None-Match'], undefined);
  }));

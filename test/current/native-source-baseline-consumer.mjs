import test from 'node:test';
import assert from 'node:assert/strict';
import { sha } from '../../src/local-state.mjs';
import { withCurrentCore } from '../../src/current-core.mjs';
import { recoverManagedSourceBaseline } from '../../src/native-source-baseline.mjs';
import {
  nativeSourceBody,
  sourceInstallationIdentity,
} from '../../src/native-source-contract.mjs';

function fixture(core, fault) {
  const repository = 'example/project',
    repositoryId = '100';
  const source = 'a'.repeat(40),
    main = 'b'.repeat(40);
  const member = { id: '42', login: 'owner' };
  const old = new Map([
    [
      '.nibfin/runtime.json',
      JSON.stringify({
        version: 1,
        producer: 'c'.repeat(40),
        package: { coreSha256: 'd'.repeat(64) },
      }),
    ],
    ['.github/workflows/nibfin-stage.yml', 'old reviewed stage'],
  ]);
  const files = Object.fromEntries([...old].map(([name, body]) => [name, sha(body)]));
  const identity = sourceInstallationIdentity(core, {
    repository,
    repositoryId,
    files,
  });
  const desired = {
    files: new Map([
      ['.nibfin/runtime.json', 'new reviewed runtime'],
      ['.github/workflows/nibfin-stage.yml', 'new reviewed stage'],
      ['.nibfin/skills/rescue/SKILL.md', 'new managed file'],
    ]),
  };
  const observations = new Map(
    [...old].map(([name, body]) => [
      name,
      {
        state: 'present',
        bytes: Buffer.from(
          body + (fault === 'user' && name.includes('stage') ? ' user edits' : ''),
        ),
      },
    ]),
  );
  observations.set('.nibfin/skills/rescue/SKILL.md', { state: 'absent' });
  const pr = {
    id: '700',
    number: '7',
    merged: fault !== 'unmerged',
    merged_at: fault === 'unmerged' ? null : '2026-10-31T00:00:00Z',
    merge_commit_sha: source,
    base: { ref: 'main', repo: { id: repositoryId } },
    user: { id: fault === 'foreign' ? '99' : member.id },
    body: nativeSourceBody(identity) + (fault === 'manifest' ? ' changed' : ''),
    html_url: 'https://github.com/example/project/pull/7',
  };
  let reads = 0;
  const get = async (path, options) => {
    reads++;
    if (path === '/git/ref/heads/main')
      return { object: { type: 'commit', sha: main } };
    if (path.startsWith('/commits?')) return [{ sha: source }];
    if (path === '/commits/' + source + '/pulls?per_page=100') return [pr];
    if (path === '/pulls/7') return pr;
    const match = /^\/contents\/(.+)\?ref=([a-f0-9]{40})$/.exec(path);
    assert.ok(match, path);
    assert.equal(match[2], source);
    assert.equal(options.optional, true);
    const name = match[1];
    if (!old.has(name)) {
      if (fault === 'unknown') throw Error('authenticated read unavailable');
      return null;
    }
    const body = old.get(name);
    return {
      type: 'file',
      path: name,
      encoding: 'base64',
      size: Buffer.byteLength(body),
      content: Buffer.from(body).toString('base64'),
    };
  };
  return {
    options: { repository, repositoryId, member, desired, observations, get },
    identity,
    files,
    reads: () => reads,
  };
}

test('fresh source baseline comes from original protected PR and exact files', async () =>
  withCurrentCore(async (core) => {
    const f = fixture(core);
    const baseline = await recoverManagedSourceBaseline(core, f.options);
    assert.equal(baseline.installation, f.identity);
    assert.deepEqual(baseline.files, f.files);
    assert.equal(baseline.kind, 'protected-managed-source-baseline');
    for (const key of ['work', 'grant', 'activation', 'privateKey', 'receipts'])
      assert.equal(Object.hasOwn(baseline, key), false);
  }));

test('foreign author and unmerged source cannot establish an installation', async () =>
  withCurrentCore(async (core) => {
    for (const fault of ['foreign', 'unmerged']) {
      const f = fixture(core, fault);
      await assert.rejects(
        recoverManagedSourceBaseline(core, f.options),
        /protected-installation-pr/,
      );
    }
  }));

test('prior manifest identity must match every historical managed file', async () =>
  withCurrentCore(async (core) => {
    const f = fixture(core, 'manifest');
    await assert.rejects(
      recoverManagedSourceBaseline(core, f.options),
      /installation-manifest/,
    );
  }));

test('user changes after the original installation remain protected', async () =>
  withCurrentCore(async (core) => {
    const f = fixture(core, 'user');
    await assert.rejects(
      recoverManagedSourceBaseline(core, f.options),
      /user-modification-preserved/,
    );
  }));

test('unknown historical reads cannot be interpreted as absent source', async () =>
  withCurrentCore(async (core) => {
    const f = fixture(core, 'unknown');
    await assert.rejects(
      recoverManagedSourceBaseline(core, f.options),
      /authenticated read unavailable/,
    );
  }));

test('an arbitrary runtime file is preserved without historical adoption', async () =>
  withCurrentCore(async (core) => {
    const f = fixture(core);
    f.options.observations.set('.nibfin/runtime.json', {
      state: 'present',
      bytes: Buffer.from('user configuration'),
    });
    assert.equal(await recoverManagedSourceBaseline(core, f.options), null);
    assert.equal(f.reads(), 0);
  }));

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { writePrivate, readPrivate, sha } from '../../src/local-state.mjs';
import { retireInstalledSourceProposal } from '../../src/native-source-history.mjs';
import {
  nativeSourceBody,
  sourceInstallationIdentity,
} from '../../src/native-source-contract.mjs';

async function fixture(core, context) {
  const root = await fs.mkdtemp(join(context.root, 'source-history-'));
  const repository = 'example/project',
    repositoryId = '100';
  const files = { '.nibfin/runtime.json': sha('old managed bytes') };
  const identity = sourceInstallationIdentity(core, {
    repository,
    repositoryId,
    files,
  });
  const proposal = {
    version: 1,
    kind: 'administrative-installation',
    phase: 'installed',
    identity,
    repository,
    repositoryId,
    files,
    commit: 'a'.repeat(40),
    branch: 'nibfin/install-' + identity.slice(0, 24),
    pr: { number: '8', id: '200' },
    mergedCommit: 'b'.repeat(40),
  };
  const pr = {
    id: '200',
    merged: true,
    head: { sha: proposal.commit, ref: proposal.branch, repo: { id: repositoryId } },
    base: { ref: 'main', repo: { id: repositoryId } },
    user: { id: '7' },
    merge_commit_sha: proposal.mergedCommit,
    body: nativeSourceBody(identity),
  };
  const path = join(root, 'managed-source-pr.json');
  await writePrivate(path, proposal);
  return {
    path,
    proposal,
    pr,
    options: {
      root,
      proposal,
      repository,
      repositoryId,
      member: { id: '7' },
      previous: { repository, repositoryId, files },
      get: async (endpoint) => {
        assert.equal(endpoint, '/pulls/8');
        return pr;
      },
    },
  };
}

test('only verified completed source proposals retire to history', async (t) =>
  withCurrentCore(async (core, context) => {
    await t.test(
      'retains exact prior provenance for a later source version',
      async () => {
        const f = await fixture(core, context);
        await retireInstalledSourceProposal(core, f.options);
        assert.equal(await readPrivate(f.path, { missing: true }), null);
        const retained = join(
          f.options.root,
          'managed-source-history',
          f.proposal.identity + '.json',
        );
        assert.deepEqual(await readPrivate(retained), f.proposal);
      },
    );
    for (const [name, change] of Object.entries({
      'unknown write': (f) => {
        f.proposal.phase = 'merge-intent';
      },
      'unmerged PR': (f) => {
        f.pr.merged = false;
      },
      'foreign author': (f) => {
        f.pr.user.id = '8';
      },
      'wrong source': (f) => {
        f.pr.head.sha = 'c'.repeat(40);
      },
      'wrong base': (f) => {
        f.pr.base.ref = 'other';
      },
      'wrong identity body': (f) => {
        f.pr.body = 'different installation';
      },
      'different managed bytes': (f) => {
        f.options.previous = {
          ...f.options.previous,
          files: { '.nibfin/runtime.json': sha('different bytes') },
        };
      },
    }))
      await t.test('preserves the original proposal for ' + name, async () => {
        const f = await fixture(core, context);
        change(f);
        await writePrivate(f.path, f.proposal);
        await assert.rejects(retireInstalledSourceProposal(core, f.options));
        assert.deepEqual(await readPrivate(f.path), f.proposal);
      });
  }));

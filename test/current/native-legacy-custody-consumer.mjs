import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { inspectLegacyNativeCustody } from '../../src/native-legacy-custody.mjs';
import { legacyCustodyFixture } from './native-legacy-fixture.mjs';

test(
  'legacy source and conversation are reconstructed ' + 'without importing config',
  async () =>
    withCurrentCore(async (core, context) => {
      const f = await legacyCustodyFixture(core, context);
      const marker = join(f.directory, 'must-never-run');
      f.put(
        'g55/workspace/repository.git/config',
        '[core]\nfsmonitor = touch ' + marker + '\n',
      );
      f.put(
        'g55/workspace/repository.git/hooks/post-checkout',
        '#!/bin/sh\ntouch ' + marker + '\n',
      );
      const proof = await inspectLegacyNativeCustody(core, f.input);
      const repeated = await inspectLegacyNativeCustody(core, f.input);
      assert.equal(core.contentIdentity(repeated), core.contentIdentity(proof));
      assert.equal(proof.status, 'UNACCEPTED');
      assert.equal(proof.candidate.tree, f.candidate.tree);
      assert.equal(proof.snapshot.session, f.archive.metadata[55].originalSession);
      assert.equal(proof.snapshot.execution, f.unit.delivery.executionId);
      assert.equal(proof.workspaceSnapshot.status, 'UNACCEPTED');
      assert.equal(proof.temporaryProofRemoved, true);
      await assert.rejects(fs.stat(marker), { code: 'ENOENT' });
      const config = proof.workspaceSnapshot.entries.find(
        (entry) => entry.path === '.git/config',
      );
      assert.equal(
        Buffer.from(config.data, 'base64').toString().includes(marker),
        false,
      );
    }),
);

test(
  'coherent archive metadata cannot substitute ' + 'a different Git tree or Writer',
  async () =>
    withCurrentCore(async (core, context) => {
      for (const mutate of [
        (f) => {
          f.candidate.tree = 'b'.repeat(40);
          f.archive.metadata[55].candidateTree = f.candidate.tree;
          f.seal();
        },
        (f) => {
          f.put(
            f.sessionPath,
            JSON.stringify({
              type: 'session_meta',
              payload: { id: 'another-writer', cwd: '/work' },
            }) + '\n',
          );
        },
        (f) => {
          f.unit.repairs = -1;
          f.seal();
        },
        (f) => {
          f.candidate.paths = [];
          f.seal();
        },
        (f) => {
          f.unit.shape.body += 'changed';
          f.seal();
        },
      ]) {
        const f = await legacyCustodyFixture(core, context);
        mutate(f);
        await assert.rejects(inspectLegacyNativeCustody(core, f.input));
      }
    }),
);

test(
  'a live or ambiguous legacy stage cannot be treated ' + 'as finished Writer work',
  async () =>
    withCurrentCore(async (core, context) => {
      for (const operation of ['deliver', 'merge', null]) {
        const f = await legacyCustodyFixture(core, context);
        f.payload.pending.operation = operation;
        f.seal();
        await assert.rejects(
          inspectLegacyNativeCustody(core, f.input),
          /unaccepted-boundary/,
        );
      }
    }),
);

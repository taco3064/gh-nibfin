import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { inspectLegacyNativeCustody } from '../../src/native-legacy-custody.mjs';
import { prepareNativeCycle } from '../../src/native-cycle-policy.mjs';
import { verifyNativeLegacyHandover } from '../../src/native-legacy-handover.mjs';
import { prepareDeferredNativeRole } from '../../src/native-deferred-role.mjs';
import { resumeNativeSession } from '../../src/native-session-resume.mjs';
import { legacyCustodyFixture } from './native-legacy-fixture.mjs';
import { nativeCycleFixture } from './native-cycle-fixture.mjs';
import { initializeFileHostStore } from '../../src/host-store.mjs';
import { executeNativeDelivery } from '../../src/native-delivery.mjs';
import { execFileSync } from 'node:child_process';

test('shaped legacy work retains its Shape and resumes the same Writer', async () =>
  withCurrentCore(async (core, context) => {
    const f = await nativeCycleFixture(core, context);
    const old = await legacyCustodyFixture(core, context);
    const original = await inspectLegacyNativeCustody(core, old.input);
    f.issue.labels.push({ id: '901', name: 'shaped' });
    f.setTree(['docs', 'a.txt']);
    const originalBody = f.issue.body;
    const check = await prepareNativeCycle(core, {
      ...f.options,
      handover: original,
      checkOnly: true,
    });
    assert.equal(check.next, 'deliver');
    await assert.rejects(fs.stat(join(f.root, 'delivery', '100', '123')), {
      code: 'ENOENT',
    });
    const store = await prepareNativeCycle(core, {
      ...f.options,
      handover: original,
    });
    const state = await store.read();
    assert.equal(f.issue.body, originalBody);
    assert.deepEqual(Object.keys(state.receipts), [
      'shape',
      'shape-validator',
      'publication',
    ]);
    for (const receipt of Object.values(state.receipts)) {
      assert.ok(receipt.retained);
      assert.equal(receipt.execution.source, null);
    }
    assert.equal(
      state.receipts.shape.execution.id,
      original.shape.author.executionId,
    );
    assert.equal(state.candidate, null);
    assert.equal(state.deferredRoles.deliver.phase, 'prepared');
    const proof = await verifyNativeLegacyHandover(core, {
      root: f.root,
      store,
      state,
    });
    assert.equal(proof.plan.session, original.session);
    const binding = {
      stage: 'deliver',
      issue: '55',
      memberId: '7',
      request: {
        id: 'normal-writer-dispatch',
        repository: 'example/project',
        repositoryId: '100',
      },
    };
    const continued = await prepareDeferredNativeRole(core, {
      root: f.root,
      store,
      binding,
      memberId: '7',
    });
    assert.equal(continued.handoverId, proof.plan.identity);
    assert.equal(continued.caseId, undefined, 'Owner handover is not AI Rescue');
    assert.equal(continued.next.session, original.session);
    assert.notEqual(continued.next.execution, original.execution);
    const directory = await fs.mkdtemp(join(context.root, 'restored-old-writer-'));
    const session = await resumeNativeSession(core, {
      store,
      role: 'deliver',
      memberId: '7',
      directory,
      request: binding.request.id,
    });
    assert.equal(session.id, original.session);
    const after = await store.read();
    assert.equal(after.candidate, null);
    assert.equal(core.nextDeliveryStage(after.receipts), 'deliver');
    assert.equal(after.sessionResumes.deliver.status, 'UNACCEPTED');
    assert.equal(after.sessionResumes.deliver.plan.originalRepairCount, 0);
    await prepareDeferredNativeRole(core, {
      root: f.root,
      store,
      binding,
      memberId: '7',
    });
    assert.equal(
      (await store.read()).sessionResumes.deliver.next.execution,
      continued.next.execution,
      'an armed handover is never replayed',
    );
    const host = await initializeFileHostStore(core, {
      directory: join(f.root, 'state'),
      installation: {
        packageVersion: context.identity.packageVersion,
        pins: context.identity.pins,
      },
      repositories: [{ repository: 'example/project', repositoryId: '100' }],
    });
    const before = await host.read();
    await host.updateWork({
      expected: before.identity,
      work: { ...before.work, paused: false },
    });
    await host.close();
    const sourceDirectory = join(f.root, 'source-observation');
    await fs.mkdir(sourceDirectory, { mode: 0o700 });
    execFileSync('/usr/bin/git', ['init', '--template='], { cwd: sourceDirectory });
    await fs.writeFile(join(sourceDirectory, 'README.md'), 'source');
    execFileSync('/usr/bin/git', ['add', 'README.md'], { cwd: sourceDirectory });
    const prepared = [];
    const result = await executeNativeDelivery(core, {
      root: f.root,
      context,
      provider: f.provider,
      transport: f.transport,
      progress: async () => {},
      binding: {
        ...binding,
        configuration: f.configuration,
        ownerType: 'User',
        correlation: binding.request.id,
        sourceCommit: 'b'.repeat(40),
        request: { ...binding.request, runId: '4000', attempt: '1' },
        jobRecord: { runnerId: '999' },
      },
      roleFactory: async () => ({
        sourceDirectory,
        candidateDirectory: sourceDirectory,
        workspace: () => sourceDirectory,
        get: () => {
          throw Error('not a new role');
        },
        prepareRole: async (task) => {
          prepared.push(task.role);
          assert.equal(task.task.scopeBody, originalBody);
          throw Error('test:stop-before-model');
        },
        close: async () => {},
      }),
    });
    assert.deepEqual(prepared, ['deliver'], JSON.stringify(result));
    assert.equal(result.current.stage, 'deliver');
    assert.notEqual(result.disposition, 'completed');
    assert.equal(f.issue.body, originalBody);
  }));

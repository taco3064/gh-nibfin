import test from 'node:test';
import assert from 'node:assert/strict';
import { join } from 'node:path';
import fs from 'node:fs/promises';
import { randomUUID } from 'node:crypto';
import { withCurrentCore } from '../../src/current-core.mjs';
import { nativeCycleFixture } from './native-cycle-fixture.mjs';
import { prepareNativeCycle } from '../../src/native-cycle-policy.mjs';
import { nativeShapeFeedback } from '../../src/native-shape-feedback.mjs';
import { initializeFileHostStore } from '../../src/host-store.mjs';

test('current Core accepts applicability syntax without Host format revision', async () =>
  withCurrentCore(async (core, context) => {
    const f = await nativeCycleFixture(core, context);
    const store = await prepareNativeCycle(core, f.options);
    const original = await store.read();
    const shape =
      'Tracked Markdown documentation.\n```nibfin-candidate\n' +
      '{"artifacts":[],"hooks":[]}\n```\n' +
      '`artifacts` is empty because Scope needs no extra files.\n' +
      '`hooks` is empty based on the inspected repository setup.';
    const body = core.formatRequirementsDocument({
      scope: original.scopeBody,
      shape,
    });
    assert.equal(core.parseCandidateObligations(body).ok, true);
    const result = await nativeShapeFeedback(core, {
      root: f.root,
      store,
      claim: { stage: 'shape-validator' },
      provider: f.provider,
    });
    assert.equal(result.outcome, 'not-applicable');
    assert.deepEqual(await store.read(), original);
  }));

test('legacy journal completes once and preserves unrelated work', async () =>
  withCurrentCore(async (core, context) => {
    const f = await nativeCycleFixture(core, context);
    const store = await prepareNativeCycle(core, f.options);
    const original = await store.read();
    const request = 'native-11111111-1111-4111-8111-111111111111';
    const reference = await store.retain({
      kind: 'native-shape-format-feedback',
      scopeHash: original.scopeHash,
      cycle: original.cycle,
      source: { request },
    });
    const marker = {
      id: core.contentIdentity({ reference }),
      reference,
      phase: 'prepared',
    };
    const sessions = {};
    for (const role of ['shape', 'shape-validator']) {
      const directory = join(context.root, 'legacy-session-' + role);
      const id = randomUUID(),
        execution = 'legacy-' + role;
      await fs.mkdir(directory, { mode: 0o700 });
      await fs.writeFile(
        join(directory, id + '.jsonl'),
        JSON.stringify({ type: 'session_meta', payload: { id } }) + '\n',
        { mode: 0o600 },
      );
      const snapshot = core.captureRescueSession({
        directory,
        memberId: '7',
        role,
        session: id,
        execution,
      });
      const record = {
        kind: 'native-role-session',
        metadata: { id, execution, role, memberId: '7', adapter: 'codex' },
        snapshot,
      };
      sessions[role] = { reference: await store.retain(record), record };
    }
    await store.update((state) => {
      state.shapeRevision = marker;
      state.shapeRevisions = [{ id: marker.id, reference }];
      state.sessionResumes = Object.fromEntries(
        Object.entries(sessions).map(([role, session]) => [
          role,
          {
            status: 'UNACCEPTED',
            role,
            memberId: '7',
            scopeHash: original.scopeHash,
            cycle: original.cycle,
            caseId: marker.id,
            reference: session.reference,
          },
        ]),
      );
      return state;
    });
    const host = await initializeFileHostStore(core, {
      directory: join(f.root, 'state'),
      installation: {
        packageVersion: context.identity.packageVersion,
        pins: context.identity.pins,
      },
      repositories: [{ repository: 'example/project', repositoryId: '100' }],
    });
    const foreign = {
      kind: 'native-stage',
      cycle: 'foreign',
      request: 'other-work',
      stage: 'deliver',
      status: 'UNACCEPTED',
    };
    try {
      const before = await host.read();
      await host.updateWork({
        expected: before.identity,
        work: {
          ...before.work,
          paused: false,
          reconciled: false,
          pending: [
            foreign,
            {
              kind: 'native-stage',
              cycle: original.cycle,
              request,
              stage: 'shape-validator',
              status: 'UNACCEPTED',
            },
          ],
        },
      });
      const options = { root: f.root, store, claim: {}, provider: f.provider };
      const scope = f.issue.body;
      f.issue.body += '\nChanged Scope';
      await assert.rejects(nativeShapeFeedback(core, options), /current-scope/);
      assert.equal((await store.read()).shapeRevision.phase, 'prepared');
      f.issue.body = scope;
      const result = await nativeShapeFeedback(core, options);
      assert.equal(result.id, marker.id);
      assert.equal((await store.read()).shapeRevision.phase, 'applied');
      assert.deepEqual((await host.read()).work.pending, [foreign]);
      assert.equal(
        (await nativeShapeFeedback(core, options)).outcome,
        'not-applicable',
      );
      await store.update((state) => {
        state.shapeRevision.phase = 'prepared';
        return state;
      });
      assert.equal((await nativeShapeFeedback(core, options)).id, marker.id);
      assert.deepEqual((await host.read()).work.pending, [foreign]);
      assert.equal((await store.read()).shapeRevisions.length, 1);
      for (const [role, session] of Object.entries(sessions)) {
        assert.equal(
          (await store.read()).sessionResumes[role].reference,
          session.reference,
        );
        assert.deepEqual(await store.get(session.reference), session.record);
      }
    } finally {
      await host.close();
    }
  }));

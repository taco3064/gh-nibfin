// Explicit synthetic stopped-runtime boundary, real retained Core receipts and
// current Owner signatures. This does not certify a live OS/runtime switch.
import assert from 'node:assert/strict';
import { join } from 'node:path';
import { openNativeCycle } from '../../src/native-delivery-store.mjs';
import { openFileHostStore } from '../../src/host-store.mjs';
import { nativeOwnerSigner } from '../../src/native-owner-policy.mjs';
import { readPrivate, writePrivate } from '../../src/local-state.mjs';
import {
  continuationRepairContext,
} from '../../src/native-continuation-repair-proof.mjs';
import {
  authorizeNativeContinuationRepair,
  nativeStageRetry,
} from '../../src/native-continuation-repair.mjs';

export async function retainedRuntimeRepairFixture(
  core,
  { root, archive, target, configuration, provider, sourceCommit },
) {
  const store = await openNativeCycle(core, { root, target });
  const before = await store.read();
  const signer = await nativeOwnerSigner(core, { root });
  const old = before.continuation;
  const previousExecution = { ...old.execution, core: 'e'.repeat(64) };
  const previousContext = await continuationRepairContext(core, {
    root,
    parent: old.parent,
    execution: previousExecution,
  });
  const approval = signer.sign(
    core.prepareMergedContinuation(old.parent, previousExecution).draft,
  );
  const checked = core.verifyMergedContinuation(approval, {
    parent: old.parent,
    execution: previousExecution,
    context: previousContext,
  });
  const final = structuredClone(before.receipts.final);
  final.execution.continuation = checked.execution;
  const request = 'fixture-failed-closure';
  const source = { ...final.execution.source, runId: '9000' };
  const failure = {
    target,
    cycle: before.cycle,
    disposition: 'blocked',
    executed: 'closure',
    request,
    run: { id: '9000', attempt: '1' },
    inputs: { continuation: checked.identity, binding: checked.binding },
    owner: {
      outcome: 'blocked',
      output: {
        outcome: 'blocked',
        finding: 'completion:events',
      },
    },
  };
  const reference = await store.retain({
    version: 1,
    kind: 'native-core-result',
    result: failure,
  });
  const stopped = await store.update((state) => {
    state.continuation = { ...old, execution: previousExecution, approval };
    state.receipts.final = final;
    state.expectations.final.expected = structuredClone(final);
    state.attempts.closure = {
      request,
      source,
      phase: 'unresolved',
      reason: 'owner-result',
    };
    state.dispatch = { id: request, stage: 'closure' };
    const { baseRef, ...completionBinding } = state.mergeIntent.binding;
    const intent = (kind) =>
      core.controlledIntent({
        version: 1,
        kind,
        correlation: kind + '-' + state.cycle,
        source: {
          request,
          event: 'workflow_dispatch',
          repositoryId: target.repositoryId,
          runId: '9000',
          attempt: '1',
          actorId: '7',
        },
        binding: { ...completionBinding, runId: '9000', attempt: '1' },
        target: { id: target.databaseId, number: target.number, workflow: null },
        payload:
          kind === 'close'
            ? { state: 'closed', state_reason: 'completed' }
            : { labels: ['delivered'] },
      });
    state.completionPlan = {
      decision: core.contentIdentity({
        cycle: state.cycle,
        final,
        writerId: state.member.id,
      }),
      intents: { close: intent('close'), label: intent('label') },
    };
    return state;
  });
  const host = await openFileHostStore(core, { directory: join(root, 'state') });
  try {
    const priorHost = await host.read();
    const work = {
      ...priorHost.work,
      active: false,
      terminated: true,
      reconciled: false,
      pending: [
        {
          kind: 'native-stage',
          request,
          cycle: stopped.cycle,
          stage: 'closure',
          status: 'UNACCEPTED',
        },
      ],
      receipts: [
        { kind: 'native-cycle', cycle: stopped.cycle, stage: 'closure', reference },
      ],
    };
    const protectedWork = { ...work, paused: true, reconciled: true };
    await host.updateWork({ expected: priorHost.identity, work: protectedWork });
    const activation = await readPrivate(join(root, 'activation.json'));
    const owner = await readPrivate(join(root, 'owner-policy.json'));
    const body = {
      version: 1,
      phase: 'activated',
      originalState: stopped,
      originalWork: work,
      protectedWork,
      owner: owner.identity,
      archive: old.parent.archive,
      directory: archive,
      nextActivation: activation.identity,
      next: activation.installation,
      authorizationHost: (await host.read()).identity,
    };
    await writePrivate(join(root, 'runtime-repair.json'), {
      ...body,
      identity: core.contentIdentity(body),
    });
    const run = (stage, source, request, conclusion) => ({
      id: source.runId,
      run_attempt: 1,
      repository: { id: target.repositoryId },
      head_repository: { id: target.repositoryId },
      actor: { id: '7' },
      triggering_actor: { id: '7' },
      event: 'workflow_dispatch',
      status: 'completed',
      conclusion,
      head_sha: sourceCommit,
      path: '.github/workflows/nibfin-stage.yml',
      created_at: new Date(Date.now() - 10000).toISOString(),
      display_title: core.actionRunName({
        issue: target.number,
        stage,
        correlation: request,
      }),
    });
    const reads = {
      ...provider,
      request: async (input) => {
        if (input.endpoint.endsWith('/actions/runs/9000'))
          return {
            outcome: 'read',
            data: run('closure', source, request, 'failure'),
          };
        if (input.endpoint.endsWith('/actions/runs/' + final.execution.source.runId))
          return {
            outcome: 'read',
            data: run(
              'final',
              final.execution.source,
              final.execution.request,
              'success',
            ),
          };
        return provider.request(input);
      },
    };
    const options = {
      root,
      configuration,
      repository: target.repository,
      directory: archive,
      provider: reads,
    };
    for (const [endpoint, change] of [
      ['/user', (data) => { data.id = '999'; }],
      ['/issues/' + target.number, (data) => { data.body += '\nchanged'; }],
      ['/actions/runs/9000', (data) => { data.conclusion = 'success'; }],
    ]) {
      const guarded = { ...reads, request: async (input) => {
        const result = structuredClone(await reads.request(input));
        if (input.endpoint.endsWith(endpoint)) change(result.data);
        return result;
      } };
      await assert.rejects(authorizeNativeContinuationRepair(core, {
        ...options, provider: guarded,
      }), /native-continuation-repair:/);
      assert.equal((await store.read()).identity, stopped.identity);
      assert.deepEqual((await host.read()).work, protectedWork);
    }
    const check = await authorizeNativeContinuationRepair(core, {
      ...options,
      checkOnly: true,
    });
    assert.equal(check.effects, 'none');
    assert.equal((await store.read()).identity, stopped.identity);
    const result = await authorizeNativeContinuationRepair(core, options);
    assert.equal(result.outcome, 'repair-authorized');
    const ready = await store.read();
    assert.deepEqual(ready.receipts.final, stopped.receipts.final);
    assert.deepEqual(ready.attempts.closure, stopped.attempts.closure);
    assert.deepEqual(ready.completionPlan, stopped.completionPlan);
    assert.equal(
      await nativeStageRetry(core, {
        root,
        store,
        state: ready,
        stage: 'closure',
      }),
      request,
    );
    for (const mutate of [
      (s) => {
        s.runtimeRepair.phase = 'claimed';
      },
      (s) => {
        s.attempts.closure.request = 'different';
      },
      (s) => {
        s.receipts.final.execution.id = 'rewritten';
      },
    ]) {
      const changed = structuredClone(ready);
      mutate(changed);
      await assert.rejects(
        nativeStageRetry(core, {
          root,
          store,
          state: changed,
          stage: 'closure',
        }),
        /native-continuation-repair:/,
      );
    }
    assert.deepEqual((await host.read()).work.pending, work.pending);
    const raw = await store.get(ready.runtimeRepair.original);
    assert.deepEqual(raw.state, stopped);
    assert.deepEqual(raw.work, work);
    // An interruption between cycle publication and unpause resumes the exact
    // prepared state and approval rather than generating another approval.
    const h = await host.read();
    await host.updateWork({ expected: h.identity, work: protectedWork });
    // An explicit later Owner pause has a different CAS identity and must not
    // be mistaken for the installer's interrupted pause.
    await assert.rejects(authorizeNativeContinuationRepair(core, options),
      /owner-state-intervened/);
    const paused = await host.read();
    const interrupted = await readPrivate(join(root, 'runtime-repair.json'), {
      limit: 134217728,
    });
    const { identity: oldJournal, ...journalBody } = interrupted;
    journalBody.releaseExpected = paused.identity;
    await writePrivate(join(root, 'runtime-repair.json'), {
      ...journalBody, identity: core.contentIdentity(journalBody),
    });
    const repeated = await authorizeNativeContinuationRepair(core, options);
    assert.equal(repeated.approval, result.approval);
    assert.equal((await store.read()).identity, ready.identity);
    return { stopped, request };
  } finally {
    await host.close();
  }
}

import { join } from 'node:path';
import { maintenanceLock } from './upgrade.mjs';
import { openFileHostStore } from './host-store.mjs';
import { openNativeCycle } from './native-delivery-store.mjs';
import { nativeOwnerSigner } from './native-owner-policy.mjs';
import { readPrivate, writePrivate } from './local-state.mjs';
import { currentPin, currentProducer } from './current-core.mjs';
import { readNativeMergedContinuation } from './native-handover-context.mjs';
import {
  readNativeRuntimeRepair,
  requireRepairWork,
} from './native-runtime-repair.mjs';
import { observeContinuationRepair } from './native-continuation-repair-proof.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-continuation-repair:' + reason);
};
const same = (core, a, b) => core.canonicalJSON(a) === core.canonicalJSON(b);

/** Official init signs only after all project sources and the actual new
 * activation are verified. A failed retry cannot consume this approval twice.
 */
export async function authorizeNativeContinuationRepair(
  core,
  { root, configuration, repository, directory, provider, checkOnly },
) {
  const initial = await readNativeRuntimeRepair(core, root);
  if (
    !initial ||
    initial.originalState.target.repository !== repository ||
    !['activated', 'completed'].includes(initial.phase)
  )
    return null;
  return maintenanceLock(root, async (assertLease) => {
    let journal = await readNativeRuntimeRepair(core, root);
    need(
      ['activated', 'completed'].includes(journal.phase) &&
        journal.directory === directory,
      'original-repair-request',
    );
    const host = await openFileHostStore(core, { directory: join(root, 'state') });
    try {
      const hostState = await host.read();
      if (journal.phase === 'completed' && !hostState.work.paused) return null;
      requireRepairWork(core, hostState.work, journal);
      need(
        hostState.identity ===
          (journal.phase === 'completed'
            ? journal.releaseExpected
            : journal.authorizationHost),
        'owner-state-intervened',
      );
      const activation = await readPrivate(join(root, 'activation.json'));
      need(
        activation.identity === journal.nextActivation &&
          activation.phase === 'active',
        'actual-new-activation',
      );
      const resumed = await openNativeCycle(core, {
        root,
        target: journal.originalState.target,
      });
      const currentState = await resumed.read();
      if (journal.preparedState === currentState.identity) {
        await readNativeMergedContinuation(core, {
          root,
          store: resumed,
          state: currentState,
        });
        if (checkOnly)
          return {
            outcome: 'repair-verified-not-authorized',
            target: currentState.target,
            next: 'closure',
            effects: 'none',
          };
        const complete = {
          ...journal,
          phase: 'completed',
          releaseExpected: hostState.identity,
        };
        delete complete.identity;
        await writePrivate(join(root, 'runtime-repair.json'), {
          ...complete,
          identity: core.contentIdentity(complete),
        });
        await host.updateWork({
          expected: hostState.identity,
          work: { ...hostState.work, paused: false },
        });
        return {
          outcome: 'repair-authorized',
          target: currentState.target,
          approval: core.contentIdentity(journal.approval),
          next: 'closure',
          effects: 'execution-compatibility-only',
        };
      }
      need(journal.phase === 'activated', 'prepared-cycle-drift');
      const old = journal.originalState.continuation;
      const execution = journal.execution ?? {
        ...old.execution,
        core: currentPin.coreSha256,
        source: currentProducer,
        activation: activation.identity,
        validFrom: Date.now() - 1000,
      };
      const proof = await observeContinuationRepair(core, {
        root,
        directory,
        provider,
        configuration,
        execution,
      });
      need(
        proof.state.identity === journal.originalState.identity &&
          proof.archive.identity === journal.archive,
        'stopped-state-drift',
      );
      if (checkOnly)
        return {
          outcome: 'repair-verified-not-authorized',
          target: proof.state.target,
          next: 'closure',
          effects: 'none',
        };
      const signer = await nativeOwnerSigner(core, { root });
      const approval = journal.approval ?? signer.sign(proof.draft);
      core.verifyMergedContinuation(approval, { ...proof, context: proof.context });
      const { identity, ...body } = journal;
      journal = { ...body, execution, approval };
      await writePrivate(join(root, 'runtime-repair.json'), {
        ...journal,
        identity: core.contentIdentity(journal),
      });
      assertLease();
      const raw = await proof.store.retain(
        {
          version: 1,
          kind: 'native-runtime-repair-original',
          state: journal.originalState,
          work: journal.originalWork,
        },
        { detached: true },
      );
      const transform = (state) => {
        need(state.identity === journal.originalState.identity, 'cycle-drift');
        state.records[raw.slice(14)] = 'native-runtime-repair-original';
        state.continuation = {
          ...state.continuation,
          execution,
          approval,
          repair: proof.repair,
        };
        state.runtimeRepair = {
          version: 1,
          approval: core.contentIdentity(approval),
          original: raw,
          stage: 'closure',
          phase: 'approved',
        };
        // The exact dispatch and failed attempt remain in the retained raw
        // state. The old attempt also remains active until the one-shot claim.
        state.dispatch = null;
        return state;
      };
      const { identity: oldStateId, ...preparedBody } = transform(
        structuredClone(journal.originalState),
      );
      preparedBody.revision++;
      journal.preparedState = core.contentIdentity(preparedBody);
      await writePrivate(join(root, 'runtime-repair.json'), {
        ...journal,
        identity: core.contentIdentity(journal),
      });
      const next = await proof.store.update(transform);
      need(next.identity === journal.preparedState, 'publication-readback');
      await readNativeMergedContinuation(core, {
        root,
        store: proof.store,
        state: next,
      });
      const freshHost = await host.read();
      requireRepairWork(core, freshHost.work, journal);
      need(freshHost.identity === hostState.identity, 'owner-state-intervened');
      const complete = {
        ...journal,
        phase: 'completed',
        result: { approval: core.contentIdentity(approval), state: next.identity },
        releaseExpected: freshHost.identity,
      };
      await writePrivate(join(root, 'runtime-repair.json'), {
        ...complete,
        identity: core.contentIdentity(complete),
      });
      await host.updateWork({
        expected: freshHost.identity,
        work: { ...freshHost.work, paused: false },
      });
      return {
        outcome: 'repair-authorized',
        target: next.target,
        approval: core.contentIdentity(approval),
        next: 'closure',
        effects: 'execution-compatibility-only',
      };
    } finally {
      await host.close();
    }
  });
}

export async function nativeStageRetry(core, { root, store, state, stage }) {
  if (!state.attempts[stage]) return null;
  need(
    stage === 'closure' &&
      state.runtimeRepair?.phase === 'approved' &&
      state.runtimeRepair.stage === stage &&
      state.runtimeRepair.approval ===
        core.contentIdentity(state.continuation?.approval),
    'original-stage-reconciliation-required',
  );
  const checked = await readNativeMergedContinuation(core, { root, store, state });
  need(
    checked.repair &&
      same(core, checked.repair.retained.attempt, state.attempts[stage]) &&
      same(core, checked.repair.retained.receipts, state.receipts) &&
      same(core, checked.repair.retained.expectations, state.expectations),
    'original-attempt-drift',
  );
  return state.attempts[stage].request;
}

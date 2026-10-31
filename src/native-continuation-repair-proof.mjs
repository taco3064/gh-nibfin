import fs from 'node:fs/promises';
import { join } from 'node:path';
import { readPrivate } from './local-state.mjs';
import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { openNativeCycle, nativeCycleDirectory } from './native-delivery-store.mjs';
import { openNativeHandoverArchive } from './native-handover-archive.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-continuation-repair:' + reason);
};
const same = (core, a, b) => core.canonicalJSON(a) === core.canonicalJSON(b);

export async function continuationRepairContext(core, { root, parent, execution }) {
  const owner = await readNativeOwnerPolicy(core, { root });
  need(
    owner.memberId === parent.memberId && owner.keyId === execution.keyId,
    'current-owner',
  );
  const { draft } = core.prepareMergedContinuation(parent, execution);
  const permission = draft.permissions[0];
  return {
    now: Date.now(),
    trustedKeys: new Map([[owner.keyId, owner.publicKey]]),
    revocation: {
      id: owner.keyId,
      available: true,
      revokedRecords: owner.revokedRecords ?? [],
    },
    expectedAuthority: core.authoritySnapshot(parent.authority),
    expectedPolicy: { ...parent.policy, core: execution.core },
    expectedWork: draft.work,
    request: {
      repository: draft.work[0].repository,
      issueId: parent.target.issueId,
      number: parent.target.number,
      path: permission.paths[0],
      operation: 'verify-deployment',
    },
  };
}

/** Read-only proof, before any pause or runtime switch. Only the original
 * signed continuation and its exact failed closure can enter this boundary.
 */
export async function observeContinuationRepair(
  core,
  { root, directory, provider, configuration, execution },
) {
  need(core.distributionAPI?.mergedContinuationRepair === 1, 'compatible-core');
  const archive = await openNativeHandoverArchive(core, {
    directory,
    hostRoot: root,
  });
  const owner = await readNativeOwnerPolicy(core, { root });
  const candidates = [];
  for (const path of archive.candidates) {
    const original = await archive.readCycle(path);
    let store;
    try {
      store = await openNativeCycle(core, { root, target: original.state.target });
    } catch (error) {
      if (error.code === 'ENOENT') continue;
      throw error;
    }
    const state = await store.read();
    if (!state.continuation || state.completion) continue;
    need(
      state.continuation.parent.archive === archive.identity &&
        state.continuation.parent.state === original.state.identity,
      'original-archive',
    );
    candidates.push({ store, state });
  }
  need(candidates.length === 1, 'one-stopped-cycle-required');
  const { store, state } = candidates[0];
  const previous = state.continuation;
  need(
    core.nextDeliveryStage(state.receipts, null) === 'closure' &&
      !state.receipts.closure &&
      !state.closed &&
      !state.completion &&
      state.attempts.closure?.phase === 'unresolved',
    'closure-boundary',
  );
  const target = configuration.targets.find(
    (t) =>
      same(core, t.repositoryId, state.target.repositoryId) &&
      t.repository === state.target.repository,
  );
  need(target && state.member.id === owner.memberId, 'pool-target');
  const get = async (endpoint) => {
    const result = await provider.request({
      endpoint,
      target: target.repositoryId,
      operation: 'native-repair-read',
      correlation: state.cycle,
      signal: AbortSignal.timeout(30000),
    });
    need(result.outcome === 'read', 'authenticated-observation');
    return result.data;
  };
  const base = '/repos/' + target.repository;
  const account = await get('/user');
  const repo = await get(base);
  const issue = await get(base + '/issues/' + state.target.number);
  need(
    account.id === owner.memberId &&
      account.login === owner.memberLogin &&
      repo.id === target.repositoryId &&
      repo.owner.id === target.ownerId &&
      repo.owner.type === target.ownerType &&
      repo.private === false &&
      repo.visibility === 'public' &&
      repo.permissions?.admin === true,
    'authenticated-owner',
  );
  need(
    issue.id === state.target.databaseId &&
      issue.node_id === state.target.issueId &&
      issue.state === 'open' &&
      issue.body === state.shapeBody &&
      !issue.pull_request &&
      issue.assignees.some((a) => a.id === owner.memberId) &&
      ['scope', 'shaped'].every((name) =>
        issue.labels.some((l) => l.name === name),
      ) &&
      !issue.labels.some((l) => l.name === 'delivered'),
    'live-requirements',
  );
  const source = state.attempts.closure.source;
  const finalSource = state.receipts.final.execution.source;
  async function runProof(source, stage, request, conclusion) {
    const run = await get(base + '/actions/runs/' + source.runId);
    need(
      String(run.id) === source.runId &&
        String(run.run_attempt) === source.attempt &&
        run.repository?.id === target.repositoryId &&
        run.head_repository?.id === target.repositoryId &&
        run.actor?.id === owner.memberId &&
        run.triggering_actor?.id === owner.memberId &&
        run.event === 'workflow_dispatch' &&
        run.status === 'completed' &&
        run.conclusion === conclusion &&
        run.path === '.github/workflows/nibfin-stage.yml' &&
        core.bodyDigest(run.head_sha + ':' + run.path) === source.workflow &&
        run.display_title ===
          core.actionRunName({
            issue: state.target.number,
            stage,
            correlation: request,
          }),
      'original-run',
    );
    return run;
  }
  const failed = await runProof(
    source,
    'closure',
    state.attempts.closure.request,
    'failure',
  );
  await runProof(
    finalSource,
    'final',
    state.receipts.final.execution.request,
    'success',
  );
  const events = await provider.collection({
    endpoint: base + '/issues/' + state.target.number + '/events?per_page=100',
    target: target.repositoryId,
    operation: 'native-repair-read',
    correlation: state.cycle,
    signal: AbortSignal.timeout(30000),
    allowPage: (p) =>
      p.split('?')[0] === base + '/issues/' + state.target.number + '/events',
  });
  need(events.outcome === 'complete' && Array.isArray(events.data), 'events');
  const since = Date.parse(failed.created_at);
  need(Number.isFinite(since), 'failure-time');
  const lifecycle = events.data.filter(
    (e) =>
      ['closed', 'reopened'].includes(e.event) &&
      (!Number.isFinite(Date.parse(e.created_at)) ||
        Date.parse(e.created_at) >= since),
  );
  need(lifecycle.length === 0, 'lifecycle-changed');
  const records = join(nativeCycleDirectory(root, state.target), 'records');
  let failure = null;
  for (const name of await fs.readdir(records)) {
    if (name === 'maintenance.lock') continue;
    need(/^[a-f0-9]{64}\.json$/.test(name), 'record-inventory');
    const value = await readPrivate(join(records, name), { limit: 134217728 });
    const { identity, ...body } = value;
    need(
      identity === core.contentIdentity(body) && name === identity + '.json',
      'record-integrity',
    );
    if (value.kind === 'operation-record') {
      const intent = core.controlledIntent(value.payload.value.intent);
      const completionWrite = intent.kind === 'close' ||
        (intent.kind === 'label' && intent.payload.labels.includes('delivered'));
      need(!completionWrite, 'mutation-custody');
    }
    const payload = value.payload;
    if (
      payload?.kind === 'native-core-result' &&
      payload.result?.request === state.attempts.closure.request
    ) {
      need(failure === null, 'ambiguous-failure');
      failure = payload.result;
    }
  }
  need(failure, 'retained-failure');
  const repair = {
    version: 1,
    previous: {
      approval: previous.approval,
      parent: previous.parent,
      execution: previous.execution,
      ...(previous.repair ? { repair: previous.repair } : {}),
    },
    retained: {
      state: state.identity,
      stage: 'closure',
      receipts: state.receipts,
      expectations: state.expectations,
      attempt: state.attempts.closure,
      failure,
      observation: {
        version: 1,
        state: state.identity,
        failure: core.contentIdentity(failure),
        issue: 'open',
        delivered: false,
        lifecycleSinceFailure: [],
        operations: { close: 'not-submitted', label: 'not-submitted' },
        originalRun: {
          id: source.runId,
          attempt: source.attempt,
          repositoryId: target.repositoryId,
          conclusion: 'failure',
        },
        finalRun: {
          id: finalSource.runId,
          attempt: finalSource.attempt,
          repositoryId: target.repositoryId,
          conclusion: 'success',
        },
        terminated: true,
      },
    },
  };
  const context = await continuationRepairContext(core, {
    root,
    parent: previous.parent,
    execution,
  });
  const draft = core.prepareMergedContinuationRepair(
    previous.parent,
    execution,
    repair,
    context,
  ).draft;
  return {
    archive,
    store,
    state,
    parent: previous.parent,
    execution,
    repair,
    context,
    draft,
  };
}

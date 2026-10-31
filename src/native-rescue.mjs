import { composeNativeRescue } from './native-rescue-composition.mjs';
import { join } from 'node:path';
import { openFileHostStore } from './host-store.mjs';
import { openNativeCycle } from './native-delivery-store.mjs';
import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { nativeActivationStatus } from './native-activation.mjs';
import { nativeRescueAgent } from './native-rescue-agent.mjs';
import { readPrivate } from './local-state.mjs';
import { nativePoolStore } from './native-pool-store.mjs';
import { retainNativeCandidate } from './native-base-revision.mjs';
import { reconcileNativePublication } from './native-publication-reconcile.mjs';
import { nativeGithubRateLimit } from './native-github-rate-limit.mjs';
const need = (ok, code) => {
  if (!ok) throw Error('native-rescue:' + code);
};
const verifyUnknownContinuation = (c) =>
  c.outcome === 'UNRESOLVED' &&
  c.code === 'UNKNOWN_DISPATCH' &&
  c.continuation?.outcome === 'confirmed-success';

// Native adapters for the existing Core recovery owners. The normal dispatcher
// remains the only delivery entry; Rescue can diagnose and prepare one original
// role continuation, never supply an accepted result or execute product stages.
export async function nativeRescue(
  core,
  {
    root,
    configuration,
    provider,
    signal,
    transport,
    onDispatch = async () => {},
    agent = nativeRescueAgent,
  } = {},
) {
  const host = await openFileHostStore(core, { directory: join(root, 'state') }),
    owner = await readNativeOwnerPolicy(core, { root }),
    cache = new Map();
  const same = (a, b) => core.canonicalJSON(a) === core.canonicalJSON(b);
  const get = async (state, endpoint) => {
    const r = await provider.request({
      endpoint,
      target: state.target.repositoryId,
      operation: 'native-rescue-observation',
      correlation: state.cycle,
      signal,
    });
    need(r.outcome === 'read', 'read');
    return r.data;
  };
  async function checkpoint(store) {
    const s = await store.read(),
      h = await host.read();
    need(
      (await nativeActivationStatus(core, { root })).status === 'ready' &&
        h.work.restrictionsChecked &&
        !h.work.paused &&
        !h.work.hold &&
        !h.work.unknownWrite,
      'owner-restriction',
    );
    need(
      (await readNativeOwnerPolicy(core, { root })).identity === owner.identity,
      'authority-changed',
    );
    const issue = await get(
      s,
      '/repos/' + s.target.repository + '/issues/' + s.target.number,
    );
    need(
      core.bodyDigest(core.parseRequirementsDocument(issue.body).scope) ===
        s.scopeHash && issue.assignees.some((a) => a.id === s.member.id),
      'scope-or-assignee-changed',
    );
    return { s, h, issue };
  }
  async function composition(store) {
    const initial = await store.read(),
      key = initial.target.repositoryId + ':' + initial.cycle;
    if (cache.has(key)) return cache.get(key);
    const value = await composeNativeRescue(core, {
      root,
      configuration,
      provider,
      signal,
      transport,
      onDispatch,
      agent,
      host,
      owner,
      checkpoint,
      get,
      same,
      need,
      store,
    });
    cache.set(key, value);
    return value;
  }
  async function observe({ store, claim }) {
    const s = await store.read(),
      protectedStage = !core.modelRoleStages.includes(claim.stage);
    need(
      core.coreStages.includes(claim.stage) &&
        s.attempts[claim.stage]?.phase === 'unresolved',
      'unresolved-role-required',
    );
    if (protectedStage && s.protectedStageCases?.[claim.id])
      return s.protectedStageCases[claim.id].caseId;
    const role = protectedStage
      ? s.receipts.deliver
        ? 'deliver'
        : 'shape'
      : claim.stage;
    const originalExecution = protectedStage ? s.receipts[role]?.execution.id : null;
    need(!protectedStage || originalExecution, 'original-role-required');
    const sessions = [];
    for (const [key, kind] of Object.entries(s.records)) {
      if (kind !== 'native-role-session') continue;
      const value = await store.get('native-record:' + key);
      if (
        protectedStage &&
        value.metadata.role === role &&
        value.metadata.execution === originalExecution
      )
        sessions.push({ reference: 'native-record:' + key, value });
      else if (!protectedStage && value.metadata.role === claim.stage) {
        const requests = Object.entries(s.records).filter(
          ([, k]) => k === 'native-role-request',
        );
        for (const [r] of requests) {
          const original = await store.get('native-record:' + r);
          if (
            original.input.execution.id === value.metadata.execution &&
            original.input.execution.source?.runId === claim.runId
          )
            sessions.push({ reference: 'native-record:' + key, value });
        }
      }
    }
    need(sessions.length === 1, 'original-session-required');
    const session = sessions[0];
    const job = await nativePoolStore(core, { root, configuration }).jobs.read(
      claim.id,
    );
    need(
      job?.phase === 'released' && session.value.snapshot.terminated === true,
      'original-resources-active',
    );
    let workspaceReference =
        protectedStage && role === 'deliver'
          ? await retainNativeCandidate(core, {
              root,
              state: s,
              store,
              execution: s.receipts.deliver.execution,
            })
          : null,
      unaccepted = null;
    for (const [key, kind] of Object.entries(s.records))
      if (!workspaceReference && kind === 'native-unaccepted-workspace') {
        const value = await store.get('native-record:' + key);
        if (value.execution.id === session.value.metadata.execution) {
          workspaceReference = 'native-record:' + key;
          unaccepted = core.contentIdentity(value);
        }
      }
    const target = {
      repository: s.target.repository,
      repositoryId: s.target.repositoryId,
      issueId: s.target.issueId,
      number: s.target.number,
      cycle: s.cycle,
      authority: s.authority.identity,
      requirements: s.scopeHash,
      scopeHash: s.scopeHash,
      shapeHash: s.shapeBody === null ? null : core.bodyDigest(s.shapeBody),
      // A stopped Writer can have edits without a qualified Core Candidate. Bind
      // its retained UNACCEPTED snapshot, never fabricate a normal stage receipt.
      candidate: s.workspaceExpected?.identity ?? unaccepted,
      execution: session.value.metadata.execution,
      role,
      adapter: 'codex',
      access: 'read-only',
      status: 'UNACCEPTED',
      memberId: s.member.id,
      sessionMemberId: s.member.id,
      runnerMemberId: s.member.id,
      runnerId: job.runnerId,
      session: session.value.metadata.id,
      source: { runId: claim.runId, attempt: '1' },
    };
    core.validateRescueTarget(target);
    need(
      target.role !== 'deliver' || workspaceReference,
      'writer-preservation-required',
    );
    await store.update((v) => {
      v.nativeRescueTargets ??= {};
      const key = core.contentIdentity(target),
        value = {
          sessionReference: session.reference,
          workspaceReference,
          ...(protectedStage
            ? { failureStage: claim.stage, failureRequest: claim.id }
            : {}),
        };
      if (v.nativeRescueTargets[key]) {
        need(same(v.nativeRescueTargets[key], value), 'target-rebinding');
        return null;
      }
      v.nativeRescueTargets[key] = value;
      return v;
    });
    const c = await composition(store),
      source = { runId: claim.runId, attempt: '1', head: claim.sourceCommit };
    await c.provenance.verify(source.runId, source.attempt);
    const blocked = {
      outcome: 'blocked',
      execution: target.execution,
      role: target.role,
      target,
      failureClass: 'implementation_defect',
      contractConflicts: [],
      summary: s.attempts[claim.stage].reason,
    };
    const id = await c.controller.register({
      target,
      source,
      code: 'STAGE_BLOCKED',
      blocked,
    });
    if (protectedStage)
      await store.update((v) => {
        v.protectedStageCases ??= {};
        v.protectedStageCases[claim.id] = {
          caseId: id,
          stage: claim.stage,
          status: 'diagnosing',
          attempts: 1,
        };
        return v;
      });
    if (protectedStage) await c.notifyProtected(id);
    else await c.controller.wake(id);
    return id;
  }
  return Object.freeze({
    observe,
    async observeProtected({ store, claim }) {
      if (
        core.modelRoleStages.includes(claim.stage) ||
        !core.coreStages.includes(claim.stage)
      )
        return { outcome: 'not-applicable' };
      const s = await store.read();
      if (s.attempts[claim.stage]?.phase !== 'unresolved')
        return { outcome: 'not-applicable' };
      let old = s.protectedStageCases?.[claim.id];
      if (old) {
        const c = (await host.rescue(s.target.repositoryId).read()).cases[old.caseId];
        need(c, 'protected-case');
        const decide = (options) =>
          core.protectedStageDiagnosisPlan({
            stage: claim.stage,
            attemptPhase: s.attempts[claim.stage].phase,
            marker: old,
            diagnosis: c.diagnosis,
            ...options,
          });
        const quota = await nativeGithubRateLimit(root);
        let decision = decide({ quota });
        if (decision.outcome === 'correct-marker') {
          await store.retain({
            kind: 'native-diagnosis-marker-correction',
            caseId: c.id,
            previous: old,
            reason:
              'No retained AI result exists; the earlier marker ' +
              'was not a completed diagnosis.',
          });
          await store.update((v) => {
            v.protectedStageCases[claim.id] = {
              ...old,
              status: decision.status,
              attempts: old.attempts ?? 1,
            };
            return v;
          });
          old = (await store.read()).protectedStageCases[claim.id];
          decision = decide({ quota });
        }
        if (decision.outcome !== 'retry') return decision;
        const active = (
          await nativePoolStore(core, { root, configuration }).jobs.pending()
        ).some(
          (j) =>
            j.issue === s.target.number &&
            j.request.repository === s.target.repository,
        );
        decision = decide({ quota, active });
        if (decision.outcome !== 'retry') return decision;
        const original = await composition(store);
        await original.controller.checkpoint(c);
        await original.records.update((v) => {
          v.cases[c.id] = {
            ...v.cases[c.id],
            outcome: 'PENDING',
            code: 'PRE_DIAGNOSIS_RETRY',
            nextCheckAt: null,
          };
          return v;
        });
        await store.update((v) => {
          need(same(v.protectedStageCases[claim.id], old), 'protected-case-changed');
          v.protectedStageCases[claim.id] = {
            ...old,
            status: 'diagnosing',
            attempts: decision.attempts,
          };
          return v;
        });
        await original.notifyProtected(c.id);
        return { outcome: 'diagnosing' };
      }
      const reconciled = await reconcileNativePublication(core, {
        root,
        store,
        claim,
        provider,
      });
      if (reconciled.outcome === 'unknown') return reconciled;
      if ((await host.read()).work.unknownWrite)
        return {
          outcome: 'unknown',
          reason: 'original-write-reconciliation-required',
        };
      await observe({ store, claim });
      return { outcome: 'diagnosing' };
    },
    async execute(binding, { progress = async () => {} } = {}) {
      const selected = configuration.targets.find(
          (t) => t.repository === binding.request.repository,
        ),
        issue = await get(
          { target: selected, cycle: binding.request.id },
          '/repos/' + selected.repository + '/issues/' + binding.issue,
        );
      const store = await openNativeCycle(core, {
        root,
        target: {
          repository: selected.repository,
          repositoryId: selected.repositoryId,
          issueId: issue.node_id,
          databaseId: issue.id,
          number: binding.issue,
        },
      });
      const c = await composition(store),
        result = await c.execute(binding);
      await progress({
        ...core.rescueActionReport(result, result),
        ...(result.failureStage ? { stage: result.failureStage } : {}),
      });
      return {
        disposition:
          result.outcome === 'RESOLVED'
            ? 'completed'
            : core.eligibleRescueRecheck(result)
              ? 'waiting'
              : 'blocked',
        reason: result.code ?? result.outcome,
        rescue: {
          outcome: result.outcome,
          caseId: result.id,
          diagnosis: result.diagnosis ?? null,
        },
      };
    },
    async refresh() {
      // Reconstruct controllers from durable cases after a daemon restart.
      for (const target of configuration.targets)
        for (const c of Object.values(
          (await host.rescue(target.repositoryId).read()).cases,
        )) {
          if (
            !(core.eligibleRescueRecheck(c) || verifyUnknownContinuation(c)) ||
            cache.has(target.repositoryId + ':' + c.target.cycle)
          )
            continue;
          const issue = await get(
            { target, cycle: c.target.cycle },
            '/repos/' + target.repository + '/issues/' + c.target.number,
          );
          const store = await openNativeCycle(core, {
            root,
            target: {
              repository: target.repository,
              repositoryId: target.repositoryId,
              issueId: issue.node_id,
              databaseId: issue.id,
              number: c.target.number,
            },
          });
          need(
            (await store.read()).cycle === c.target.cycle,
            'original-cycle-required',
          );
          await composition(store);
        }
      for (const c of cache.values()) {
        if (c.busy()) continue;
        for (const item of Object.values((await c.records.read()).cases)) {
          if (item.target.cycle !== c.cycle) continue;
          // A reporting notification can be unknown even though the original role
          // continuation is proven. Reuse Core's independent receipt verifier; an
          // active continuation remains deferred and never becomes an accepted role.
          if (verifyUnknownContinuation(item)) await c.controller.verify(item.id);
          else if (core.eligibleRescueRecheck(item) && item.nextCheckAt <= Date.now())
            await c.controller.wake(item.id);
        }
      }
    },
    async close() {
      await host.close();
    },
  });
}

import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { setTimeout as wait } from 'node:timers/promises';
import { nativeExecutionInputs } from './native-execution-inputs.mjs';
import { managedWorkflowPlan } from './workflow-inputs.mjs';
import {
  readPrivate,
  sha,
  writePrivate,
  ensurePrivateDirectory,
} from './local-state.mjs';

export async function composeNativeRescue(
  core,
  {
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
  },
) {
  const initial = await store.read();
  const records = host.rescue(initial.target.repositoryId),
    base = '/repos/' + initial.target.repository,
    plan = managedWorkflowPlan(core, {
      executionPool: configuration,
      repository: initial.target.repository,
    });
  const files = {};
  for (const name of ['stage', 'rescue']) {
    const path = '.github/workflows/nibfin-' + name + '.yml',
      file = await get(initial, base + '/contents/' + path + '?ref=main');
    need(
      file.type === 'file' &&
        file.encoding === 'base64' &&
        Buffer.from(file.content, 'base64').equals(Buffer.from(plan.files.get(path))),
      'workflow-source',
    );
    files[name] = file;
  }
  const policy = {
    version: 1,
    visibility: 'public',
    repository: initial.target.repository,
    repositoryId: initial.target.repositoryId,
    ref: 'main',
    stageWorkflow: '.github/workflows/nibfin-stage.yml',
    stageBlob: files.stage.sha,
    rescueBlob: files.rescue.sha,
    actorIds: [owner.memberId],
    historical: [],
    maxRepairs: 3,
    cooldownMs: 30000,
    jobTimeoutMinutes: 60,
    authority: initial.authority.identity,
    expiresAt: initial.grant.payload.expiresAt,
  };
  const provenance = core.createRescueProvenance({ policy, provider });
  let jobBinding = null,
    activeCase = null,
    adapter = null,
    protectedOperations;
  const input = await nativeExecutionInputs(core, {
    root,
    store,
    configuration,
    provider,
    signal,
    transport,
    binding: { request: { id: 'native-rescue' } },
    checkpoint: () => checkpoint(store),
  });
  const original = async (t) => {
    const s = await store.read(),
      retained = s.nativeRescueTargets?.[core.contentIdentity(t)];
    need(retained, 'original-target');
    const session = await store.get(retained.sessionReference);
    need(
      session.kind === 'native-role-session' &&
        session.snapshot.terminated === true &&
        session.metadata.id === t.session &&
        session.metadata.execution === t.execution &&
        session.metadata.role === t.role &&
        session.metadata.memberId === t.memberId,
      'original-session',
    );
    return { s, retained, session };
  };
  const capture = async (t) => {
    const { s, retained, session } = await original(t);
    return core.contentIdentity({
      scope: s.scopeHash,
      base: s.base,
      candidate: s.candidate,
      workspace: s.workspace,
      unaccepted: retained.workspaceReference ?? null,
      session: session.snapshot.digest,
    });
  };
  const current = async (t) => {
    const { s, h } = await checkpoint(store);
    await original(t);
    const { retained } = await original(t);
    const unaccepted = retained.workspaceReference
      ? core.contentIdentity(await store.get(retained.workspaceReference))
      : null;
    const actual = {
      ...t,
      scopeHash: s.scopeHash,
      shapeHash: s.shapeBody === null ? null : core.bodyDigest(s.shapeBody),
      authority: s.authority.identity,
      candidate: s.workspaceExpected?.identity ?? unaccepted,
    };
    const prepared =
      s.nativeRescueContinuations?.[activeCase ?? s.sessionResumes?.[t.role]?.caseId];
    const recovery = prepared && same(prepared.target, t) ? prepared : null;
    const receipt = recovery ? s.receipts[t.role] : null;
    return {
      ...h.work,
      target: actual,
      authority: s.authority.identity,
      conflict: false,
      stageReceipt: receipt
        ? { caseId: recovery.caseId, receipt, expected: s.expectations[t.role] }
        : null,
    };
  };
  const continuation = core.createRescueContinuation({
    load: async (t) => {
      const { session } = await original(t);
      return {
        status: 'UNACCEPTED',
        target: t,
        terminated: true,
        workspace: await capture(t),
        session: session.metadata.id,
        memberId: t.memberId,
      };
    },
    retain: async (value) => {
      await store.retain({ kind: 'native-rescue-continuation', ...value });
      return { outcome: 'retained' };
    },
    prepare: async (value) => {
      const s = await store.read(),
        next = {
          execution: 'role-' + randomUUID(),
          request: 'native-' + randomUUID(),
          result: 'result-' + randomUUID(),
          session: value.session,
          memberId: value.target.memberId,
          role: value.target.role,
          freshBudget: true,
          status: 'UNACCEPTED',
          workspace: value.workspace,
          access: value.target.role === 'deliver' ? 'writer' : 'read-only',
        };
      const retained = s.nativeRescueTargets[core.contentIdentity(value.target)];
      await store.update((s) => {
        s.nativeRescueContinuations ??= {};
        need(
          !s.nativeRescueContinuations[value.caseId],
          'original-continuation-required',
        );
        s.nativeRescueContinuations[value.caseId] = {
          caseId: value.caseId,
          status: 'prepared',
          target: value.target,
          next,
          plan: value.plan,
          proof: value.proof,
          sessionReference: retained.sessionReference,
          workspaceReference: retained.workspaceReference ?? null,
        };
        return s;
      });
      return next;
    },
  });
  async function arm(c, proof) {
    const { s, h } = await checkpoint(store),
      prepared = s.nativeRescueContinuations?.[c.id];
    need(prepared && same(prepared.next, proof.next), 'prepared-continuation');
    need(!h.work.active && h.work.terminated, 'original-work-active');
    const pending = h.work.pending.filter((p) => p.cycle === s.cycle);
    if (prepared.status === 'armed' && pending.length === 0) {
      need(
        s.dispatch?.id === prepared.next.request && !s.attempts[c.target.role],
        'armed-continuation-changed',
      );
      return;
    }
    need(
      pending.length === 1 && pending[0].status === 'UNACCEPTED',
      'original-work-unaccepted',
    );
    if (prepared.status === 'prepared')
      await store.update((v) => {
        // Observation/operation records share this file but do not change the
        // original stopped work. Compare the protected inputs inside the store
        // transaction instead of rejecting unrelated telemetry revisions.
        need(
          same(v.attempts[c.target.role], s.attempts[c.target.role]) &&
            v.attempts[c.target.role]?.phase === 'unresolved' &&
            same(v.dispatch, s.dispatch) &&
            same(v.nativeRescueContinuations[c.id], prepared) &&
            [
              'core',
              'installation',
              'cycle',
              'scopeHash',
              'authority',
              'policy',
              'grant',
              'base',
              'head',
              'candidate',
              'workspace',
              'workspaceExpected',
              'shapeBody',
            ].every((key) => same(v[key] ?? null, s[key] ?? null)),
          'original-attempt',
        );
        v.recoveries ??= [];
        v.recoveries.push({
          kind: 'ai-rescue',
          caseId: c.id,
          attempt: v.attempts[c.target.role],
          dispatch: v.dispatch,
          proof: prepared.proof,
        });
        delete v.attempts[c.target.role];
        v.dispatch = { id: prepared.next.request, stage: c.target.role };
        v.sessionResumes ??= {};
        v.sessionResumes[c.target.role] = {
          status: 'UNACCEPTED',
          role: c.target.role,
          memberId: c.target.memberId,
          scopeHash: s.scopeHash,
          cycle: s.cycle,
          caseId: c.id,
          reference: prepared.sessionReference,
          workspaceReference: prepared.workspaceReference,
          next: prepared.next,
          plan: prepared.plan,
        };
        v.nativeRescueContinuations[c.id].status = 'armed';
        return v;
      });
    for (let attempt = 0; attempt < 20; attempt++) {
      const fresh = await host.read();
      need(same(fresh.work, h.work), 'work-changed');
      try {
        await host.updateWork({
          expected: fresh.identity,
          work: {
            ...fresh.work,
            pending: fresh.work.pending.filter((p) => p.cycle !== s.cycle),
            reconciled: true,
          },
        });
        return;
      } catch (e) {
        if (!e.message?.endsWith('work-compare-and-swap')) throw e;
      }
    }
    need(false, 'work-update-unavailable');
  }
  const originalIntent = core.createRescueDispatch({
    policy,
    provenance,
    memberId: initial.member.id,
    load: async ({ target: t, kind }) => {
      const s = await store.read(),
        workflow = await provenance.workflow(kind),
        request = activeCase ?? core.contentIdentity(t);
      return {
        workflow,
        source: {
          request,
          event: 'workflow_dispatch',
          repositoryId: t.repositoryId,
          runId: t.source.runId,
          attempt: t.source.attempt,
          actorId: t.memberId,
        },
        binding: {
          repository: t.repository,
          repositoryId: t.repositoryId,
          issueId: t.issueId,
          number: t.number,
          cycle: t.cycle,
          scopeHash: t.scopeHash,
          shapeHash: t.shapeHash,
          authorization: core.contentIdentity(s.grant),
          runId: t.source.runId,
          attempt: t.source.attempt,
          candidate: t.candidate,
          base: s.base,
          head: s.head,
        },
      };
    },
  });
  const intent = async (value) => {
    const result = await originalIntent(value),
      s = await store.read(),
      retained = s.nativeRescueTargets?.[core.contentIdentity(value.case.target)];
    if (value.kind === 'notification' && retained?.failureStage) {
      result.payload.inputs.mode = 'diagnose';
      result.payload.inputs.nibfin_stage = retained.failureStage;
      result.payload.display.stage = retained.failureStage;
    }
    return core.controlledIntent(result);
  };
  const controller = core.createRescueController({
    policy,
    records,
    provenance,
    current,
    capture,
    authorize: async ({ target: t }) => {
      const { s, issue } = await checkpoint(store),
        a = await input.authorization.eligibility(
          { operation: 'dispatch' },
          { signal },
        );
      const permission = await get(
        s,
        base + '/collaborators/' + s.member.login + '/permission',
      );
      return {
        ok: a.ok === true,
        memberId: s.member.id,
        runnerId: t.runnerId,
        assignee: issue.assignees.some((a) => a.id === s.member.id),
        permission: permission.permission,
        sessionMemberId: s.member.id,
      };
    },
    active: async (t) => {
      const h = await host.read();
      return h.work.pending.some(
        (p) => p.cycle === t.cycle && ['ACTIVE', 'UNRESOLVED'].includes(p.status),
      );
    },
    deterministic: async () => null,
    adapter: async (t) => {
      need(jobBinding && activeCase, 'authenticated-rescue-job');
      adapter = await agent(core, {
        root,
        binding: jobBinding,
        store,
        target: t,
        caseId: activeCase,
        checkpoint: () => checkpoint(store),
        signal,
      });
      return adapter;
    },
    retainDiagnosis: async (value) => {
      await store.retain({ kind: 'native-rescue-diagnosis', ...value });
      return { outcome: 'retained' };
    },
    continueRole: continuation,
    intent,
    operations: (protectedOperations = async ({
      case: c,
      intent: v,
      beforeSubmit,
    }) => {
      // This includes the repeated provenance, live Scope and authorization
      // guards plus official readback, not just one HTTP request. Keep all guards
      // fresh and bounded without expiring at the provider's 30s request budget.
      const normal = core.createControlledOperations({
        timeoutMs: 180000,
        authorization: input.authorization,
        provider,
        operationRecords: store.operationRecords,
        locate: store.locateOperation,
        execution: { runId: c.target.source.runId, attempt: c.target.source.attempt },
        beforeSubmit: async (actual) => {
          await beforeSubmit(actual);
          if (v.payload.inputs.request_id === undefined)
            await arm(c, (await store.read()).nativeRescueContinuations[c.id]);
        },
      });
      return {
        execute: async (actual) => {
          let result = await normal.execute(actual, { signal });
          // Actions may acknowledge a dispatch before its Run appears in the list.
          // Reopen only an already-retained result through the same Core operation
          // owner: its existing intent/attempt path re-observes without another POST.
          const readbackSignal = AbortSignal.any([
            AbortSignal.timeout(120000),
            ...(signal ? [signal] : []),
          ]);
          for (const delay of [1000, 2000, 4000, 8000, 16000, 30000]) {
            if (
              result.outcome !== 'unknown' ||
              !result.refs?.result ||
              readbackSignal.aborted
            )
              break;
            const prior = await store.locateOperation({
              identity: core.contentIdentity(actual),
            });
            need(
              prior.outcome === 'found' && same(prior.refs, result.refs),
              'original-readback-record',
            );
            try {
              await wait(delay, undefined, { signal: readbackSignal });
            } catch {
              break;
            }
            result = await normal.execute(actual, { signal: readbackSignal });
          }
          if (result.outcome === 'confirmed-success') {
            const s = await store.read(),
              inputs = actual.payload.inputs,
              rescue = inputs.request_id !== undefined;
            const id = rescue ? actual.correlation : inputs.nibfin_request,
              issue = await get(s, base + '/issues/' + s.target.number);
            const claim = {
              version: 1,
              id,
              provenance: {
                kind: 'native-rescue-dispatch',
                memberId: s.member.id,
                caseId: c.id,
              },
              repository: s.target.repository,
              repositoryId: s.target.repositoryId,
              issue: s.target.number,
              cycle: s.cycle,
              stage: inputs.nibfin_stage,
              workflowId: actual.target.workflow,
              workflowPath: rescue
                ? '.github/workflows/nibfin-rescue.yml'
                : '.github/workflows/nibfin-stage.yml',
              jobName: rescue ? 'rescue' : 'stage / execute',
              sourceCommit: actual.payload.refIdentity.sha,
              issueBodySha256: sha(issue.body),
              correlation: actual.correlation,
              title: core.actionRunName({
                issue: s.target.number,
                stage: inputs.nibfin_stage,
                correlation: actual.correlation,
                ...(rescue ? { rescue: true, mode: inputs.mode } : {}),
              }),
              createdAt: Date.now(),
              phase: 'run-observed',
              runId: String(result.observation.id),
            };
            await ensurePrivateDirectory(join(root, 'dispatches'));
            const file = join(root, 'dispatches', id + '.json'),
              prior = await readPrivate(file, { missing: true });
            if (prior)
              need(
                prior.runId === claim.runId && prior.title === claim.title,
                'dispatch-rebinding',
              );
            else
              await writePrivate(
                file,
                { ...claim, identity: core.contentIdentity(claim) },
                { exclusive: true },
              );
            await onDispatch({ store, intent: actual, result, caseId: c.id });
          }
          return result;
        },
      };
    }),
    completion: async () => {
      const s = await store.read();
      if (!s.completion) return null;
      const c = Object.values(s.nativeRescueContinuations ?? {}).find(
        (c) => c.status === 'armed',
      );
      return {
        receipt: s.completion,
        expected: s.completionCurrent,
        cycle: s.cycle,
        caseId: c?.caseId ?? null,
        continuation: c ? s.receipts[c.target.role]?.execution.source.runId : null,
      };
    },
  });
  const value = {
    records,
    controller,
    provenance,
    policy,
    cycle: initial.cycle,
    busy: () => jobBinding !== null,
    async notifyProtected(id) {
      const c = (await records.read()).cases[id];
      await controller.checkpoint(c);
      const s = await store.read(),
        original = s.nativeRescueTargets[core.contentIdentity(c.target)],
        marker = s.protectedStageCases[original.failureRequest];
      const proof = {
        fingerprint: core.contentIdentity({
          target: c.target,
          source: c.source,
          protectedStage: true,
          diagnosisAttempt: marker.attempts ?? 1,
        }),
      };
      const v = await intent({ case: c, kind: 'notification', proof });
      const owned = await protectedOperations({
        case: c,
        intent: v,
        beforeSubmit: async (actual) => {
          need(
            same(actual, v) &&
              same(await intent({ case: c, kind: 'notification', proof }), v),
            'protected-notification-changed',
          );
          await controller.checkpoint(c);
        },
      });
      const result = await owned.execute(v);
      await records.update((s) => {
        s.notifications[id] = {
          ...proof,
          outcome: result.outcome,
          observation: result.observation ?? null,
        };
        s.cases[id] = {
          ...s.cases[id],
          triggered: result.outcome === 'confirmed-success',
          outcome: result.outcome === 'confirmed-success' ? 'DEFERRED' : 'UNRESOLVED',
          code:
            result.outcome === 'confirmed-success'
              ? 'PROTECTED_STAGE_DIAGNOSIS_QUEUED'
              : 'UNKNOWN_DISPATCH',
          nextCheckAt: null,
        };
        return s;
      });
      if (result.outcome !== 'confirmed-success')
        await store.update((v) => {
          v.protectedStageCases[original.failureRequest].status =
            result.outcome === 'unknown'
              ? 'notification-unresolved'
              : 'diagnosis-unavailable';
          return v;
        });
      return result;
    },
    async execute(binding) {
      need(!jobBinding, 'diagnosis-already-active');
      const matches = Object.values((await records.read()).cases).filter(
        (c) => c.target.cycle === initial.cycle,
      );
      const operations = (await store.read()).operations;
      const notification = Object.values(operations).find(
        (o) =>
          o.intent.kind === 'dispatch' &&
          o.intent.correlation === binding.correlation &&
          o.intent.payload.inputs.request_id,
      );
      need(notification, 'retained-rescue-dispatch');
      const id = notification.intent.payload.inputs.request_id;
      const claim = await readPrivate(
          join(root, 'dispatches', binding.request.id + '.json'),
        ),
        { identity, ...claimBody } = claim;
      need(
        identity === core.contentIdentity(claimBody) &&
          claim.runId === binding.request.runId &&
          claim.repository === binding.request.repository &&
          claim.correlation === binding.correlation &&
          claim.workflowPath === '.github/workflows/nibfin-rescue.yml' &&
          binding.mode === notification.intent.payload.inputs.mode,
        'actual-rescue-invocation',
      );
      need(
        matches.some((c) => c.id === id),
        'case-cycle',
      );
      jobBinding = binding;
      activeCase = id;
      try {
        const s = await store.read(),
          selected = matches.find((c) => c.id === id),
          original = s.nativeRescueTargets?.[core.contentIdentity(selected.target)];
        const result =
          binding.mode === 'verify'
            ? await controller.verify(id)
            : await controller.run(id, {
                signal,
                diagnoseOnly: binding.mode === 'diagnose',
              });
        if (original?.failureStage && result.outcome !== 'RESOLVED') {
          // Protected owners have no model execution to retry. Diagnose their real
          // failure without spending/resetting the original-role repair budget or
          // pretending that diagnosis alone repaired the product stage.
          const diagnosis = core.protectedStageDiagnosisResult(result),
            { code } = diagnosis;
          await records.update((v) => {
            v.cases[id] = {
              ...v.cases[id],
              outcome: diagnosis.outcome,
              code,
              nextCheckAt: diagnosis.nextCheckAt,
            };
            return v;
          });
          await store.update((v) => {
            const marker = v.protectedStageCases?.[original.failureRequest];
            need(marker?.caseId === id, 'protected-case');
            marker.status = diagnosis.status;
            marker.lastFailure = { code, runId: binding.request.runId };
            return v;
          });
          return {
            ...(await records.read()).cases[id],
            failureStage: original.failureStage,
          };
        }
        return result;
      } finally {
        try {
          await adapter?.close();
        } finally {
          adapter = null;
          jobBinding = null;
          activeCase = null;
        }
      }
    },
  };
  return value;
}

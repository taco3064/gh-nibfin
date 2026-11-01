import { readNativeContinuationCustody } from './native-continuation-custody.mjs';
import { join } from 'node:path';
import { currentPin, currentProducer } from './current-core.mjs';
import { readPrivate } from './local-state.mjs';
import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { readNativeMember } from './native-member.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-handover:' + reason);
};
const same = (core, a, b) => core.canonicalJSON(a) === core.canonicalJSON(b);
const continuationCall = (run) => {
  try {
    return run();
  } catch (error) {
    const code = /^(?:continuation|evidence|authority|core):[a-z-]+$/.test(
      error.message,
    )
      ? error.message.replace(':', '-')
      : 'retained-evidence-invalid';
    throw Error('native-handover:' + code);
  }
};

export async function nativeContinuationApprovalContext(
  core,
  { root, parent, execution, operation = 'verify-deployment' },
) {
  need(core.distributionAPI?.mergedContinuation === 1, 'compatible-core-required');
  const owner = await readNativeOwnerPolicy(core, { root });
  const installation = await readPrivate(join(root, 'native-installation.json'));
  const activation = await readPrivate(join(root, 'activation.json'));
  const { identity, ...body } = activation;
  need(
    identity === core.contentIdentity(body) &&
      activation.phase === 'active' &&
      activation.installation === core.contentIdentity(installation) &&
      execution.activation === identity &&
      execution.core === currentPin.coreSha256 &&
      execution.source === currentProducer &&
      installation.core === execution.core &&
      owner.memberId === execution.memberId &&
      owner.keyId === execution.keyId &&
      parent.memberId === owner.memberId,
    'execution-authority',
  );
  const { draft } = continuationCall(() =>
    core.prepareMergedContinuation(parent, execution),
  );
  const work = draft.work[0],
    permission = draft.permissions[0];
  return {
    now: Date.now(),
    trustedKeys: new Map([[owner.keyId, owner.publicKey]]),
    revocation: {
      id: owner.keyId,
      available: true,
      revokedRecords: owner.revokedRecords ?? [],
    },
    expectedAuthority: core.authoritySnapshot(parent.authority),
    expectedPolicy: { ...parent.policy, core: currentPin.coreSha256 },
    expectedWork: draft.work,
    request: {
      repository: work.repository,
      issueId: work.issueId,
      number: work.number,
      path: permission.paths[0],
      operation,
    },
  };
}

/** Actual authenticated observations precede signing and atomic import. Labels
 * and the archive's summary are never substitutes for the original merged work.
 */
export async function observeNativeMergedHandover(
  core,
  { root, configuration, repository, state, archive, provider, execution: retained },
) {
  need(core.distributionAPI?.mergedContinuation === 1, 'compatible-core-required');
  const owner = await readNativeOwnerPolicy(core, { root });
  const selected = configuration.targets.find((p) => p.repository === repository);
  need(
    selected &&
      state.target.repository === repository &&
      state.target.repositoryId === selected.repositoryId &&
      state.member.id === owner.memberId &&
      core.nextDeliveryStage(state.receipts, null) === 'final' &&
      !state.completion &&
      state.closed === false &&
      !state.attempts.final &&
      Object.values(state.attempts).every((a) => a.phase === 'completed'),
    'merged-pending-final-required',
  );
  const get = async (path) => {
    const result = await provider.request({
      endpoint: path,
      target: selected.repositoryId,
      operation: 'native-handover-read',
      correlation: 'owner-init',
      signal: AbortSignal.timeout(30000),
    });
    need(result.outcome === 'read', 'authenticated-observation');
    return result.data;
  };
  const account = await get('/user'),
    base = '/repos/' + repository;
  const repo = await get(base),
    issue = await get(base + '/issues/' + state.target.number);
  need(
    account.id === owner.memberId &&
      account.login === owner.memberLogin &&
      repo.id === selected.repositoryId &&
      repo.full_name?.toLowerCase() === repository &&
      repo.owner?.id === selected.ownerId &&
      repo.owner.type === selected.ownerType &&
      repo.private === false &&
      repo.visibility === 'public' &&
      repo.permissions?.admin === true &&
      repo.default_branch === 'main',
    'current-owner',
  );
  need(
    issue.id === state.target.databaseId &&
      issue.node_id === state.target.issueId &&
      String(issue.number) === state.target.number &&
      !issue.pull_request &&
      issue.state === 'open' &&
      issue.body === state.shapeBody &&
      core.parseRequirementsDocument(issue.body).scope === state.scopeBody &&
      issue.assignees.some((a) => a.id === owner.memberId) &&
      ['scope', 'shaped'].every((label) =>
        issue.labels.some((l) => l.name === label),
      ),
    'current-requirements',
  );
  const member = await readNativeMember(core, { root, memberId: owner.memberId });
  need(
    member.image === state.memberRuntime.image &&
      member.runtimeProfile === state.memberRuntime.runtimeProfile &&
      core.contentIdentity({
        agent: member.agent,
        image: member.image,
        runtimeProfile: member.runtimeProfile,
      }) === state.policy.runtime,
    'original-runtime',
  );
  const endpoint = state.publication?.pr?.observation?.endpoint;
  need(
    typeof endpoint === 'string' &&
      endpoint.startsWith(base + '/pulls/') &&
      /^[1-9][0-9]*$/.test(endpoint.slice((base + '/pulls/').length)),
    'original-PR',
  );
  const pr = await get(endpoint),
    merge = state.receipts.merge;
  need(
    pr.id === state.publication.pr.observation.id &&
      pr.merged === true &&
      pr.merge_commit_sha === merge.inputs.commit &&
      pr.head?.sha === state.head &&
      pr.head.repo?.id === selected.repositoryId &&
      pr.base?.repo?.id === selected.repositoryId &&
      pr.base.ref === state.policy.merge.baseBranch &&
      pr.user?.id === owner.memberId,
    'merged-PR-readback',
  );
  const source = merge.execution.source;
  need(source?.repositoryId === selected.repositoryId, 'original-Run');
  const run = await get(base + '/actions/runs/' + source.runId);
  need(
    String(run.id) === source.runId &&
      String(run.run_attempt) === source.attempt &&
      run.repository?.id === selected.repositoryId &&
      run.head_repository?.id === selected.repositoryId &&
      run.actor?.id === owner.memberId &&
      run.triggering_actor?.id === owner.memberId &&
      run.event === 'workflow_dispatch' &&
      run.display_title ===
        core.actionRunName({
          issue: state.target.number,
          stage: 'merge',
          correlation: merge.execution.request,
        }) &&
      /^[a-f0-9]{40}$/.test(run.head_sha) &&
      core.bodyDigest(run.head_sha + ':.github/workflows/nibfin-stage.yml') ===
        source.workflow &&
      run.status === 'completed' &&
      run.conclusion === 'success' &&
      run.path === '.github/workflows/nibfin-stage.yml',
    'original-Run-readback',
  );
  const priorOwner = await archive.readJSON('historical-authority/owner-policy.json');
  const { identity: priorIdentity, ...priorBody } = priorOwner;
  need(
    priorIdentity === core.contentIdentity(priorBody) &&
      priorOwner.keyId === state.ownerKey.id &&
      priorOwner.publicKey === state.ownerKey.publicKey &&
      priorOwner.memberId === owner.memberId,
    'historical-owner',
  );
  const stages = core.coreStages.slice(0, core.coreStages.indexOf('merge') + 1);
  const parent = {
    state: state.identity,
    archive: archive.identity,
    cycle: state.cycle,
    target: state.target,
    memberId: state.member.id,
    ownerKey: state.ownerKey,
    grant: state.grant,
    policy: state.policy,
    authority: state.authority,
    scopeBody: state.scopeBody,
    shapeBody: state.shapeBody,
    receipts: Object.fromEntries(stages.map((s) => [s, state.receipts[s]])),
    expectations: Object.fromEntries(stages.map((s) => [s, state.expectations[s]])),
    revocation: {
      id: priorOwner.keyId,
      available: true,
      revokedRecords: priorOwner.revokedRecords ?? [],
    },
  };
  const activation = await readPrivate(join(root, 'activation.json'));
  const execution = retained ?? {
    core: currentPin.coreSha256,
    source: currentProducer,
    activation: activation.identity,
    memberId: owner.memberId,
    keyId: owner.keyId,
    validFrom: Date.now() - 1000,
    expiresAt: Math.min(Date.now() + 604800000, state.grant.payload.expiresAt),
  };
  const context = await nativeContinuationApprovalContext(core, {
    root,
    parent,
    execution,
  });
  const draft = continuationCall(() =>
    core.prepareMergedContinuation(parent, execution),
  ).draft;
  return { parent, execution, context, draft };
}

/** Original receipts, sessions and attempts stay historical. Only the separately
 * signed fresh Grant is used by the active authorization boundary.
 */
export async function readNativeMergedContinuation(
  core,
  { root, store, state, operation = 'verify-deployment' },
) {
  const value = await readNativeContinuationCustody(core, { store, state });
  const context = await nativeContinuationApprovalContext(core, {
    root,
    parent: value.parent,
    execution: value.execution,
    operation,
  });
  const authorized = core.verifyGrant(value.approval, context);
  need(authorized.ok, 'approval-' + authorized.category);
  const checked = continuationCall(() =>
    core.verifyMergedContinuation(value.approval, {
      parent: value.parent,
      execution: value.execution,
      ...(value.repair ? { repair: value.repair } : {}),
      context,
    }),
  );
  return {
    ...checked,
    approval: value.approval,
    parent: value.parent,
    plannedExecution: value.execution,
    ...(value.repair ? { repair: value.repair } : {}),
    context,
  };
}

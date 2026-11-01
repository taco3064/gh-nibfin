import { readNativeOwnerPolicy } from './native-owner-policy.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-legacy-handover:' + reason);
};
const targetFor = (target) => ({
  repository: target.repository,
  issueId: target.issueId,
  number: target.number,
});
const currentFor = (state) => ({
  target: targetFor(state.target),
  memberId: state.member.id,
  scopeHash: state.scopeHash,
  core: state.core,
  cycle: state.cycle,
  labels: state.handoverLabels,
  workflow: 'shaped',
  authority: state.authority.identity,
  ...(state.handoverPreviousIdentity
    ? { predecessor: state.handoverPreviousIdentity }
    : {}),
});
const referenceFor = (core, payload) =>
  'native-record:' + core.contentIdentity({ version: 1, kind: 'retained', payload });

/** Prepare only data for atomic cycle publication. A normal current Owner Grant
 * binds this proof. Actual signature checks precede import and every execution.
 */
export function prepareLegacyNativeHandover(
  core,
  { state, original, previous = null },
) {
  need(core.distributionAPI?.unacceptedHandover === 1, 'compatible-core-required');
  const current = currentFor(state);
  if (previous)
    core.verifyUnacceptedHandoverRevision({ previous, current, original });
  else need(!current.predecessor, 'previous-proof-required');
  const plan = core.unacceptedHandoverPolicy({ original, current });
  const { snapshot, workspaceSnapshot, ...metadata } = original;
  const session = {
    kind: 'native-role-session',
    version: 1,
    metadata: {
      id: original.session,
      execution: original.execution,
      memberId: original.memberId,
      role: 'deliver',
      adapter: 'codex',
    },
    snapshot,
  };
  const sessionReference = referenceFor(core, session);
  const workspaceReference = referenceFor(core, workspaceSnapshot);
  const history = previous
    ? {
        kind: 'native-handover-preexecution-history',
        version: 1,
        state: previous.state,
      }
    : null;
  const proof = {
    version: 1,
    kind: 'native-owner-legacy-handover',
    original: metadata,
    current,
    sessionReference,
    workspaceReference,
    plan: plan.identity,
    ...(history ? { previousReference: referenceFor(core, history) } : {}),
  };
  const proofReference = referenceFor(core, proof);
  return {
    recordId: plan.recordId,
    current,
    retained: [session, workspaceSnapshot, proof, ...(history ? [history] : [])],
    legacyHandover: proofReference,
    deferredRoles: {
      deliver: {
        phase: 'prepared',
        status: 'UNACCEPTED',
        scopeHash: state.scopeHash,
        memberId: state.member.id,
        caseId: plan.identity,
        proof: proofReference,
        sessionReference,
        workspaceReference,
        target: {
          ...targetFor(state.target),
          repositoryId: state.target.repositoryId,
          cycle: state.cycle,
          execution: original.execution,
          role: 'deliver',
          memberId: original.memberId,
          session: original.session,
          adapter: 'codex',
        },
        plan: {
          kind: 'legacy-unaccepted-handover',
          instructions: plan.instructions,
          originalRepairCount: original.repairs,
        },
      },
    },
  };
}

export async function verifyNativeLegacyHandover(
  core,
  { root, store, state, now = Date.now(), depth = 0 },
) {
  if (!state.legacyHandover) return null;
  need(depth <= 16, 'history-depth');
  need(core.distributionAPI?.unacceptedHandover === 1, 'compatible-core-required');
  const owner = await readNativeOwnerPolicy(core, { root });
  const proof = await store.get(state.legacyHandover);
  need(proof.kind === 'native-owner-legacy-handover', 'proof');
  const session = await store.get(proof.sessionReference);
  const workspaceSnapshot = await store.get(proof.workspaceReference);
  need(
    session.kind === 'native-role-session' &&
      session.metadata.id === proof.original.session &&
      session.metadata.execution === proof.original.execution &&
      session.metadata.memberId === owner.memberId &&
      session.metadata.role === 'deliver',
    'original-session',
  );
  const original = {
    ...proof.original,
    snapshot: session.snapshot,
    workspaceSnapshot,
  };
  const current = currentFor(state);
  need(
    core.canonicalJSON(current) === core.canonicalJSON(proof.current) &&
      owner.memberId === state.member.id &&
      owner.keyId === state.grant.keyId,
    'current-authority',
  );
  const context = legacyHandoverContext(core, {
    owner,
    authority: state.authority,
    policy: state.policy,
    work: state.grant.payload.work,
    target: state.target,
    paths: state.paths,
    now,
  });
  const plan = core.verifyUnacceptedHandover({
    original,
    current,
    grant: state.grant,
    context,
  });
  need(plan.identity === proof.plan, 'bound-original-proof');
  if (proof.previousReference) {
    need(core.distributionAPI.unacceptedHandoverRevision === 1, 'revision-core');
    const history = await store.get(proof.previousReference);
    need(
      history.kind === 'native-handover-preexecution-history' &&
        history.version === 1 &&
        history.state.identity === state.handoverPreviousIdentity,
      'historical-state',
    );
    const previous = await verifyNativeLegacyHandover(core, {
      root,
      store,
      state: history.state,
      now: history.state.grant.payload.validFrom,
      depth: depth + 1,
    });
    core.verifyUnacceptedHandoverRevision({
      previous: { ...previous, state: history.state },
      current,
      original,
    });
  } else need(!state.handoverPreviousIdentity, 'historical-state-required');
  return { proof, original, current, plan, context, grant: state.grant };
}

export function legacyHandoverContext(
  core,
  { owner, authority, policy, work, target, paths, now = Date.now() },
) {
  return {
    now,
    trustedKeys: new Map([[owner.keyId, owner.publicKey]]),
    revocation: {
      id: owner.keyId,
      available: true,
      revokedRecords: owner.revokedRecords ?? [],
    },
    expectedAuthority: core.authoritySnapshot(authority),
    expectedPolicy: policy,
    expectedWork: work,
    request: {
      repository: {
        id: target.repositoryId,
        name: target.repository,
        visibility: 'public',
      },
      issueId: target.issueId,
      number: target.number,
      path: paths[0],
      operation: 'readiness',
    },
  };
}

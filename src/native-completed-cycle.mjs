import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { readNativeContinuationCustody } from './native-continuation-custody.mjs';
import { verifyNativeLegacyHandover } from './native-legacy-handover.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-completed-cycle:' + reason);
};
const same = (core, a, b) => core.canonicalJSON(a) === core.canonicalJSON(b);

// Terminal history is independently verified as history. This entry returns no
// Grant, execution context or permission to run on a different installation.
export async function readCompletedNativeCycle(core, { root, store, state }) {
  need(
    state.closed &&
      state.fanout?.outcome === 'discovered' &&
      state.completion &&
      core.nextDeliveryStage(state.receipts) === 'fanout',
    'terminal-required',
  );
  const owner = await readNativeOwnerPolicy(core, { root });
  const value = state.continuation
    ? await readNativeContinuationCustody(core, { store, state })
    : null;
  const approval = value?.approval ?? state.grant;
  const authority = value?.parent.authority ?? state.authority;
  const context = {
    now: approval.payload.validFrom,
    trustedKeys: new Map([[owner.keyId, owner.publicKey]]),
    revocation: {
      id: owner.keyId,
      available: true,
      revokedRecords: owner.revokedRecords ?? [],
    },
    expectedAuthority: core.authoritySnapshot(authority),
    expectedPolicy: approval.payload.policy,
    expectedWork: approval.payload.work,
    request: {
      repository: {
        id: state.target.repositoryId,
        name: state.target.repository,
        visibility: 'public',
      },
      issueId: state.target.issueId,
      number: state.target.number,
      path: approval.payload.permissions[0].paths[0],
      operation: 'mark-delivered',
    },
  };
  need(
    owner.memberId === state.member.id &&
      approval.keyId === owner.keyId &&
      core.verifyGrant(approval, context).ok,
    'historical-approval',
  );
  let binding;
  if (value) {
    const checked = core.verifyMergedContinuation(approval, { ...value, context });
    binding = checked.binding;
    for (const source of [value.parent, value.repair?.retained].filter(Boolean))
      for (const [stage, receipt] of Object.entries(source.receipts))
        need(
          same(core, state.receipts[stage], receipt) &&
            same(core, state.expectations[stage], source.expectations[stage]),
          'historical-rewrite',
        );
    for (const stage of checked.remaining)
      need(
        same(core, state.receipts[stage].execution.continuation, checked.execution),
        'execution-provenance',
      );
  } else {
    binding = {
      authority: authority.identity,
      grant: core.contentIdentity(approval),
      policy: core.contentIdentity(state.policy),
      core: state.core,
      runtime: state.policy.runtime,
      repositoryId: state.target.repositoryId,
      issueId: state.target.issueId,
      scopeHash: core.bodyDigest(state.scopeBody),
      shapeHash: core.bodyDigest(state.shapeBody),
    };
    if (state.legacyHandover) {
      const legacy = await verifyNativeLegacyHandover(core, {
        root,
        store,
        state,
        now: context.now,
      });
      core.checkUnacceptedHandoverFrame(legacy, {
        ...state,
        evidenceCurrent: state.expectations,
      });
    }
  }
  const completed = core.verifyCompletion(state.completion, state.completionCurrent);
  for (const [stage, receipt] of Object.entries(completed.payload.receipts)) {
    need(
      same(core, receipt, state.receipts[stage]) &&
        same(core, state.expectations[stage], state.completionCurrent[stage]),
      'completion-rewrite',
    );
    for (const key of Object.keys(binding).filter((key) => key !== 'instructions'))
      need(receipt.binding[key] === binding[key], 'completion-binding');
  }
  return {
    identity: completed.identity,
    approval: core.contentIdentity(approval),
    readOnly: true,
  };
}

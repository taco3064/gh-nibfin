// The original state is retained as a content record before an atomic reapproval.
// Existing immutable records stay in place, including the original conversation.
export async function reapproveNativeCycle(
  core,
  { store, previous, value, retained },
) {
  await store.update(async (before) => {
    if (
      before.identity !== previous.state.identity ||
      value.handoverPreviousIdentity !== before.identity
    )
      throw Error('native-cycle-reapproval:compare-and-swap');
    core.verifyUnacceptedHandoverPrefix({ ...previous, state: before });
    const records = { ...before.records };
    for (const payload of retained) {
      const reference = await store.retain(payload, { detached: true });
      records[reference.slice(14)] = payload.kind ?? 'retained';
    }
    return {
      ...before,
      ...value,
      records,
      createdAt: before.createdAt,
      reapprovedAt: Date.now(),
    };
  });
  return store;
}

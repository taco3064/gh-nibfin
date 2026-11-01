const need = (ok, reason) => {
  if (!ok) throw Error('native-handover:' + reason);
};
const same = (core, a, b) => core.canonicalJSON(a) === core.canonicalJSON(b);

// Read historical data only. Execution authority is checked by its caller.
export async function readNativeContinuationCustody(core, { store, state }) {
  const value = state.continuation;
  need(value?.version === 1, 'original-continuation');
  const original = await store.get(value.original);
  need(original.kind === 'native-original-cycle', 'original-state-custody');
  const manifest = Buffer.from(original.manifest, 'base64');
  const bytes = Buffer.from(original.bytes, 'base64');
  need(core.byteDigest(manifest) === value.parent.archive, 'original-manifest');
  const entry = JSON.parse(manifest).files.find((f) => f.path === original.path);
  need(
    entry && core.byteDigest(bytes) === entry.sha256 && bytes.length === entry.bytes,
    'original-state-bytes',
  );
  const originalState = JSON.parse(bytes);
  const { identity, ...body } = originalState;
  need(identity === value.parent.state, 'original-state-custody');
  need(identity === core.contentIdentity(body), 'original-state');
  for (const stage of Object.keys(value.parent.receipts))
    need(
      same(core, state.attempts[stage], originalState.attempts[stage]),
      'historical-attempt-rewrite',
    );
  return value;
}

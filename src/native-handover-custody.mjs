import fs from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';

const need = (ok, reason) => {
  if (!ok) throw Error('native-handover-custody:' + reason);
};

/** Validate actual portable Git objects and original Writer conversations.
 * This restores only private disposable proof copies; project code never runs.
 */
export async function verifyNativeHandoverCustody(core, { archive, cycle }) {
  const { state } = cycle,
    records = new Map(),
    writers = [];
  for (const path of cycle.records) {
    const value = await archive.readJSON(path),
      { identity, ...body } = value;
    need(
      value.version === 1 &&
        core.contentIdentity(body) === identity &&
        path.endsWith('/records/' + identity + '.json') &&
        ['retained', 'operation-record', 'completion-record'].includes(value.kind),
      'record-integrity',
    );
    records.set(identity, {
      path,
      kind: value.kind,
      payloadKind: value.payload?.kind ?? 'retained',
    });
    const payload = value.payload;
    if (
      payload?.kind === 'native-role-session' &&
      payload.metadata?.role === 'deliver' &&
      payload.metadata.execution === state.receipts.deliver.execution.id
    ) {
      need(
        payload.metadata.memberId === state.member.id &&
          payload.metadata.id === payload.snapshot?.session,
        'writer-identity',
      );
      writers.push({ reference: 'native-record:' + identity, payload });
    }
  }
  function references(value, depth = 0) {
    need(depth <= 128, 'reference-depth');
    if (typeof value === 'string' && /^native-record:[a-f0-9]{64}$/.test(value))
      need(records.has(value.slice(14)), 'missing-original-blob');
    else if (value && typeof value === 'object')
      for (const child of Object.values(value)) references(child, depth + 1);
  }
  references(state);
  for (const [identity, kind] of Object.entries(state.records))
    need(
      records.get(identity)?.kind === 'retained' &&
        records.get(identity).payloadKind === kind,
      'retained-record-index',
    );
  for (const [name, kind] of [
    ['operationBlobs', 'operation-record'],
    ['completionBlobs', 'completion-record'],
  ])
    for (const identity of Object.values(state[name]))
      need(records.get(identity)?.kind === kind, 'operation-blob-index');
  need(writers.length > 0 && writers.length <= 1000, 'original-writer-required');
  need(
    new Set(writers.map((w) => w.payload.metadata.id)).size === 1,
    'ambiguous-original-writer',
  );
  const temporary = await fs.mkdtemp(join(tmpdir(), 'nibfin-handover-proof-'));
  let proof;
  try {
    const workspace = core.importWorkspace(
      Buffer.from(core.canonicalJSON(state.workspace)),
      { expected: state.workspaceExpected },
    );
    need(
      core.canonicalJSON(workspace.candidate) === core.canonicalJSON(state.candidate),
      'original-candidate',
    );
    const root = join(temporary, 'workspace');
    await fs.mkdir(root, { mode: 0o700 });
    core.materializeWorkspace(workspace, { root, expected: state.workspaceExpected });
    const sessions = [];
    for (const [index, { reference, payload }] of writers.entries()) {
      const directory = join(temporary, 'session-' + index);
      await fs.mkdir(directory, { mode: 0o700 });
      const result = core.restoreRescueSession({
        snapshot: payload.snapshot,
        directory,
        expected: {
          memberId: state.member.id,
          role: 'deliver',
          session: payload.metadata.id,
          execution: payload.metadata.execution,
          adapter: 'codex',
        },
      });
      sessions.push({ reference, session: result.session, digest: result.digest });
    }
    proof = { records: records.size, candidate: workspace.candidate.tree, sessions };
  } finally {
    await fs.rm(temporary, { recursive: true });
  }
  return { ...proof, temporaryProofRemoved: true };
}

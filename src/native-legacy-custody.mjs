import fs from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { tmpdir } from 'node:os';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { retainUnacceptedWorkspace } from './native-unaccepted-work.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-legacy-custody:' + reason);
};
const sha = (bytes) => createHash('sha256').update(bytes).digest('hex');
const oid = (value) => typeof value === 'string' && /^[a-f0-9]{40}$/.test(value);
const uuid = (value) =>
  typeof value === 'string' &&
  /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);

/** Historical data is never authority. Independently reconstruct inert Git and
 * conversation bytes before the current Owner can approve their reuse. No old
 * verdict, configuration, hook, credential or service is executed or imported.
 */
export async function inspectLegacyNativeCustody(
  core,
  { archive, prefix, repository, issueId, number, memberId },
) {
  need(
    /^g[1-9][0-9]*$/.test(prefix) &&
      /^[1-9][0-9]*$/.test(number) &&
      /^[1-9][0-9]*$/.test(memberId),
    'target',
  );
  const statePath = prefix + '/state.json';
  const source = await archive.readJSON(statePath);
  need(
    source.payload && sha(core.canonicalJSON(source.payload)) === source.hash,
    'legacy-state-integrity',
  );
  const legacy = source.payload;
  const unit = legacy.issues?.[issueId];
  const candidate = unit?.candidate;
  const delivered = unit?.delivery;
  need(
    unit &&
      candidate?.repository === repository &&
      oid(candidate.base) &&
      oid(candidate.tree) &&
      Number.isSafeInteger(unit.repairs) &&
      unit.repairs >= 0 &&
      delivered?.subject === issueId &&
      uuid(delivered.executionId),
    'original-work',
  );
  need(
    typeof unit.shape?.body === 'string' &&
      sha(JSON.stringify(unit.shape.body)) === unit.shape.bodyHash &&
      /^[a-f0-9]{64}$/.test(delivered.deliveryInputs?.scopeHash),
    'original-requirements',
  );
  need(
    legacy.pending?.operation === 'accept' &&
      legacy.pending.issueId === issueId &&
      legacy.pending.repository === repository,
    'unaccepted-boundary',
  );
  const metadata = archive.metadata[number];
  need(
    metadata?.status === 'UNACCEPTED' &&
      metadata.candidateBase === candidate.base &&
      metadata.candidateTree === candidate.tree &&
      uuid(metadata.originalSession),
    'archive-index',
  );
  const sessionPaths = [...archive.files.keys()].filter(
    (path) =>
      path.startsWith(prefix + '/session/') &&
      path.endsWith('.jsonl') &&
      path.split('/').at(-1).includes(metadata.originalSession),
  );
  need(sessionPaths.length === 1, 'original-session');
  const conversation = await archive.readBytes(sessionPaths[0]);
  const events = conversation.toString('utf8').split('\n').filter(Boolean);
  need(events.length > 0 && events.length <= 100000, 'session-event-limit');
  const parsed = events.map((line) => JSON.parse(line));
  const first = parsed[0];
  need(
    first.type === 'session_meta' &&
      first.payload?.id === metadata.originalSession &&
      first.payload.cwd === '/work' &&
      parsed.every(
        (event) =>
          event.type !== 'session_meta' ||
          event.payload?.id === metadata.originalSession,
      ),
    'session-identity',
  );
  const temporary = await fs.mkdtemp(join(tmpdir(), 'nibfin-legacy-proof-'));
  let result;
  try {
    const workspace = join(temporary, 'work');
    const gitDirectory = join(workspace, '.git');
    const sessionDirectory = join(temporary, 'session');
    await fs.mkdir(gitDirectory, { recursive: true, mode: 0o700 });
    await fs.mkdir(join(gitDirectory, 'refs'), { mode: 0o700 });
    await fs.mkdir(sessionDirectory, { mode: 0o700 });
    const workPrefix = prefix + '/workspace/work/';
    const gitPrefix = prefix + '/workspace/repository.git/';
    let files = 0;
    for (const path of archive.files.keys()) {
      let destination;
      if (path.startsWith(workPrefix)) {
        const relative = path.slice(workPrefix.length);
        need(!relative.split('/').includes('.git'), 'embedded-git');
        destination = join(workspace, relative);
      } else if (path.startsWith(gitPrefix)) {
        const relative = path.slice(gitPrefix.length);
        if (relative !== 'index' && !relative.startsWith('objects/')) continue;
        need(
          relative === 'index' ||
            /^objects\/[a-f0-9]{2}\/[a-f0-9]{38}$/.test(relative) ||
            /^objects\/pack\/pack-[a-f0-9]{40}\.(?:pack|idx|rev)$/.test(relative),
          'git-object-path',
        );
        destination = join(gitDirectory, relative);
      } else continue;
      const bytes = await archive.readBytes(path);
      await fs.mkdir(dirname(destination), { recursive: true, mode: 0o700 });
      const mode = archive.files.get(path).sourceMode & 0o700;
      await fs.writeFile(destination, bytes, { flag: 'wx', mode });
      await fs.chmod(destination, mode);
      files++;
    }
    await fs.writeFile(join(gitDirectory, 'HEAD'), candidate.base + '\n', {
      flag: 'wx',
      mode: 0o600,
    });
    await fs.writeFile(
      join(gitDirectory, 'config'),
      '[core]\nrepositoryformatversion = 0\nbare = false\n',
      { flag: 'wx', mode: 0o600 },
    );
    const git = (args) =>
      execFileSync(
        '/usr/bin/git',
        [
          '-c',
          'core.hooksPath=/dev/null',
          '-c',
          'core.fsmonitor=false',
          '--git-dir=' + gitDirectory,
          '--work-tree=' + workspace,
          ...args,
        ],
        {
          cwd: workspace,
          env: {
            PATH: '/usr/bin:/bin',
            HOME: '/nonexistent',
            LANG: 'C',
            GIT_CONFIG_GLOBAL: '/dev/null',
            GIT_CONFIG_NOSYSTEM: '1',
            GIT_TERMINAL_PROMPT: '0',
            GIT_NO_REPLACE_OBJECTS: '1',
            GIT_OPTIONAL_LOCKS: '0',
          },
          timeout: 30000,
          maxBuffer: 16 * 1024 * 1024,
          stdio: ['ignore', 'pipe', 'pipe'],
        },
      ).toString('utf8');
    need(
      git(['cat-file', '-t', candidate.base]).trim() === 'commit',
      'original-base',
    );
    need(git(['write-tree']).trim() === candidate.tree, 'original-index');
    git(['diff', '--no-ext-diff', '--no-textconv', '--exit-code', candidate.tree]);
    const paths = git([
      'diff',
      '--no-ext-diff',
      '--no-textconv',
      '--name-only',
      '-z',
      candidate.base,
      candidate.tree,
    ])
      .split('\0')
      .filter(Boolean)
      .sort();
    need(
      core.canonicalJSON(paths) === core.canonicalJSON([...candidate.paths].sort()),
      'candidate-paths',
    );
    // Git may refresh disposable stat/cache-tree fields during inspection. The
    // transferable index remains the exact archived bytes, independent of this
    // proof directory's inode numbers, timestamps or optional lock behavior.
    await fs.writeFile(
      join(gitDirectory, 'index'),
      await archive.readBytes(gitPrefix + 'index'),
      { mode: 0o600 },
    );
    await fs.writeFile(
      join(sessionDirectory, sessionPaths[0].split('/').at(-1)),
      conversation,
      { flag: 'wx', mode: 0o600 },
    );
    const snapshot = core.captureRescueSession({
      directory: sessionDirectory,
      memberId,
      role: 'deliver',
      session: metadata.originalSession,
      execution: delivered.executionId,
      adapter: 'codex',
    });
    let workspaceSnapshot;
    await retainUnacceptedWorkspace({
      directory: workspace,
      execution: { id: delivered.executionId },
      retain: async (value) => {
        workspaceSnapshot = value;
        return 'inspected-only';
      },
    });
    result = {
      version: 1,
      kind: 'native-legacy-unaccepted-custody',
      status: 'UNACCEPTED',
      archive: archive.identity,
      state: archive.files.get(statePath).sha256,
      originalStateHash: source.hash,
      target: { repository, issueId, number },
      memberId,
      repairs: unit.repairs,
      originalScope: delivered.deliveryInputs.scopeHash,
      originalShape: sha(JSON.stringify(unit.shape.body)),
      shape: structuredClone(unit.shape),
      candidate: structuredClone(candidate),
      execution: delivered.executionId,
      session: metadata.originalSession,
      conversation: sha(conversation),
      files,
      snapshot,
      workspaceSnapshot,
    };
  } finally {
    await fs.rm(temporary, { recursive: true });
  }
  return { ...result, temporaryProofRemoved: true };
}

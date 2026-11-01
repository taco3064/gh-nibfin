import fs from 'node:fs/promises';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';

const sha = (bytes) => createHash('sha256').update(bytes).digest('hex');

export async function legacyCustodyFixture(core, context) {
  const directory = await fs.mkdtemp(join(context.root, 'legacy-custody-'));
  const work = join(directory, 'original');
  await fs.mkdir(work, { mode: 0o700 });
  const git = (args) =>
    execFileSync(
      '/usr/bin/git',
      [
        '-c',
        'core.hooksPath=/dev/null',
        '-c',
        'core.fsmonitor=false',
        '-c',
        'user.name=Custody test',
        '-c',
        'user.email=test@example.invalid',
        ...args,
      ],
      {
        cwd: work,
        env: {
          PATH: '/usr/bin:/bin',
          HOME: '/nonexistent',
          LANG: 'C',
          GIT_CONFIG_GLOBAL: '/dev/null',
          GIT_CONFIG_NOSYSTEM: '1',
        },
        stdio: ['ignore', 'pipe', 'pipe'],
        encoding: 'utf8',
      },
    ).trim();
  git(['init', '--quiet']);
  await fs.writeFile(join(work, 'a.txt'), 'original base\n');
  git(['add', 'a.txt']);
  git(['commit', '--quiet', '-m', 'base']);
  const base = git(['rev-parse', 'HEAD']);
  await fs.writeFile(join(work, 'a.txt'), 'retained unaccepted edit\n');
  git(['add', 'a.txt']);
  const tree = git(['write-tree']);
  const issueId = 'I_123';
  const number = '55';
  const repository = 'example/project';
  const session = '00000000-0000-4000-8000-000000000001';
  const execution = '00000000-0000-4000-8000-000000000002';
  const candidate = { repository, base, tree, paths: ['a.txt'] };
  const unit = {
    repairs: 0,
    candidate,
    shape: {
      body:
        'Original plan\n\n```kairo-candidate\n' + '{"artifacts":[],"hooks":[]}\n```',
    },
    delivery: {
      subject: issueId,
      executionId: execution,
      deliveryInputs: { scopeHash: sha(JSON.stringify('Original Scope')) },
    },
  };
  unit.shape.bodyHash = core.bodyDigest(unit.shape.body);
  const record = (stage, outcome, id) => ({
    stage,
    outcome,
    failureClass: 'none',
    subject: issueId,
    executionId: id,
    resultHash: core.contentIdentity({ stage }),
    artifactIdentities: {
      scopeHash: unit.delivery.deliveryInputs.scopeHash,
      shapeHash: unit.shape.bodyHash,
    },
  });
  unit.shape.author = record('shape', 'ready', 'original-author');
  unit.shape.validator = record('shape-validator', 'approved', 'original-validator');
  unit.shape.publication = {
    bodyHash: unit.shape.bodyHash,
    scopeHash: unit.delivery.deliveryInputs.scopeHash,
    decisionId: 'd'.repeat(64),
  };
  const payload = {
    pending: { operation: 'accept', issueId, repository },
    issues: { [issueId]: unit },
  };
  const files = new Map();
  const bytes = new Map();
  const put = (path, value) => {
    const data = Buffer.isBuffer(value) ? value : Buffer.from(value);
    bytes.set(path, data);
    files.set(path, { bytes: data.length, sha256: sha(data), sourceMode: 0o600 });
  };
  const seal = () =>
    put(
      'g55/state.json',
      JSON.stringify({
        payload,
        hash: sha(core.canonicalJSON(payload)),
      }),
    );
  seal();
  async function walk(path, relative = '') {
    for (const entry of await fs.readdir(path, { withFileTypes: true })) {
      const child = relative ? relative + '/' + entry.name : entry.name;
      if (entry.isDirectory()) await walk(join(path, entry.name), child);
      else if (child.startsWith('.git/'))
        put(
          'g55/workspace/repository.git/' + child.slice(5),
          await fs.readFile(join(path, entry.name)),
        );
      else
        put('g55/workspace/work/' + child, await fs.readFile(join(path, entry.name)));
    }
  }
  await walk(work);
  const sessionPath = 'g55/session/' + session + '.jsonl';
  put(
    sessionPath,
    JSON.stringify({
      type: 'session_meta',
      payload: { id: session, cwd: '/work' },
    }) + '\n',
  );
  const archive = {
    identity: 'a'.repeat(64),
    files,
    metadata: {
      55: {
        status: 'UNACCEPTED',
        originalSession: session,
        candidateBase: base,
        candidateTree: tree,
      },
    },
    readBytes: async (path) => {
      if (!bytes.has(path)) throw Error('Unlisted fixture file');
      return Buffer.from(bytes.get(path));
    },
    readJSON: async (path) => JSON.parse(bytes.get(path)),
  };
  return {
    archive,
    payload,
    unit,
    candidate,
    sessionPath,
    put,
    seal,
    directory,
    input: { archive, prefix: 'g55', repository, issueId, number, memberId: '7' },
  };
}

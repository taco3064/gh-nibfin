import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { userInfo } from 'node:os';
import { withCurrentCore } from '../../src/current-core.mjs';
import { prepareNativeOwnerPolicy } from '../../src/native-owner-policy.mjs';
import { prepareNativeCycle } from '../../src/native-cycle-policy.mjs';
import { nativeExecutionInputs } from '../../src/native-execution-inputs.mjs';
import { ensurePrivateDirectory, writePrivate } from '../../src/local-state.mjs';
const repository = 'example/project',
  target = {
    version: 1,
    repository,
    repositoryId: '100',
    ownerId: '7',
    ownerType: 'User',
  };
const configuration = {
    version: 2,
    id: 'member-default',
    capacity: 2,
    targets: [target],
  },
  member = { id: '7', login: 'member' };

// Explicit GitHub observation doubles. Actual private records, signatures,
// authority, authorization, dependency and selection owners are installed Core.
export async function nativeCycleFixture(
  core,
  context,
  { name = 'native-fixture' } = {},
) {
  assert.match(name, /^[a-z][a-z0-9-]*$/);
  const base = join(context.root, name),
    root = join(base, 'control');
  await ensurePrivateDirectory(root);
  await ensurePrivateDirectory(join(base, 'member-auth'));
  await writePrivate(join(base, 'member-auth/auth.json'), { testOnly: true });
  const image = 'sha256:' + 'a'.repeat(64),
    runtimeProfile = 'linux-amd64-codex-node22-docker29-v1';
  const config = {
    version: 1,
    name: 'native-member',
    agent: 'codex',
    memberId: '7',
    capacity: 2,
    image,
    runtimeProfile,
  };
  await writePrivate(join(root, 'member-runtime.json'), {
    ...config,
    identity: core.contentIdentity(config),
  });
  const env = {
    schemaVersion: 1,
    memberLogin: 'shared-native-v1',
    name: 'native-member',
    agent: 'codex',
    githubId: '7',
    account: { uid: userInfo().uid, username: userInfo().username },
    loginSpace: join(base, 'member-auth'),
    runtime: {
      image,
      runtimeProfile,
      socket: '/run/user/' + userInfo().uid + '/docker.sock',
    },
    instances: [1, 2].map((n) => ({
      id: 'fixture-' + n,
      workspace: join(base, 'work-' + n),
      registration: join(base, 'reg-' + n),
      service: join(base, 'service-' + n),
    })),
  };
  await ensurePrivateDirectory(join(base, 'environments/native-member'));
  await writePrivate(join(base, 'environments/native-member/environment.json'), env);
  const issue = {
    id: '123',
    node_id: 'I_123',
    number: 55,
    title: 'Document native onboarding',
    state: 'open',
    state_reason: null,
    closed_at: null,
    updated_at: '2026-10-30T00:00:00Z',
    body:
      'Document the native Owner and Member setup.\n' +
      'No additional untracked or ignored Candidate artifacts are required.\n' +
      'Deployment is not required for this source-only change.',
    url: 'https://api.github.com/repos/' + repository + '/issues/55',
    repository_url: 'https://api.github.com/repos/' + repository,
    assignees: [member],
    labels: [{ id: '900', name: 'scope' }],
  };
  const repo = {
    id: '100',
    full_name: repository,
    private: false,
    visibility: 'public',
    default_branch: 'main',
    owner: { id: '7', type: 'User', login: 'example' },
    permissions: { admin: true },
    allow_rebase_merge: true,
  };
  let baseCommit = 'b'.repeat(40),
    treePaths = ['docs'],
    intercept;
  const workflow = 'name: CI\non: push\n';
  let denied = false;
  const transport = async (r) => {
    if (denied) return { status: 403, body: '{}' };
    if (intercept) {
      const value = await intercept(r);
      if (value) return value;
    }
    if (
      r.method === 'PATCH' &&
      r.endpoint === '/repos/' + repository + '/issues/55'
    ) {
      issue.body = JSON.parse(r.body).body;
      return { status: 200, body: JSON.stringify(issue) };
    }
    if (
      r.method === 'POST' &&
      r.endpoint === '/repos/' + repository + '/issues/55/labels'
    ) {
      if (!issue.labels.some((label) => label.name === 'shaped'))
        issue.labels.push({ id: '901', name: 'shaped' });
      return { status: 200, body: JSON.stringify(issue.labels) };
    }
    assert.equal(r.method, 'GET', 'unexpected fixture mutation');
    let data;
    if (r.endpoint === '/user' || r.endpoint === '/users/member') data = member;
    else if (r.endpoint === '/repos/' + repository) data = repo;
    else if (r.endpoint === '/repos/' + repository + '/issues/55') data = issue;
    else if (r.endpoint === '/repos/' + repository + '/issues/55/labels?per_page=100')
      data = issue.labels;
    else if (r.endpoint === '/repos/' + repository + '/issues/55/events?per_page=100')
      data = [];
    else if (r.endpoint.includes('/collaborators/member/permission'))
      data = { permission: 'admin', user: member };
    else if (r.endpoint.endsWith('/git/ref/heads/main'))
      data = { object: { type: 'commit', sha: baseCommit } };
    else if (r.endpoint.includes('/git/trees/'))
      data = {
        truncated: false,
        tree: treePaths.map((path) => ({ path, type: 'tree', sha: 'c'.repeat(40) })),
      };
    else if (r.endpoint.endsWith('/protection/required_status_checks'))
      data = { contexts: ['ci'], checks: [{ context: 'ci', app_id: '15368' }] };
    else if (r.endpoint.includes('/check-runs'))
      data = {
        total_count: 1,
        check_runs: [{ id: '2000', name: 'ci', app: { id: '15368' } }],
      };
    else if (r.endpoint.includes('/contents/.github/workflows/ci.yml'))
      data = {
        encoding: 'base64',
        content: Buffer.from(workflow).toString('base64'),
      };
    else if (r.endpoint.includes('/dependencies/blocked_by')) data = [];
    else if (r.endpoint === '/repos/' + repository + '/issues?state=all&per_page=100')
      data = [issue];
    else return { status: 404, body: '{}' };
    return { status: 200, body: JSON.stringify(data) };
  };
  const provider = core.createGithubProvider({ transport, attempts: 1 });
  await prepareNativeOwnerPolicy(core, { root, configuration, provider });
  return {
    root,
    issue,
    repo,
    workflow,
    transport,
    provider,
    context,
    configuration,
    member,
    deny: () => {
      denied = true;
    },
    setBase: (value) => {
      baseCommit = value;
    },
    setTree: (value) => {
      treePaths = value;
    },
    intercept: (value) => {
      intercept = value;
    },
    options: { root, configuration, repository, number: '55', provider, context },
  };
}

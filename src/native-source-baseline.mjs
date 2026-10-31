import { sha } from './local-state.mjs';
import {
  nativeSourceBody,
  sourceInstallationIdentity,
} from './native-source-contract.mjs';

const need = (value, code) => {
  if (!value) throw Error('native-source-baseline:' + code);
};
const commit = (value) => /^[a-f0-9]{40}$/.test(value ?? '');

// Recover only a source comparison baseline from an authenticated, protected
// installation PR. This supplies no runtime state, Grant, signing key or receipt.
export async function recoverManagedSourceBaseline(
  core,
  { repository, repositoryId, desired, observations, member, get },
) {
  const runtime = observations.get('.nibfin/runtime.json');
  if (runtime?.state !== 'present') return null;
  let policy;
  try {
    policy = JSON.parse(runtime.bytes);
  } catch {
    return null;
  }
  if (
    policy?.version !== 1 ||
    !commit(policy.producer) ||
    !/^[a-f0-9]{64}$/.test(policy.package?.coreSha256 ?? '')
  )
    return null;
  const main = (await get('/git/ref/heads/main')).object;
  need(main.type === 'commit' && commit(main.sha), 'main');
  const history = await get(
    '/commits?path=.nibfin%2Fruntime.json&sha=' + main.sha + '&per_page=1',
  );
  need(
    Array.isArray(history) && history.length === 1 && commit(history[0].sha),
    'source-history',
  );
  const source = history[0].sha;
  const linked = await get('/commits/' + source + '/pulls?per_page=100');
  need(Array.isArray(linked) && linked.length < 100, 'source-pr-list');
  const prs = linked.filter(
    (pr) =>
      pr.merged_at &&
      pr.merge_commit_sha === source &&
      pr.base?.ref === 'main' &&
      pr.base.repo?.id === repositoryId &&
      pr.user?.id === member.id,
  );
  need(prs.length === 1, 'protected-installation-pr');
  const pr = await get('/pulls/' + prs[0].number);
  need(
    pr.id === prs[0].id &&
      pr.merged === true &&
      pr.merge_commit_sha === source &&
      pr.base?.ref === 'main' &&
      pr.base.repo?.id === repositoryId &&
      pr.user?.id === member.id,
    'protected-installation-pr',
  );
  const files = {};
  for (const [name] of desired.files) {
    const value = await get('/contents/' + name + '?ref=' + source, {
      optional: true,
    });
    if (value === null) continue;
    need(
      value.type === 'file' &&
        value.path === name &&
        value.encoding === 'base64' &&
        typeof value.content === 'string' &&
        Number.isInteger(value.size) &&
        value.size >= 0 &&
        value.size <= 1048576,
      'source-file',
    );
    const bytes = Buffer.from(value.content.replace(/\s/g, ''), 'base64');
    need(bytes.length === value.size, 'source-bytes');
    files[name] = sha(bytes);
  }
  const identity = sourceInstallationIdentity(core, {
    repository,
    repositoryId,
    files,
  });
  need(pr.body === nativeSourceBody(identity), 'installation-manifest');
  need(files['.nibfin/runtime.json'] === sha(runtime.bytes), 'runtime-changed');
  for (const [name, body] of desired.files) {
    const observed = observations.get(name);
    need(observed && observed.state !== 'unknown', 'current-observation');
    need(
      observed.state === 'absent' ||
        observed.bytes.equals(Buffer.from(body)) ||
        files[name] === sha(observed.bytes),
      'user-modification-preserved',
    );
  }
  return {
    version: 1,
    kind: 'protected-managed-source-baseline',
    repository,
    repositoryId,
    source,
    installation: identity,
    pr: { id: pr.id, number: String(pr.number), url: pr.html_url },
    files,
  };
}

import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import {
  repository,
  repositoryId,
  sha256,
  need,
  commit,
  assetNames,
} from './release-contract.mjs';

export async function publishRelease({
  manifest,
  assets,
  run,
  request,
  record,
  immutabilityPolicy = 'api-preflight',
}) {
  need(
    ['api-preflight', 'repository-enforced'].includes(immutabilityPolicy),
    'policy',
  );
  const m = manifest,
    base = '/repos/' + repository;
  need(m?.format === 1 && m.package === 'gh-nibfin', 'manifest');
  need(m.repository === repository && m.repositoryId === repositoryId, 'repository');
  need(commit(m.source) && /^\d+\.\d+\.\d+$/.test(m.version), 'source');
  need(run?.repository === repository && run.repositoryId === repositoryId, 'run');
  need(run.ref === 'refs/heads/main' && run.sha === m.source, 'run-source');
  need(run.event === 'workflow_dispatch' && /^[1-9]\d*$/.test(run.id), 'run-event');
  need(/^[1-9]\d*$/.test(run.attempt), 'run-attempt');
  need(assets instanceof Map && assets.size === assetNames.length, 'assets');
  need(
    assetNames.every((name) => Buffer.isBuffer(assets.get(name))),
    'assets',
  );
  need(sha256(assets.get('gh-nibfin.tar.gz')) === m.archive.sha256, 'archive');
  need(assets.get('gh-nibfin.tar.gz').length === m.archive.size, 'archive-size');
  need(
    JSON.stringify(JSON.parse(assets.get('cli-release-manifest.json'))) ===
      JSON.stringify(m),
    'manifest-bytes',
  );
  const tag = 'v' + m.version;
  const body = JSON.stringify({
    format: 1,
    source: m.source,
    manifest: sha256(assets.get('cli-release-manifest.json')),
  });
  const expected = [...assets].map(([name, bytes]) => ({
    name,
    size: bytes.length,
    digest: 'sha256:' + sha256(bytes),
  }));
  let receipt = { status: 'checking', source: m.source, tag, run, assets: expected };
  record(receipt);
  const get = async (path, optional = false) => {
    const response = await request({ method: 'GET', path });
    if (optional && response.status === 404) return null;
    need(response.status === 200, 'readback-' + response.status);
    return response.data;
  };
  const write = async (method, path, data, bytes) => {
    receipt = { ...receipt, status: 'write-intent', operation: { method, path } };
    record(receipt);
    let response;
    try {
      response = await request({ method, path, data, bytes });
    } catch (error) {
      record({ ...receipt, status: 'unknown', cause: error.message });
      throw Error('cli-release:unknown-write-reconcile-before-retry');
    }
    if (![200, 201].includes(response.status)) {
      record({ ...receipt, status: 'failed', httpStatus: response.status });
      throw Error('cli-release:write-failed-reconcile-before-retry');
    }
    return response.data;
  };
  const target = async (prefix, name, optional = false) => {
    let object = (await get(prefix + '/git/ref/tags/' + name, optional))?.object;
    for (let n = 0; object && n < 4; n++) {
      need(commit(object.sha), 'tag-object');
      if (object.type === 'commit') return object.sha;
      need(object.type === 'tag', 'tag-type');
      object = (await get(prefix + '/git/tags/' + object.sha)).object;
    }
    need(!object, 'tag-depth');
    return null;
  };
  const coreBase = '/repos/taco3064/nibfin';
  const coreTag = 'v' + m.core.packageVersion;
  const coreRelease = await get(coreBase + '/releases/tags/' + coreTag);
  need(coreRelease.immutable === true && coreRelease.draft === false, 'core-release');
  need(coreRelease.prerelease === false, 'core-prerelease');
  need(coreRelease.target_commitish === m.core.producer, 'core-source');
  need((await target(coreBase, coreTag)) === m.core.producer, 'core-tag');
  const coreAssets = await get(
    coreBase + '/releases/' + coreRelease.id + '/assets?per_page=100',
  );
  need(
    m.core.assets.length === 3 &&
      m.core.assets.every(
        (a) =>
          coreAssets.filter((b) => b.name === a.name).length === 1 &&
          coreAssets.some(
            (b) =>
              b.name === a.name &&
              b.state === 'uploaded' &&
              b.size === a.size &&
              b.digest === 'sha256:' + a.sha256,
          ),
      ),
    'core-assets',
  );
  const repo = await get(base),
    branch = await get(base + '/branches/main');
  need(String(repo.id) === repositoryId && repo.full_name === repository, 'repo');
  need(repo.visibility === 'public' && branch.protected === true, 'protected-main');
  if (immutabilityPolicy === 'api-preflight') {
    need((await get(base + '/immutable-releases')).enabled === true, 'immutability');
  }
  const prs = await get(base + '/commits/' + m.source + '/pulls?per_page=100');
  need(
    prs.some(
      (p) =>
        p.merged_at &&
        p.merge_commit_sha === m.source &&
        p.base?.ref === 'main' &&
        String(p.base.repo?.id) === repositoryId,
    ),
    'merge',
  );
  const priorTag = await target(base, tag, true);
  need(!priorTag || priorTag === m.source, 'conflicting-tag');
  const matches = [];
  for (let page = 1; ; page++) {
    need(page <= 100, 'release-discovery');
    const list = await get(base + '/releases?per_page=100&page=' + page);
    need(Array.isArray(list) && list.length <= 100, 'release-list');
    matches.push(...list.filter((r) => r.tag_name === tag));
    if (list.length < 100) break;
  }
  need(matches.length <= 1, 'ambiguous-release');
  let release = matches[0];
  const identity = (r) =>
    need(
      r &&
        Number.isSafeInteger(r.id) &&
        r.id > 0 &&
        r.tag_name === tag &&
        r.target_commitish === m.source &&
        r.body === body &&
        r.prerelease === false,
      'conflicting-release',
    );
  if (!release) {
    need(!priorTag, 'unowned-tag');
    release = await write('POST', base + '/releases', {
      tag_name: tag,
      target_commitish: m.source,
      name: tag,
      body,
      draft: true,
      prerelease: false,
    });
    identity(release);
    need(release.draft === true, 'draft');
  }
  const id = release.id;
  identity(release);
  release = await get(base + '/releases/' + id);
  identity(release);
  need(release.id === id, 'release-id');
  const readAssets = () => get(base + '/releases/' + id + '/assets?per_page=100');
  const checkAssets = (list, complete) => {
    need(Array.isArray(list) && list.length <= expected.length, 'asset-inventory');
    const seen = new Set();
    for (const a of list) {
      const e = expected.find((item) => item.name === a.name);
      need(e && !seen.has(a.name) && a.state === 'uploaded', 'asset-name');
      need(a.size === e.size && a.digest === e.digest, 'asset-content');
      seen.add(a.name);
    }
    if (complete) need(seen.size === expected.length, 'missing-asset');
  };
  let existing = await readAssets();
  checkAssets(existing, !release.draft);
  if (release.draft) {
    for (const a of expected) {
      if (existing.some((item) => item.name === a.name)) continue;
      const upload = 'https://uploads.github.com' + base + '/releases/' + id;
      await write(
        'POST',
        upload + '/assets?name=' + a.name,
        null,
        assets.get(a.name),
      );
      existing = await readAssets();
      checkAssets(existing, false);
      need(
        existing.some((item) => item.name === a.name),
        'upload-readback',
      );
    }
    checkAssets(existing, true);
    // The workflow uses repository-enforced immutability, confirmed on readback.
    await write('PATCH', base + '/releases/' + id, { draft: false });
  }
  release = await get(base + '/releases/tags/' + tag);
  identity(release);
  need(release.id === id && release.immutable === true, 'immutable-readback');
  need(release.draft === false && !!release.published_at, 'published-readback');
  need((await target(base, tag)) === m.source, 'published-tag');
  checkAssets(await readAssets(), true);
  const result = {
    ...receipt,
    status: 'published',
    release: { id, url: release.html_url, immutable: true },
  };
  record(result);
  return result;
}

export function githubRequest(token) {
  need(typeof token === 'string' && token.length > 0, 'token');
  return async ({ method, path, data, bytes }) => {
    const url = path.startsWith('/') ? 'https://api.github.com' + path : path;
    const own = 'https://api.github.com/repos/' + repository;
    const core = 'https://api.github.com/repos/taco3064/nibfin/';
    const upload = 'https://uploads.github.com/repos/' + repository + '/releases/';
    need(
      url === own ||
        url.startsWith(own + '/') ||
        (method === 'GET' && url.startsWith(core)) ||
        url.startsWith(upload),
      'origin',
    );
    const response = await fetch(url, {
      method,
      redirect: 'error',
      signal: AbortSignal.timeout(30000),
      headers: {
        Authorization: 'Bearer ' + token,
        Accept: 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28',
        'Content-Type': bytes ? 'application/octet-stream' : 'application/json',
      },
      body: bytes ?? (data ? JSON.stringify(data) : undefined),
    });
    return { status: response.status, data: await response.json() };
  };
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  const env = process.env;
  const manifest = JSON.parse(readFileSync('out/cli-release-manifest.json'));
  try {
    await publishRelease({
      manifest,
      assets: new Map(assetNames.map((n) => [n, readFileSync('out/' + n)])),
      run: {
        repository: env.GITHUB_REPOSITORY,
        repositoryId: env.GITHUB_REPOSITORY_ID,
        ref: env.GITHUB_REF,
        sha: env.GITHUB_SHA,
        event: env.GITHUB_EVENT_NAME,
        id: env.GITHUB_RUN_ID,
        attempt: env.GITHUB_RUN_ATTEMPT,
      },
      request: githubRequest(env.GH_TOKEN),
      immutabilityPolicy: env.NIBFIN_RELEASE_IMMUTABILITY_POLICY ?? 'api-preflight',
      record: (r) =>
        writeFileSync(
          'out/publication-result.json',
          JSON.stringify(r, null, 2) + '\n',
        ),
    });
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}

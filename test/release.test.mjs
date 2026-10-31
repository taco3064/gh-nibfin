import { test } from 'node:test';
import assert from 'node:assert/strict';
import { publishRelease } from '../tooling/release-publish.mjs';
import { repository, repositoryId, sha256 } from '../tooling/release-contract.mjs';

function fixture({ fault, published = false, conflicting = false } = {}) {
  const source = 'a'.repeat(40),
    producer = 'b'.repeat(40);
  const bytes = Buffer.from('archive');
  const coreAssets = [
    'nibfin-core.tgz',
    'release-manifest.json',
    'compatibility.json',
  ].map((name) => ({ name, size: 7, sha256: 'c'.repeat(64) }));
  const manifest = {
    format: 1,
    package: 'gh-nibfin',
    repository,
    repositoryId,
    version: '0.1.1',
    source,
    core: { producer, packageVersion: '0.1.1', assets: coreAssets },
    archive: { size: bytes.length, sha256: sha256(bytes) },
  };
  const assets = new Map([
    ['gh-nibfin.tar.gz', bytes],
    ['cli-release-manifest.json', Buffer.from(JSON.stringify(manifest))],
  ]);
  const body = JSON.stringify({
    format: 1,
    source,
    manifest: sha256(assets.get('cli-release-manifest.json')),
  });
  let release =
    published || conflicting
      ? {
          id: 2,
          tag_name: 'v0.1.1',
          target_commitish: source,
          body: conflicting ? 'foreign' : body,
          draft: false,
          immutable: true,
          prerelease: false,
          published_at: '2026-11-01T00:00:00Z',
        }
      : null;
  let uploaded = published
    ? [...assets].map(([name, b]) => ({
        name,
        state: 'uploaded',
        size: b.length,
        digest: 'sha256:' + sha256(b),
      }))
    : [];
  let tagged = published;
  const writes = [],
    records = [];
  const base = '/repos/' + repository,
    core = '/repos/taco3064/nibfin';
  const request = async ({ method, path, data, bytes: upload }) => {
    if (method !== 'GET') {
      writes.push({ method, path });
      if (fault === 'unknown') throw Error('connection lost after write');
      if (method === 'PATCH') {
        release = {
          ...release,
          draft: false,
          immutable: fault !== 'mutable',
          published_at: '2026-11-01T00:00:00Z',
        };
        tagged = true;
      } else if (path === base + '/releases') release = { id: 2, ...data };
      else {
        const name = new URL(path).searchParams.get('name');
        uploaded.push({
          name,
          state: 'uploaded',
          size: upload.length,
          digest: 'sha256:' + sha256(upload),
        });
      }
      return { status: method === 'POST' ? 201 : 200, data: release };
    }
    let result;
    if (path === core + '/releases/tags/v0.1.1')
      result = {
        id: 1,
        immutable: fault !== 'core',
        draft: false,
        prerelease: false,
        target_commitish: producer,
      };
    else if (path === core + '/git/ref/tags/v0.1.1')
      result = { object: { type: 'commit', sha: producer } };
    else if (path === core + '/releases/1/assets?per_page=100')
      result = coreAssets.map((a) => ({
        ...a,
        state: 'uploaded',
        digest: 'sha256:' + a.sha256,
      }));
    else if (path === base)
      result = {
        id: Number(repositoryId),
        full_name: repository,
        visibility: 'public',
      };
    else if (path === base + '/branches/main') result = { protected: true };
    else if (path.includes('/commits/'))
      result = [
        {
          merged_at: 'today',
          merge_commit_sha: source,
          base: { ref: 'main', repo: { id: Number(repositoryId) } },
        },
      ];
    else if (path === base + '/git/ref/tags/v0.1.1') {
      if (!tagged) return { status: 404 };
      result = { object: { type: 'commit', sha: source } };
    } else if (path === base + '/releases?per_page=100&page=1')
      result = release ? [release] : [];
    else if (path.endsWith('/assets?per_page=100')) result = uploaded;
    else if (path === base + '/releases/2' || path.includes('/releases/tags/'))
      result = release;
    else throw Error('unexpected request: ' + path);
    return { status: 200, data: structuredClone(result) };
  };
  return {
    writes,
    records,
    publish: () =>
      publishRelease({
        manifest,
        assets,
        request,
        run: {
          repository,
          repositoryId,
          ref: 'refs/heads/main',
          sha: source,
          event: 'workflow_dispatch',
          id: '1',
          attempt: '1',
        },
        record: (r) => records.push(r),
      }),
  };
}
test('publishes exact assets and reconciles immutable readback', async () => {
  const fresh = fixture();
  assert.equal((await fresh.publish()).status, 'published');
  assert.equal(fresh.writes.length, 4);
  const prior = fixture({ published: true });
  assert.equal((await prior.publish()).status, 'published');
  assert.equal(prior.writes.length, 0);
});
test('rejects foreign existing release before writes', async () => {
  const f = fixture({ conflicting: true });
  await assert.rejects(f.publish(), /conflicting-release/);
  assert.equal(f.writes.length, 0);
});
test('missing integrated immutable Core prevents publication', async () => {
  const f = fixture({ fault: 'core' });
  await assert.rejects(f.publish(), /core-release/);
  assert.equal(f.writes.length, 0);
});
test('uncertain writes retain cause and are not retried', async () => {
  const f = fixture({ fault: 'unknown' });
  await assert.rejects(f.publish(), /unknown-write-reconcile/);
  assert.equal(f.writes.length, 1);
  assert.equal(f.records.at(-1).status, 'unknown');
  assert.equal(f.records.at(-1).cause, 'connection lost after write');
});
test('mutable remote release cannot be reported as success', async () => {
  const f = fixture({ fault: 'mutable' });
  await assert.rejects(f.publish(), /immutable-readback/);
  assert.notEqual(f.records.at(-1).status, 'published');
});

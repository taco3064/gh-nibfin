import { writeFileSync, mkdirSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { version } from '../src/version.mjs';
import {
  currentPin,
  currentProducer,
  verifyCurrentCore,
} from '../src/current-core.mjs';
import {
  repository,
  repositoryId,
  sha256,
  need,
  commit,
} from './release-contract.mjs';

export async function prepareRelease({ source, root = process.cwd() }) {
  const git = (...args) => execFileSync('git', ['-C', root, ...args]);
  need(commit(source), 'source');
  need(git('rev-parse', 'HEAD').toString().trim() === source, 'checkout');
  need(!git('status', '--porcelain', '--untracked-files=no').length, 'dirty-source');
  need(/^\d+\.\d+\.\d+$/.test(version), 'version');
  const verified = await verifyCurrentCore();
  const core = verified.manifest;
  need(core.source.kind === 'merged', 'unpublished-core-source');
  need(core.source.commit === currentProducer, 'core-source');
  const entries = git('ls-tree', '-r', source).toString().trim().split('\n');
  const files = entries.map((entry) => {
    const match = /^(100644|100755) blob ([a-f0-9]{40})\t(.+)$/.exec(entry);
    need(match, 'source-type');
    const [, mode, object, path] = match;
    need(!path.includes('\n') && !path.includes('\\'), 'source-path');
    return { path, mode, sha256: sha256(git('cat-file', 'blob', object)) };
  });
  need(
    files.some((f) => f.path === 'gh-nibfin' && f.mode === '100755'),
    'entry',
  );
  const archive = git('archive', '--format=tar.gz', source);
  const manifest = {
    format: 1,
    package: 'gh-nibfin',
    repository,
    repositoryId,
    version,
    source,
    files,
    core: {
      producer: currentProducer,
      ...currentPin,
      assets: Object.entries(verified.assets)
        .filter(([name]) => name !== 'jose-6.2.13.tgz')
        .map(([name, bytes]) => ({
          name,
          size: bytes.length,
          sha256: sha256(bytes),
        })),
    },
    archive: {
      name: 'gh-nibfin.tar.gz',
      size: archive.length,
      sha256: sha256(archive),
    },
    support:
      'Source release; fresh Linux and Windows/WSL verification remain separate.',
  };
  mkdirSync(resolve(root, 'out'), { recursive: true });
  writeFileSync(resolve(root, 'out/gh-nibfin.tar.gz'), archive);
  writeFileSync(
    resolve(root, 'out/cli-release-manifest.json'),
    JSON.stringify(manifest, null, 2) + '\n',
  );
  return manifest;
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  const manifest = await prepareRelease({ source: process.env.GITHUB_SHA });
  console.log(JSON.stringify({ source: manifest.source, version: manifest.version }));
}

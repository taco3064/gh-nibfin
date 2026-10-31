import { createHash } from 'node:crypto';
export const repository = 'taco3064/gh-nibfin';
export const repositoryId = '1428544414';
export const sha256 = (bytes) => createHash('sha256').update(bytes).digest('hex');
export const need = (value, code) => {
  if (!value) throw Error('cli-release:' + code);
};
export const commit = (value) => /^[a-f0-9]{40}$/.test(value ?? '');
export const assetNames = ['gh-nibfin.tar.gz', 'cli-release-manifest.json'];

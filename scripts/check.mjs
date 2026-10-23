import { accessSync, constants, readdirSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
accessSync('gh-nibfin', constants.X_OK);
for (const file of ['gh-nibfin', ...['src', 'test', 'scripts'].flatMap(dir => readdirSync(dir).filter(f => f.endsWith('.mjs')).map(f => `${dir}/${f}`))]) {
  const result = spawnSync(process.execPath, ['--check', file], { stdio: 'inherit' });
  if (result.status !== 0) process.exit(1);
}
const result = spawnSync('./gh-nibfin', ['--help'], { encoding: 'utf8' });
if (result.status !== 0 || !result.stdout.includes('runner create')) process.exit(1);
console.log('Executable and JavaScript syntax checks passed.');

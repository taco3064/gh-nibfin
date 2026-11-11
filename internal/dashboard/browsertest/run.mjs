import {spawnSync} from 'node:child_process';
import {mkdtempSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {childExitCode} from './child.mjs';
const regression = spawnSync(process.execPath,
  ['--test', 'internal/dashboard/browsertest/child.test.mjs'], {stdio: 'inherit'});
if (childExitCode(regression) !== 0) process.exit(childExitCode(regression));
const scratch = mkdtempSync(join(tmpdir(), 'nibfin-browser-'));
try {
  const env = {...process.env, PLAYWRIGHT_BROWSERS_PATH: join(scratch, 'browsers'),
    PWTEST_CACHE_DIR: join(scratch, 'transforms'), TMPDIR: scratch,
    TMP: scratch, TEMP: scratch};
  const downloaded = spawnSync(process.execPath,
    ['node_modules/@playwright/test/cli.js', 'install', '--only-shell', 'chromium'],
    {stdio: 'inherit', env});
  if (childExitCode(downloaded) !== 0) throw new Error('Chromium download failed');
  const host = join(scratch, process.platform === 'win32' ? 'host.exe' : 'host');
  const built = spawnSync('go', ['build', '-o', host,
    './internal/dashboard/browsertest/host.go'], {stdio: 'inherit'});
  if (childExitCode(built) !== 0) process.exitCode = childExitCode(built);
  else {
    const result = spawnSync(process.execPath,
      ['node_modules/@playwright/test/cli.js', 'test',
        '--config=internal/dashboard/browsertest/playwright.config.mjs'], {
        stdio: 'inherit', env: {...env, NIBFIN_TEST_HOST: host,
          NIBFIN_TEST_OUTPUT: join(scratch, 'results')}
      });
    process.exitCode = childExitCode(result);
  }
} finally {
  rmSync(scratch, {recursive: true, force: true});
}

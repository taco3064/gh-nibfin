import {mkdtemp, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {spawnSync} from 'node:child_process';
import {launchBrowser} from './host.mjs';

const directory = await mkdtemp(join(tmpdir(), 'nibfin-browser-build-'));
try {
  // Own all browser scratch output, including failed prerequisite launches.
  for (const name of ['TMPDIR', 'TMP', 'TEMP']) process.env[name] = directory;
  const prerequisite = await launchBrowser();
  await prerequisite.close();
  const binary = join(directory, process.platform === 'win32' ? 'host.exe' : 'host');
  const build = spawnSync('go', ['build', '-buildvcs=false', '-trimpath', '-o', binary,
    './internal/dashboard/browsertest'], {stdio: 'inherit'});
  if (build.status !== 0) throw new Error('Browser fixture build failed');
  const result = spawnSync(process.execPath,
    ['--test', '--test-concurrency=1', '--test-timeout=60000',
      'tests/browser/dashboard.test.mjs'], {
      stdio: 'inherit', env: {...process.env, NIBFIN_BROWSER_HOST: binary}
    });
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
} finally {
  await rm(directory, {recursive: true, force: true});
}

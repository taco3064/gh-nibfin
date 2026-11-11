// One disposable installation and browser, used sequentially and always removed.
import {mkdtemp, copyFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join, dirname} from 'node:path';
import {fileURLToPath, pathToFileURL} from 'node:url';
import {spawn} from 'node:child_process';

const source = dirname(fileURLToPath(import.meta.url));
const root = join(source, '../..');
const scratch = await mkdtemp(join(tmpdir(), 'nibfin-browser-'));
const env = {...process.env, PLAYWRIGHT_BROWSERS_PATH: join(scratch, 'browsers')};
function run(program, args, cwd = root) {
  return new Promise((resolve, reject) => {
    const child = spawn(program, args, {cwd, env, stdio: 'inherit', shell: false});
    const timeout = setTimeout(() => child.kill(), 240000);
    child.on('error', error => { clearTimeout(timeout); reject(error); });
    child.on('exit', code => {
      clearTimeout(timeout);
      if (code === 0) resolve();
      else reject(new Error(`${program} failed (${code})`));
    });
  });
}
try {
  for (const file of ['package.json', 'package-lock.json']) {
    await copyFile(join(source, file), join(scratch, file));
  }
  const npmArgs = ['ci', '--ignore-scripts', '--no-audit', '--no-fund',
    '--cache', join(scratch, 'npm-cache')];
  if (process.platform === 'win32') {
    const npmCLI = join(dirname(process.execPath), 'node_modules/npm/bin/npm-cli.js');
    await run(process.execPath, [npmCLI, ...npmArgs], scratch);
  } else {
    await run('npm', npmArgs, scratch);
  }
  const cli = join(scratch, 'node_modules/playwright/cli.js');
  await run(process.execPath, [cli, 'install', 'chromium', '--only-shell']);
  const binary = join(scratch, process.platform === 'win32' ? 'host.exe' : 'host');
  await run('go', ['build', '-o', binary, './tests/browser/server']);
  process.env.PLAYWRIGHT_BROWSERS_PATH = env.PLAYWRIGHT_BROWSERS_PATH;
  const playwright = await import(pathToFileURL(
    join(scratch, 'node_modules/playwright/index.mjs')).href);
  const {verify} = await import('./suite.mjs');
  await verify(playwright.chromium, binary);
} finally {
  await rm(scratch, {recursive: true, force: true});
}

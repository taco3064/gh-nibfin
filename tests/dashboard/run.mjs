import {spawn} from 'node:child_process';
import {mkdtemp, copyFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {pathToFileURL} from 'node:url';

// Own a single disposable dependency/browser environment for this verification.
const scratch = await mkdtemp(join(tmpdir(), 'nibfin-rendered-'));
function run(args, env = process.env) {
  return new Promise((accept, reject) => {
    const child = spawn(process.execPath, args, {env, stdio: 'inherit', timeout: 180000});
    child.once('error', reject);
    child.once('exit', code => code === 0 ? accept() :
      reject(Error(`Rendered verification subprocess failed (${code})`)));
  });
}
try {
  for (const file of ['package.json', 'package-lock.json']) {
    await copyFile(resolve(file), join(scratch, file));
  }
  if (!process.env.npm_execpath) throw Error('Run through npm run test:dashboard');
  await run([process.env.npm_execpath, 'ci', '--ignore-scripts', '--include=dev',
    '--prefix', scratch]);
  const env = {...process.env, PLAYWRIGHT_BROWSERS_PATH: join(scratch, 'browsers')};
  const module = join(scratch, 'node_modules', 'playwright');
  await run([join(module, 'cli.js'), 'install', 'chromium', '--only-shell'], env);
  env.NIBFIN_BROWSER_MODULE = pathToFileURL(join(module, 'index.mjs')).href;
  await run(['--test', 'tests/dashboard/browser.test.mjs'], env);
} finally {
  await rm(scratch, {recursive: true, force: true});
}

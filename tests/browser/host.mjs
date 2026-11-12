import {mkdtemp, writeFile, rm} from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {createInterface} from 'node:readline';
import {chromium} from 'playwright';

export function launchBrowser() {
  // Hosted runners include Chrome. Never download browsers in CI.
  const options = process.env.GITHUB_ACTIONS === 'true' ? {channel: 'chrome'} : {};
  return chromium.launch(options);
}

export async function open(t, value, mode = '') {
  const directory = await mkdtemp(join(tmpdir(), 'nibfin-browser-fixture-'));
  const fixture = join(directory, 'snapshot.json');
  let child;
  let browser;
  let exited;
  t.after(async () => {
    try {
      await browser?.close();
    } finally {
      try {
        if (child) {
          child.stdin.end();
          const timeout = setTimeout(() => child.kill('SIGKILL'), 5000);
          try {
            const [code, signal] = await exited;
            if (code !== 0 || signal) throw new Error('Fixture host failed cleanup');
          } finally {
            clearTimeout(timeout);
          }
        }
      } finally {
        await rm(directory, {recursive: true, force: true});
      }
    }
  });
  await writeFile(fixture, JSON.stringify(value));
  child = spawn(process.env.NIBFIN_BROWSER_HOST, [fixture, mode],
    {stdio: ['pipe', 'pipe', 'pipe']});
  exited = once(child, 'exit');
  const lines = createInterface({input: child.stdout});
  const url = await Promise.race([
    once(lines, 'line').then(([line]) => line),
    exited.then(() => { throw new Error('Fixture host did not start'); })
  ]);
  lines.close();
  browser = await launchBrowser();
  const context = await browser.newContext();
  const page = await context.newPage();
  page.setDefaultTimeout(8000);
  const requests = [];
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => requests.push(request));
  const origin = new URL(url).origin;
  await context.route('**/*', async route => {
    if (new URL(route.request().url()).origin !== origin) {
      throw new Error('Unexpected external browser request');
    }
    await route.continue();
  });
  await page.goto(mode === 'missing' ? `${origin}/` : url);
  await page.waitForFunction(() => location.hash === '');
  return {page, context, requests, errors, url, origin,
    async load() {
      await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
      await page.locator('#dashboard').waitFor({state: 'visible'});
    }};
}

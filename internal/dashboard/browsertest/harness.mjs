import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {test as base, expect} from '@playwright/test';
import {fixture, instant} from './fixtures.mjs';

export {expect};
export const test = base.extend({
  app: async ({page}, use) => {
    const hosts = [];
    const browserErrors = [];
    const violations = [];
    const requests = [];
    const logs = [];
    page.on('pageerror', error => browserErrors.push(error.message));
    page.on('console', message => {
      logs.push(message.text());
      if (message.text().includes('Content Security Policy')) {
        violations.push(message.text());
      }
    });
    await page.clock.install({time: instant});
    await page.addInitScript(() => {
      document.addEventListener('DOMContentLoaded', () => {
        globalThis.bootstrapClean = location.hash === '';
      }, {once: true});
    });
    let origin;
    let proof;
    await page.route('**/*', async route => {
      const request = route.request();
      const url = new URL(request.url());
      requests.push({url: request.url(), method: request.method(),
        headers: await request.allHeaders()});
      const allowed = ['/', '/session.js', '/view.js', '/model.js', '/dom.js',
        '/repository.js', '/graph.js', '/dashboard.css', '/api/snapshot'];
      if (url.origin !== origin || !allowed.includes(url.pathname) || url.search ||
          request.method() !== 'GET') {
        violations.push('Unexpected network destination');
        return route.abort();
      }
      await route.continue();
    });
    async function start(value = fixture(), fragment = true) {
      const child = spawn(process.env.NIBFIN_TEST_HOST, [],
        {stdio: ['pipe', 'pipe', 'pipe']});
      hosts.push(child);
      const errors = [];
      child.stderr.on('data', data => {
        errors.push(data.toString());
        logs.push(data.toString());
      });
      const bootstrap = new Promise((resolve, reject) => {
        let buffer = '';
        child.stdout.on('data', data => {
          buffer += data.toString();
          if (buffer.includes('\n')) resolve(buffer.trim());
        });
        child.once('error', reject);
        child.once('exit', () => reject(new Error('Synthetic host exited')));
      });
      child.stdin.write(JSON.stringify(value) + '\n');
      const url = await Promise.race([bootstrap, new Promise((_, reject) => {
        const timer = setTimeout(() => reject(new Error('Host start timeout')), 5000);
        timer.unref();
      })]);
      origin = new URL(url).origin;
      proof = new URL(url).hash.slice(1);
      await page.goto(fragment === true ? url : origin + '/' +
        (fragment === false ? '' : '#' + fragment));
      await expect(page.locator('#snapshot')).toContainText(
        fragment === true || /^[a-f0-9]{64}$/.test(fragment) ?
          'Ready' : 'Session proof unavailable');
      return {origin, proof, errors};
    }
    async function load(value = fixture()) {
      const info = await start(value);
      await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
      await expect(page.locator('#snapshot')).toContainText('Snapshot loaded');
      return {...info, value};
    }
    try {
      await use({start, load, requests,
        snapshotReads: () => requests.filter(r => r.url.endsWith('/api/snapshot')),
        proof: () => proof, origin: () => origin});
      expect(browserErrors).toEqual([]);
      expect(violations).toEqual([]);
      if (proof) expect(logs.join('\n')).not.toContain(proof);
    } finally {
      for (const child of hosts) {
        if (child.exitCode === null) {
          child.stdin.end('stop\n');
          const exited = once(child, 'exit');
          const timer = setTimeout(() => child.kill('SIGTERM'), 3000);
          await exited;
          clearTimeout(timer);
        }
      }
    }
  }
});

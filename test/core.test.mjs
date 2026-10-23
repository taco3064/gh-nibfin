// Deliberately fails, rather than skips, when the real reviewed input is absent.
import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { verifyCoreInstallation } from '../src/core.mjs';
test('offline real pinned Core archive verification', async () => {
  assert.ok(process.env.NIBFIN_TEST_CORE_ARCHIVE, 'Supply the reviewed pinned Core archive (see README).');
  assert.ok(process.env.NIBFIN_TEST_CORE_EXPECTED, 'Supply the independently reviewed expected identity JSON.');
  const expected = JSON.parse(readFileSync(process.env.NIBFIN_TEST_CORE_EXPECTED, 'utf8'));
  const directory = mkdtempSync(join(tmpdir(), 'nibfin-core-'));
  try {
    writeFileSync(join(directory, 'package.json'), '{"private":true}');
    const install = spawnSync('npm', ['install', '--offline', '--ignore-scripts', '--no-audit', '--no-fund', '--package-lock=false', resolve(process.env.NIBFIN_TEST_CORE_ARCHIVE)], { cwd: directory, encoding: 'utf8' });
    assert.equal(install.status, 0, 'Offline archive installation must succeed.');
    const require = createRequire(join(directory, 'package.json'));
    const modulePath = require.resolve('@nibfin/core/identity');
    assert.equal((await verifyCoreInstallation({ modulePath, expected })).ok, true);
    for (const field of ['name', 'version', 'contentDigest']) {
      assert.equal((await verifyCoreInstallation({ modulePath, expected: { ...expected, [field]: 'intentionally-wrong' } })).ok, false);
    }
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

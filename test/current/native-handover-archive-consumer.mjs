import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { sha } from '../../src/local-state.mjs';
import { openNativeHandoverArchive } from '../../src/native-handover-archive.mjs';

async function fixture(core, root) {
  const directory = await fs.mkdtemp(join(root, 'handover-archive-'));
  const body = { version: 1, target: { number: '7' }, preserved: 'original' };
  const state = { ...body, identity: core.contentIdentity(body) };
  const path = 'original/cycle/state.json';
  const bytes = Buffer.from(JSON.stringify(state));
  await fs.mkdir(dirname(join(directory, path)), { recursive: true, mode: 0o700 });
  await fs.writeFile(join(directory, path), bytes, { mode: 0o600 });
  const manifest = {
    format: 1,
    createdAt: '2026-11-01T00:00:00Z',
    authority: 'historical-data-only',
    activationAuthorized: false,
    issues: { 7: { status: 'UNACCEPTED' } },
    files: [{ path, bytes: bytes.length, sha256: sha(bytes), sourceMode: 0o644 }],
  };
  const save = () =>
    fs.writeFile(join(directory, 'manifest.json'), JSON.stringify(manifest), {
      mode: 0o600,
    });
  await save();
  return {
    directory,
    manifest,
    save,
    path,
    state,
    open: () =>
      openNativeHandoverArchive(core, {
        directory,
        hostRoot: join(root, 'separate-host'),
      }),
  };
}

test('archive custody retains exact bytes without supplying permission', async (t) =>
  withCurrentCore(async (core, context) => {
    await t.test(
      'reads the real inventory and rechecks files on access',
      async () => {
        const f = await fixture(core, context.root);
        await fs.mkdir(join(f.directory, 'empty'), { mode: 0o700 });
        const archive = await f.open();
        assert.equal(archive.files.size, 1);
        assert.deepEqual(archive.candidates, [f.path]);
        assert.deepEqual((await archive.readCycle(f.path)).state, f.state);
        assert.equal(archive.metadata['7'].status, 'UNACCEPTED');
        await fs.writeFile(join(f.directory, f.path), 'changed');
        await assert.rejects(archive.readBytes(f.path), /file-changed|file-bytes/);
      },
    );
    for (const [name, change] of Object.entries({
      'altered bytes': (f) => fs.writeFile(join(f.directory, f.path), 'changed'),
      'unknown file': (f) =>
        fs.writeFile(join(f.directory, 'extra'), '', { mode: 0o600 }),
      'missing file': (f) => fs.unlink(join(f.directory, f.path)),
      'public permissions': (f) => fs.chmod(join(f.directory, f.path), 0o644),
      'duplicate inventory': async (f) => {
        f.manifest.files.push({ ...f.manifest.files[0] });
        await f.save();
      },
      'parent traversal': async (f) => {
        f.manifest.files[0].path = '../outside';
        await f.save();
      },
      'imported activation claim': async (f) => {
        f.manifest.activationAuthorized = true;
        await f.save();
      },
      'oversized file claim': async (f) => {
        f.manifest.files[0].bytes = 129 * 1024 * 1024;
        await f.save();
      },
      symlink: async (f) => {
        await fs.unlink(join(f.directory, f.path));
        await fs.symlink(
          join(f.directory, 'manifest.json'),
          join(f.directory, f.path),
        );
      },
      hardlink: (f) =>
        fs.link(join(f.directory, f.path), join(f.directory, 'linked')),
    }))
      await t.test('rejects ' + name, async () => {
        const f = await fixture(core, context.root);
        await change(f);
        await assert.rejects(f.open());
      });
  }));

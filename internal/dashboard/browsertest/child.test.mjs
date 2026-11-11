import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import test from 'node:test';
import {childExitCode} from './child.mjs';

function runnerExit(command, args) {
  const helper = new URL('./child.mjs', import.meta.url).href;
  const source = `
    import {spawnSync} from 'node:child_process';
    import {childExitCode} from ${JSON.stringify(helper)};
    const result = spawnSync(${JSON.stringify(command)}, ${JSON.stringify(args)});
    process.exitCode = childExitCode(result);
  `;
  return spawnSync(process.execPath, ['--input-type=module', '-e', source], {
    timeout: 10000
  });
}

test('signal-terminated test child fails the runner', () => {
  const result = runnerExit(process.execPath,
    ['-e', 'process.kill(process.pid, "SIGTERM")']);
  assert.equal(result.error, undefined);
  assert.equal(result.signal, null);
  assert.equal(result.status, 1);
});

test('spawn errors fail the runner', () => {
  const result = runnerExit(`${process.execPath}/missing-test-child`, []);
  assert.equal(result.error, undefined);
  assert.equal(result.signal, null);
  assert.equal(result.status, 1);
});

test('only an explicit successful exit passes', () => {
  assert.equal(childExitCode({status: 0}), 0);
  assert.equal(childExitCode({status: 7}), 7);
  assert.equal(childExitCode({status: null}), 1);
  assert.equal(childExitCode({}), 1);
  assert.equal(childExitCode({status: 0, signal: 'SIGTERM'}), 1);
  assert.equal(childExitCode({status: 0, error: new Error('spawn failed')}), 1);
});

// Core owns credential isolation, subprocess bounds and conditional GETs.
// Each native GET authenticates at GitHub; mutations retain local preparation.
export function nativeStoredGithubTransport(core, { executable = 'gh' } = {}) {
  return core.localGhTransport({ executable, authenticateReads: false });
}

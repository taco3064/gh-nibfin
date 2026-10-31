// Core owns observation policy; Host composes its response persistence.
export function nativeGithubTransport(core, { send, onResponse } = {}) {
  if (send) return core.createConditionalGithubTransport({ send, onResponse });
  return core.localGhTransport({ authenticateReads: false, onResponse });
}

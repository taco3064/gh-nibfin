const need = (v, c) => {
  if (!v) throw Error('native-publication:' + c);
};
// Publication already owns fresh authorization/dependency gates at each write
// boundary. Keep its native host/Issue/semantic-graph checks there too, rather
// than recursively running the entire Core execution checkpoint on every read
// of its immutable current() view (including reads made by those same gates).
export async function nativePublicationGuard(
  core,
  { store, checkpoint, dependencies } = {},
) {
  const initial = await store.read(),
    keys = [
      'cycle',
      'core',
      'installation',
      'authority',
      'grant',
      'policy',
      'member',
      'scopeHash',
      'scopeBody',
      'shapeBody',
      'base',
      'head',
      'candidate',
      'workspace',
      'workspaceExpected',
      'receipts',
      'expectations',
      'attempts',
      'dispatch',
    ];
  const identity = (s) =>
      core.contentIdentity(Object.fromEntries(keys.map((k) => [k, s[k] ?? null]))),
    expected = identity(initial);
  let graph;
  return Object.freeze({
    async checkpoint() {
      await checkpoint({ forceRemote: true, exactBody: true });
      need(identity(await store.read()) === expected, 'protected-inputs-changed');
    },
    dependencies: {
      async evaluate(input, options) {
        const value = await dependencies.evaluate(input, options);
        if (value.outcome === 'ready') {
          const current = core.contentIdentity({
            target: value.target,
            edges: value.edges,
            completions: value.completions,
            progress: value.progress ?? [],
          });
          if (graph === undefined) graph = current;
          else need(current === graph, 'dependencies-changed');
        }
        return value;
      },
    },
  });
}

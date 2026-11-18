'use strict';
(() => {
  const [proof, mode] = location.hash.slice(1).split(':');
  history.replaceState(null, '', '/');
  const output = document.getElementById('snapshot');
  const button = document.getElementById('connect');
  button.disabled = !/^[a-f0-9]{64}$/.test(proof);
  if (!button.disabled) output.textContent = 'Ready. Select Read snapshot to connect.';
  let view;
  let readVersion = 0;
  const request = (path, options = {}) => fetch(path, {
    credentials: 'omit', cache: 'no-store', redirect: 'error', ...options,
    headers: {'X-Nibfin-Session': proof, ...options.headers}
  });
  let activeRepository;
  if (mode === 'overview' && !button.disabled) {
    button.hidden = true;
    output.textContent = 'Select a project or an assigned Issue to inspect.';
    getOverview();
  }
  function returnToOverview() {
    readVersion++;
    view?.invalidate();
    document.getElementById('dashboard').hidden = true;
    document.getElementById('user-overview').hidden = false;
    document.getElementById('back-overview').hidden = true;
    button.hidden = true;
    const picker = document.getElementById('project-picker');
    const target = picker.disabled ? document.getElementById('refresh-overview') : picker;
    target.focus();
  }
  async function getOverview() {
    const overview = await import('/overview.js');
    overview.start(request, async (repository, identity) => {
      const version = ++readVersion;
      view?.invalidate();
      document.getElementById('dashboard').hidden = true;
      document.getElementById('back-overview').hidden = false;
      output.textContent = 'Reading project detail…';
      try {
        const selected = await request('/api/repository', {
          method: 'POST', headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({repository: repository.name})
        });
        if (version !== readVersion) return;
        if (!selected.ok) throw new Error('Unavailable');
        if (!await read(identity, repository, version)) return;
        if (version !== readVersion) return;
        activeRepository = repository;
        if (identity) view.selectIssue(identity);
        document.getElementById('user-overview').hidden = true;
        button.hidden = false;
        if (!identity) document.getElementById('repository-heading').focus();
      } catch {
        if (version !== readVersion) return;
        returnToOverview();
        output.textContent = 'Project detail unavailable; overview remains available.';
      }
    }, returnToOverview);
  }
  async function read(expected, repository = activeRepository, version = ++readVersion) {
    view?.invalidate();
    button.disabled = true;
    output.textContent = 'Reading local snapshot…';
    try {
      const response = await request('/api/snapshot');
      if (!response.ok) throw new Error('Unavailable');
      const snapshot = await response.json();
      if (version !== readVersion) return false;
      const actual = snapshot.repository?.items?.[0];
      if (repository && (snapshot.repository?.status?.availability !== 'known' ||
        snapshot.repository.items.length !== 1 || actual?.id !== repository.id ||
        actual?.name?.toLowerCase() !== repository.name.toLowerCase())) {
        throw new Error('Project identity changed');
      }
      if (expected && !(snapshot.issues?.items ?? []).some(issue =>
        issue.identity.id === expected.id && issue.identity.number === expected.number &&
        issue.identity.repository.id === expected.repository.id &&
        issue.identity.repository.name === expected.repository.name)) {
        throw new Error('Selected Issue unavailable');
      }
      view ??= await import('/view.js');
      if (version !== readVersion) return false;
      view.display(snapshot, async intent => {
        const response = await request('/api/labels', {
          method: 'POST', headers: {'Content-Type': 'application/json'},
          body: JSON.stringify(intent)
        });
        if (!response.ok) throw new Error('Unavailable');
        return response.json();
      });
      output.textContent = 'Snapshot loaded. Read again to refresh observations.';
      return true;
    } catch {
      if (version !== readVersion) return false;
      view?.invalidate();
      if (mode === 'overview') throw new Error('Unavailable');
      output.textContent = 'Snapshot unavailable or unsupported. ' +
        'Retry Read snapshot, or reopen a current local session link.';
    } finally {
      if (version === readVersion) button.disabled = false;
    }
  }
  button.addEventListener('click', () => {
    void read().catch(() => {
      returnToOverview();
      output.textContent = 'Project refresh unavailable; select a project to retry.';
    });
  });
})();

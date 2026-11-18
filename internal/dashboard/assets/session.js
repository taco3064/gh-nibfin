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
  if (mode === 'overview' && !button.disabled) {
    button.hidden = true;
    output.textContent = 'Select an Issue to inspect its repository and details.';
    getOverview();
  }
  async function getOverview() {
    const overview = await import('/overview.js');
    overview.start(request, async (repository, identity) => {
      readVersion++;
      view?.invalidate();
      output.textContent = 'Reading repository detail…';
      try {
        const selected = await request('/api/repository', {
          method: 'POST', headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({repository})
        });
        if (!selected.ok) throw new Error('Unavailable');
        if (!await read(identity)) throw new Error('Stale selection');
        view?.selectIssue(identity);
        document.getElementById('user-overview').hidden = true;
        document.getElementById('back-overview').hidden = false;
        button.hidden = false;
      } catch {
        output.textContent = 'Repository detail unavailable; overview remains available.';
      }
    });
  }
  async function read(expected) {
    const version = ++readVersion;
    view?.invalidate();
    button.disabled = true;
    output.textContent = 'Reading local snapshot…';
    try {
      const response = await fetch('/api/snapshot', {
        method: 'GET',
        headers: {'X-Nibfin-Session': proof},
        credentials: 'omit',
        cache: 'no-store',
        redirect: 'error'
      });
      if (!response.ok) throw new Error('Unavailable');
      const snapshot = await response.json();
      if (version !== readVersion) return false;
      if (expected && !(snapshot.issues?.items ?? []).some(issue =>
        issue.identity.id === expected.id && issue.identity.number === expected.number &&
        issue.identity.repository.id === expected.repository.id &&
        issue.identity.repository.name === expected.repository.name)) {
        throw new Error('Selected Issue unavailable');
      }
      view ??= await import('/view.js');
      if (version !== readVersion) return false;
      view.display(snapshot, async intent => {
        const response = await fetch('/api/labels', {
          method: 'POST', credentials: 'omit', cache: 'no-store', redirect: 'error',
          headers: {'X-Nibfin-Session': proof, 'Content-Type': 'application/json'},
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
  button.addEventListener('click', () => { void read().catch(() => {}); });
})();

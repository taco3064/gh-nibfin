'use strict';
(() => {
  const proof = location.hash.slice(1);
  history.replaceState(null, '', '/');
  const output = document.getElementById('snapshot');
  const button = document.getElementById('connect');
  button.disabled = !/^[a-f0-9]{64}$/.test(proof);
  if (!button.disabled) output.textContent = 'Ready. Select Read snapshot to connect.';
  let view;
  button.addEventListener('click', async () => {
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
      view ??= await import('/view.js');
      view.display(snapshot);
      output.textContent = 'Snapshot loaded. Read again to refresh observations.';
    } catch {
      view?.invalidate();
      output.textContent = 'Snapshot unavailable or unsupported. ' +
        'Retry Read snapshot, or reopen a current local session link.';
    } finally {
      button.disabled = false;
    }
  });
})();

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
    } catch {
      view?.invalidate();
      output.textContent = 'Snapshot unavailable or unsupported. ' +
        'Retry Read snapshot, or reopen a current local session link.';
    } finally {
      button.disabled = false;
    }
  });
})();

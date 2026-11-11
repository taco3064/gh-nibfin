'use strict';
(() => {
  const proof = location.hash.slice(1);
  history.replaceState(null, '', '/');
  const output = document.getElementById('snapshot');
  const button = document.getElementById('connect');
  button.disabled = !/^[a-f0-9]{64}$/.test(proof);
  if (!button.disabled) output.textContent = 'Ready. Select Read snapshot to connect.';
  const app = import('./app.js').catch(() => null);
  button.addEventListener('click', async () => {
    button.disabled = true;
    try {
      const response = await fetch('/api/snapshot', {
        headers: {'X-Nibfin-Session': proof},
        credentials: 'omit',
        cache: 'no-store',
        redirect: 'error'
      });
      if (!response.ok) throw new Error('Snapshot request unavailable.');
      const ui = await app;
      if (!ui) throw new Error('Frontend unavailable');
      ui.display(await response.json());
      output.textContent = 'Snapshot loaded. Read-only facts.';
    } catch {
      (await app)?.failed();
      output.textContent = 'Snapshot unavailable or unusable. ' +
        'Reopen a current local session link or retry.';
    } finally {
      button.disabled = false;
    }
  });
})();
